// Package catalog implements Phase 5: Service Catalog Push.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/adc/internal/apim"
	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

// Phase implements engine.Phase for Service Catalog push.
type Phase struct {
	cfg     *config.Config
	client  apim.CatalogClient
	repos   *store.Repositories
	logger  *logging.Logger
}

// New creates a new catalog Phase.
func New(cfg *config.Config, client apim.CatalogClient, repos *store.Repositories, logger *logging.Logger) *Phase {
	return &Phase{
		cfg:    cfg,
		client: client,
		repos:  repos,
		logger: logger,
	}
}

// Name returns the phase name.
func (p *Phase) Name() string { return "catalog" }

// Run executes the Phase 5 Service Catalog push pipeline.
func (p *Phase) Run(ctx context.Context, cycleID string) error {
	log := p.logger.WithFields("phase", "catalog", "cycle_id", cycleID)
	catCfg := p.cfg.ServiceCatalog

	// Step 0: Identify SPEC_GENERATED groups
	groups, err := p.repos.Unmanaged.GetSpecGeneratedGroups(ctx)
	if err != nil {
		return fmt.Errorf("step 0 get spec_generated groups: %w", err)
	}
	if len(groups) == 0 {
		log.Infow("No services ready to push")
		return nil
	}
	log.Infow("Step 0: Groups identified", "count", len(groups))

	// Filter by push_shadow / push_drift config
	var pushable []store.CatalogPushGroup
	for _, g := range groups {
		if g.Classification == "SHADOW" && !catCfg.PushShadow {
			log.Infow("Skipping SHADOW (push_shadow=false)", "service_key", g.ServiceKey)
			continue
		}
		if g.Classification == "DRIFT" && !catCfg.PushDrift {
			log.Infow("Skipping DRIFT (push_drift=false)", "service_key", g.ServiceKey)
			continue
		}
		pushable = append(pushable, g)
	}

	if len(pushable) == 0 {
		log.Infow("No pushable groups after filtering")
		return nil
	}

	var created, updated, skipped, errCount int
	for _, group := range pushable {
		action, err := p.pushGroup(ctx, log, group, catCfg)
		if err != nil {
			log.Errorw("Failed to push service",
				"service_key", group.ServiceKey,
				"classification", group.Classification,
				"error", err,
			)
			errCount++
			continue
		}
		switch action {
		case "CREATED":
			created++
		case "UPDATED":
			updated++
		case "SKIPPED":
			skipped++
		}
	}

	log.Infow("Catalog push complete",
		"groups", len(pushable),
		"created", created,
		"updated", updated,
		"skipped", skipped,
		"errors", errCount,
	)
	return nil
}

// pushGroup handles the push logic for a single service group.
// Returns the action taken: "CREATED", "UPDATED", or "SKIPPED".
func (p *Phase) pushGroup(ctx context.Context, log *logging.Logger, group store.CatalogPushGroup, catCfg config.ServiceCatalogConfig) (string, error) {
	catalogName := deriveCatalogName(group)
	catalogVersion := time.Now().Format("2006.01.02")

	metadata := buildMetadata(group, catalogName, catalogVersion, p.cfg.Server.Hostname)

	if group.CatalogServiceID == nil {
		// Never pushed → CREATE
		return p.createEntry(ctx, log, group, metadata, catalogName, catalogVersion)
	}

	// Previously pushed → check if still exists
	existingID, err := p.client.GetService(ctx, *group.CatalogServiceID)
	if err != nil {
		// On error, attempt UPDATE as safe default
		log.Warnw("Catalog GET failed, attempting UPDATE",
			"service_key", group.ServiceKey,
			"catalog_service_id", *group.CatalogServiceID,
			"error", err,
		)
		return p.updateEntry(ctx, log, group, *group.CatalogServiceID, metadata, catalogName, catalogVersion)
	}

	if existingID != "" {
		// Still exists → UPDATE
		return p.updateEntry(ctx, log, group, *group.CatalogServiceID, metadata, catalogName, catalogVersion)
	}

	// Entry was deleted by operator
	if catCfg.RePushDeleted {
		log.Infow("Catalog entry deleted by operator, re-creating",
			"service_key", group.ServiceKey,
			"catalog_name", catalogName,
		)
		return p.createEntry(ctx, log, group, metadata, catalogName, catalogVersion)
	}

	// Respect operator deletion
	log.Infow("Catalog entry deleted by operator, skipping (re_push_deleted=false)",
		"service_key", group.ServiceKey,
		"catalog_name", catalogName,
	)
	if _, err := p.repos.Unmanaged.MarkPushedSkipped(ctx, group); err != nil {
		log.Warnw("Failed to mark pushed-skipped", "service_key", group.ServiceKey, "error", err)
	}
	return "SKIPPED", nil
}

// createEntry creates a new catalog entry, handling 409 conflict recovery.
func (p *Phase) createEntry(ctx context.Context, log *logging.Logger, group store.CatalogPushGroup, metadata apim.ServiceMetadata, catalogName, catalogVersion string) (string, error) {
	serviceID, err := p.client.CreateService(ctx, metadata, group.OpenAPISpec)
	if err != nil {
		var conflictErr *apim.ConflictError
		if errors.As(err, &conflictErr) ||
			strings.Contains(err.Error(), "HTTP 500") ||
			strings.Contains(err.Error(), "HTTP 409") {
			// 409 or 500 — name may already exist. Try to recover via search + update.
			log.Warnw("Catalog POST conflict, recovering via search+update",
				"catalog_name", catalogName,
				"original_error", err.Error(),
			)
			return p.recoverConflict(ctx, log, group, metadata, catalogName, catalogVersion)
		}
		return "", err
	}

	// Persist catalog state
	if _, err := p.repos.Unmanaged.MarkPushed(ctx, group, serviceID, catalogVersion); err != nil {
		log.Warnw("Failed to update DB after catalog create",
			"service_key", group.ServiceKey,
			"catalog_service_id", serviceID,
			"error", err,
		)
	}

	log.Infow("Service created in catalog",
		"catalog_name", catalogName,
		"classification", group.Classification,
		"catalog_service_id", serviceID,
	)
	return "CREATED", nil
}

