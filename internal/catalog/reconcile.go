// Package catalog implements Phase 5: Service Catalog Push and Reconciliation.
package catalog

import (
	"context"
	"fmt"
	"regexp"

	"github.com/wso2/adc/internal/apim"
	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

// ownershipRegex parses the [ADC:hostname] marker from catalog entry descriptions.
var ownershipRegex = regexp.MustCompile(`^\[ADC:(.+?)\]`)

// Reconciler detects and optionally cleans up orphaned catalog entries.
type Reconciler struct {
	cfg      config.ReconciliationConfig
	hostname string
	client   apim.CatalogClient
	repos    *store.Repositories
	logger   *logging.Logger
}

// NewReconciler creates a new catalog Reconciler.
func NewReconciler(cfg config.ReconciliationConfig, hostname string, client apim.CatalogClient, repos *store.Repositories, logger *logging.Logger) *Reconciler {
	return &Reconciler{
		cfg:      cfg,
		hostname: hostname,
		client:   client,
		repos:    repos,
		logger:   logger,
	}
}

// Run executes catalog reconciliation for the given cycle.
func (r *Reconciler) Run(ctx context.Context, cycleID string) {
	log := r.logger.WithFields("phase", "reconciliation", "cycle_id", cycleID)

	if !r.cfg.Enabled {
		return
	}

	// Step 1: Load all catalog_service_id values tracked by this ADC instance
	trackedIDs, err := r.repos.Unmanaged.TrackedCatalogServiceIDs(ctx)
	if err != nil {
		log.Errorw("Failed to load tracked catalog IDs", "error", err)
		return
	}

	// Step 2: List all entries from APIM Service Catalog (paginated)
	var allEntries []apim.ServiceListEntry
	const pageSize = 100
	offset := 0

	for {
		entries, total, err := r.client.ListServices(ctx, pageSize, offset)
		if err != nil {
			log.Errorw("Failed to list catalog services", "error", err, "offset", offset)
			return
		}
		allEntries = append(allEntries, entries...)
		offset += len(entries)
		if offset >= total || len(entries) == 0 {
			break
		}
	}

	// Step 3: Classify each entry
	var ours, otherADC, unowned, deleted int

	for _, entry := range allEntries {
		// Check if this entry is tracked in our database
		if trackedIDs[entry.ID] {
			ours++
			continue
		}

		// Parse ownership marker from description
		owner := parseOwner(entry.Description)

		if owner == r.hostname {
			// Ours but not in DB yet (possible race or first cycle)
			ours++
			continue
		}

		if owner != "" {
			// Owned by another ADC instance
			otherADC++
			log.Warnw("Catalog entry owned by another ADC instance",
				"entry_name", entry.Name,
				"entry_id", entry.ID,
				"owner", owner,
			)
			continue
		}

		// No ownership marker — unowned entry
		unowned++

		if r.cfg.CleanupUnowned {
			if err := r.client.DeleteService(ctx, entry.ID); err != nil {
				log.Errorw("Failed to delete unowned catalog entry",
					"entry_name", entry.Name,
					"entry_id", entry.ID,
					"error", err,
				)
				continue
			}
			deleted++
			log.Infow("Deleted unowned catalog entry",
				"entry_name", entry.Name,
				"entry_id", entry.ID,
				"entry_version", entry.Version,
			)
		} else {
			log.Warnw("Unowned catalog entry detected (cleanup_unowned=false, skipping)",
				"entry_name", entry.Name,
				"entry_id", entry.ID,
				"entry_version", entry.Version,
			)
		}
	}

	log.Infow("Catalog reconciliation complete",
		"total_entries", len(allEntries),
		"ours", ours,
		"other_adc", otherADC,
		"unowned", unowned,
		"deleted", deleted,
	)
}

// parseOwner extracts the hostname from an [ADC:hostname] marker in a description.
// Returns empty string if no marker is found.
func parseOwner(description string) string {
	matches := ownershipRegex.FindStringSubmatch(description)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// Name returns the reconciler name for logging.
func (r *Reconciler) Name() string { return "reconciliation" }

// RunAsPhase adapts Run to the Phase interface signature for use in the engine.
func (r *Reconciler) RunAsPhase(ctx context.Context, cycleID string) error {
	r.Run(ctx, cycleID)
	return nil
}

// reconcilerPhase wraps Reconciler as an engine.Phase.
type reconcilerPhase struct {
	reconciler *Reconciler
}

// NewReconcilerPhase creates a Phase adapter for the Reconciler.
func NewReconcilerPhase(rec *Reconciler) *reconcilerPhase {
	return &reconcilerPhase{reconciler: rec}
}

// Name returns the phase name.
func (rp *reconcilerPhase) Name() string { return "reconciliation" }

// Run executes reconciliation.
func (rp *reconcilerPhase) Run(ctx context.Context, cycleID string) error {
	rp.reconciler.Run(ctx, cycleID)
	return nil
}

// ReconcilerEnabled returns whether reconciliation is configured and enabled.
func ReconcilerEnabled(cfg config.ServiceCatalogConfig) bool {
	return cfg.Enabled && cfg.Reconciliation.Enabled
}

// BuildReconciler creates a Reconciler from config and dependencies.
// Returns nil if reconciliation is not enabled.
func BuildReconciler(cfg *config.Config, client apim.CatalogClient, repos *store.Repositories, logger *logging.Logger) *Reconciler {
	if !ReconcilerEnabled(cfg.ServiceCatalog) || client == nil {
		return nil
	}
	return NewReconciler(
		cfg.ServiceCatalog.Reconciliation,
		cfg.Server.Hostname,
		client,
		repos,
		logger,
	)
}

// FormatOwnershipMarker returns the ownership prefix for the given hostname.
func FormatOwnershipMarker(hostname string) string {
	return fmt.Sprintf("[ADC:%s]", hostname)
}
