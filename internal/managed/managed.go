package managed

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/adc/internal/apim"
	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

const pipelineName = "managed_api_sync"

// Phase implements engine.Phase for managed API sync.
type Phase struct {
	cfg    *config.Config
	client apim.PublisherClient
	repos  *store.Repositories
	logger *logging.Logger
}

// New creates a new managed Phase.
func New(cfg *config.Config, client apim.PublisherClient, repos *store.Repositories, logger *logging.Logger) *Phase {
	return &Phase{
		cfg:    cfg,
		client: client,
		repos:  repos,
		logger: logger,
	}
}

// Name returns the phase name.
func (p *Phase) Name() string { return "managed" }

// Run executes the Phase 2 managed API sync pipeline.
func (p *Phase) Run(ctx context.Context, cycleID string) error {
	log := p.logger.WithFields("phase", "managed", "cycle_id", cycleID)
	passThrough := p.cfg.Managed.Sync.ContextPassThrough

	// Step 1: Fetch published API list
	summaries, err := p.client.ListPublishedAPIs(ctx)
	if err != nil {
		return fmt.Errorf("step 1 list APIs: %w", err)
	}
	log.Infow("Step 1 complete", "apis_found", len(summaries))

	// Step 2-3: Fetch details and process each API
	var synced, created, updated, unchanged int
	currentAPIIDs := make(map[string]bool, len(summaries))

	for _, summary := range summaries {
		currentAPIIDs[summary.ID] = true

		detail, err := p.client.GetAPIDetail(ctx, summary.ID)
		if err != nil {
			log.Warnw("Failed to fetch API detail, skipping",
				"api_id", summary.ID, "api_name", summary.Name, "error", err)
			continue
		}

		// Change detection: check if API has changed
		existing, err := p.repos.Managed.GetByAPIMApiID(ctx, summary.ID)
		if err != nil {
			log.Warnw("Failed to lookup existing API", "api_id", summary.ID, "error", err)
		}

		if existing != nil && detail.LastUpdatedTime != "" {
			if t, parseErr := parseAPIMTime(detail.LastUpdatedTime); parseErr == nil {
				if existing.APIMLastUpdatedAt != nil && t.Equal(*existing.APIMLastUpdatedAt) {
					// Unchanged — just update last_synced_at
					if err := p.repos.Managed.UpdateLastSynced(ctx, existing.ID); err != nil {
						log.Warnw("Failed to update last_synced_at", "api_id", summary.ID, "error", err)
					}
					unchanged++
					synced++
					continue
				}
			}
		}

		// Process API detail
		api := ProcessAPIDetail(detail, passThrough)
		ops := ProcessOperations(detail, api.EndpointBasepath, passThrough)

		// Upsert to database
		isNew := existing == nil
		if err := p.repos.Managed.UpsertAPI(ctx, api, ops); err != nil {
			log.Errorw("Failed to upsert API", "api_id", summary.ID, "error", err)
			continue
		}

		if isNew {
			created++
		} else {
			updated++
		}
		synced++

		log.Infow("API synced",
			"api_id", summary.ID,
			"api_name", summary.Name,
			"operations", len(ops),
			"status", func() string {
				if isNew {
					return "created"
				}
				return "updated"
			}(),
		)
	}

	log.Infow("Step 2-4 complete",
		"synced", synced,
		"created", created,
		"updated", updated,
		"unchanged", unchanged,
	)

	// Step 5: Detect deletions
	if len(currentAPIIDs) > 0 {
		deleted, err := p.repos.Managed.MarkDeleted(ctx, currentAPIIDs)
		if err != nil {
			log.Errorw("Deletion detection failed", "error", err)
		} else if deleted > 0 {
			log.Infow("Step 5 deletion detection", "deleted", deleted)
		}
	} else {
		// Safety: APIM returned 0 APIs — skip deletion
		activeCount, _ := p.repos.Managed.CountActive(ctx)
		if activeCount > 0 {
			log.Warnw("APIM returned 0 published APIs but ADC has active APIs, skipping deletion",
				"active_count", activeCount)
		}
	}

	// Step 6: Advance watermark
	if err := p.repos.PipelineState.Advance(ctx, pipelineName, time.Now(), int64(synced)); err != nil {
		return fmt.Errorf("step 6 advance watermark: %w", err)
	}

	log.Infow("Managed sync complete",
		"total_synced", synced,
		"created", created,
		"updated", updated,
		"unchanged", unchanged,
	)
	return nil
}

// parseAPIMTime parses APIM's timestamp format.
func parseAPIMTime(s string) (time.Time, error) {
	// APIM uses formats like "2024-01-15T10:30:00.000+0530" or RFC3339
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
		"2006-01-02T15:04:05.999-0700",
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}