// updateEntry updates an existing catalog entry.
// If APIM rejects the update because the name/version/key changed (e.g. after
// a classification change from SHADOW→DRIFT), the old entry is deleted and a
// new one is created automatically.
func (p *Phase) updateEntry(ctx context.Context, log *logging.Logger, group store.CatalogPushGroup, serviceID string, metadata apim.ServiceMetadata, catalogName, catalogVersion string) (string, error) {
	if err := p.client.UpdateService(ctx, serviceID, metadata, group.OpenAPISpec); err != nil {
		if strings.Contains(err.Error(), "Cannot update the name") ||
			strings.Contains(err.Error(), "HTTP 400") {
			log.Infow("Catalog entry name/version changed, replacing entry",
				"old_service_id", serviceID,
				"new_name", catalogName,
				"classification", group.Classification,
			)
			if delErr := p.client.DeleteService(ctx, serviceID); delErr != nil {
				return "", fmt.Errorf("delete old catalog entry %s: %w", serviceID, delErr)
			}
			return p.createEntry(ctx, log, group, metadata, catalogName, catalogVersion)
		}
		return "", err
	}

	if _, err := p.repos.Unmanaged.MarkPushed(ctx, group, serviceID, catalogVersion); err != nil {
		log.Warnw("Failed to update DB after catalog update",
			"service_key", group.ServiceKey,
			"catalog_service_id", serviceID,
			"error", err,
		)
	}

	log.Infow("Service updated in catalog",
		"catalog_name", catalogName,
		"classification", group.Classification,
		"catalog_service_id", serviceID,
	)
	return "UPDATED", nil
}

// recoverConflict handles 409 by searching for the existing entry by name and updating it.
func (p *Phase) recoverConflict(ctx context.Context, log *logging.Logger, group store.CatalogPushGroup, metadata apim.ServiceMetadata, catalogName, catalogVersion string) (string, error) {
	existingID, err := p.client.SearchByName(ctx, catalogName)
	if err != nil {
		return "", fmt.Errorf("conflict recovery search for %s: %w", catalogName, err)
	}
	if existingID == "" {
		return "", fmt.Errorf("409 conflict but service %q not found by name search", catalogName)
	}

	return p.updateEntry(ctx, log, group, existingID, metadata, catalogName, catalogVersion)
}

// deriveCatalogName generates the catalog entry name from the group.
func deriveCatalogName(group store.CatalogPushGroup) string {
	if group.Classification == "DRIFT" {
		parentName := extractParentAPIName(group.OpenAPISpec)
		if parentName != "" {
			return parentName + "-Drift"
		}
	}
	name := strings.ReplaceAll(group.ServiceKey, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

// buildMetadata constructs the ServiceMetadata for catalog API calls.
// The hostname parameter embeds an ownership marker [ADC:{hostname}] in the
// description so the reconciliation feature can identify entries belonging
// to this ADC instance.
func buildMetadata(group store.CatalogPushGroup, catalogName, catalogVersion, hostname string) apim.ServiceMetadata {
	var classDesc string
	if group.Classification == "SHADOW" {
		classDesc = fmt.Sprintf("[SHADOW] Discovered by ADC — "+
			"entire service is unmanaged. Service key: %s", group.ServiceKey)
	} else {
		classDesc = fmt.Sprintf("[DRIFT] Discovered by ADC — "+
			"undocumented operations on managed API. Service key: %s", group.ServiceKey)
	}
	description := fmt.Sprintf("[ADC:%s] %s", hostname, classDesc)

	serverURL := extractServerURL(group.OpenAPISpec)

	return apim.ServiceMetadata{
		Name:           catalogName,
		Version:        catalogVersion,
		Description:    description,
		ServiceURL:     serverURL,
		DefinitionType: "OAS3",
		MutualSSL:      false,
	}
}

// extractServerURL extracts the first server URL from the OAS spec.
func extractServerURL(spec json.RawMessage) string {
	var oas map[string]interface{}
	if err := json.Unmarshal(spec, &oas); err != nil {
		return "http://unknown-host"
	}
	servers, ok := oas["servers"].([]interface{})
	if !ok || len(servers) == 0 {
		return "http://unknown-host"
	}
	first, ok := servers[0].(map[string]interface{})
	if !ok {
		return "http://unknown-host"
	}
	if u, ok := first["url"].(string); ok && u != "" {
		return u
	}
	return "http://unknown-host"
}

// extractParentAPIName extracts the parent API name from x-adc-metadata in the spec.
func extractParentAPIName(spec json.RawMessage) string {
	var oas map[string]interface{}
	if err := json.Unmarshal(spec, &oas); err != nil {
		return ""
	}
	info, ok := oas["info"].(map[string]interface{})
	if !ok {
		return ""
	}
	meta, ok := info["x-adc-metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	if name, ok := meta["parent_api_name"].(string); ok {
		return name
	}
	return ""
}
