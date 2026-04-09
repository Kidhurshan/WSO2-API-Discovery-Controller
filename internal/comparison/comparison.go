package comparison

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/models"
	"github.com/wso2/adc/internal/store"
)

// Phase implements engine.Phase for unmanaged API detection.
type Phase struct {
	cfg    *config.Config
	repos  *store.Repositories
	logger *logging.Logger
}

// New creates a new comparison Phase.
func New(cfg *config.Config, repos *store.Repositories, logger *logging.Logger) *Phase {
	return &Phase{
		cfg:    cfg,
		repos:  repos,
		logger: logger,
	}
}

// Name returns the phase name.
func (p *Phase) Name() string { return "comparison" }

// Run executes the Phase 3 unmanaged API detection pipeline.
func (p *Phase) Run(ctx context.Context, cycleID string) error {
	log := p.logger.WithFields("phase", "comparison", "cycle_id", cycleID)
	cycleStart := time.Now()

	// Step 7: Load data for comparison
	discovered, err := p.repos.Discovered.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("step 7 load discovered: %w", err)
	}
	if len(discovered) == 0 {
		log.Infow("No discovered APIs, skipping comparison")
		return nil
	}

	managedOps, err := p.repos.ManagedOps.GetAllActive(ctx)
	if err != nil {
		return fmt.Errorf("step 7 load managed ops: %w", err)
	}

	log.Infow("Step 7: Data loaded",
		"discovered", len(discovered),
		"managed_ops", len(managedOps),
	)

	// Step 7: Primary path matching (LEFT JOIN)
	candidates := Match(discovered, managedOps)

	// Count managed vs unmanaged
	var managedCount, unmanagedCount int
	for _, c := range candidates {
		if c.ManagedOpID != nil {
			managedCount++
		} else {
			unmanagedCount++
		}
	}
	log.Infow("Step 7: Path matching complete",
		"managed", managedCount,
		"unmanaged", unmanagedCount,
	)

	// Step 8: Collision detection
	collisions := DetectCollisions(candidates)
	log.Infow("Step 8: Collision detection",
		"collisions", len(collisions),
	)

	// Step 9: Service affinity resolution
	endpointHostnameFn := func(managedAPIPK int) string {
		hostname, err := p.repos.Managed.GetEndpointHostname(ctx, managedAPIPK)
		if err != nil {
			log.Warnw("Failed to get endpoint hostname", "managed_api_id", managedAPIPK, "error", err)
			return ""
		}
		return hostname
	}

	reclassified, ambiguousIDs := ResolveCollisions(collisions, candidates, managedOps, endpointHostnameFn, log)

	// Step 10: Classify unmanaged APIs (SHADOW vs DRIFT)
	classifications := Classify(candidates, reclassified, ambiguousIDs)

	var shadowCount, driftCount int
	for _, cr := range classifications {
		switch cr.Classification {
		case models.ClassificationShadow:
			shadowCount++
		case models.ClassificationDrift:
			driftCount++
		}
	}
	log.Infow("Step 10: Classification complete",
		"shadow", shadowCount,
		"drift", driftCount,
	)

	// Step 11: Persist unmanaged APIs
	var upserted, upsertErrors int
	for _, cr := range classifications {
		u := &models.UnmanagedAPI{
			ServiceKey:      cr.ServiceKey,
			HTTPMethod:      cr.HTTPMethod,
			ResourcePath:    cr.ResourcePath,
			DiscoveredAPIID: cr.DiscoveredID,
			ManagedAPIID:    cr.ManagedAPIID,
			Classification:  cr.Classification,
			Confidence:      cr.Confidence,
		}
		if err := p.repos.Unmanaged.Upsert(ctx, u); err != nil {
			log.Warnw("Failed to upsert unmanaged API",
				"service_key", cr.ServiceKey,
				"method", cr.HTTPMethod,
				"path", cr.ResourcePath,
				"error", err,
			)
			upsertErrors++
			continue
		}
		upserted++
	}
	log.Infow("Step 11: Persist complete",
		"upserted", upserted,
		"errors", upsertErrors,
	)

	// Step 12: Lifecycle management — mark resolved
	resolved, err := p.repos.Unmanaged.MarkResolved(ctx, cycleStart)
	if err != nil {
		log.Warnw("Failed to mark resolved", "error", err)
	} else if resolved > 0 {
		log.Infow("Step 12: Resolved stale entries", "resolved", resolved)
	}

	// Step 12: Stale detection
	staleAfter := parseDuration(p.cfg.Comparison.StaleAfter, 7*24*time.Hour)
	stale, err := p.repos.Unmanaged.MarkStale(ctx, staleAfter)
	if err != nil {
		log.Warnw("Failed to mark stale", "error", err)
	} else if stale > 0 {
		log.Infow("Step 12: Stale detection", "stale", stale)
	}

	log.Infow("Comparison complete",
		"total_discovered", len(discovered),
		"managed", managedCount,
		"unmanaged", len(classifications),
		"shadow", shadowCount,
		"drift", driftCount,
	)

	return nil
}

// parseDuration parses a duration string like "7d" or "24h".
func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}

	// Handle "Nd" (days) format
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour
		}
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
