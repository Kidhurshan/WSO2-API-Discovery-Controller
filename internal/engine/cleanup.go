package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

// Cleanup performs daily data retention cleanup.
type Cleanup struct {
	cfg    config.RetentionConfig
	db     *store.DB
	logger *logging.Logger
}

// NewCleanup creates a new Cleanup.
func NewCleanup(cfg config.RetentionConfig, db *store.DB, logger *logging.Logger) *Cleanup {
	return &Cleanup{cfg: cfg, db: db, logger: logger}
}

// Run executes the cleanup operations.
func (c *Cleanup) Run(ctx context.Context, cycleID string) {
	log := c.logger.WithFields("phase", "cleanup", "cycle_id", cycleID)
	start := time.Now()

	discoveredRetention := parseDuration(c.cfg.DiscoveredRetention, 30)
	managedRetention := parseDuration(c.cfg.ManagedAPIRetention, 90)
	unmanagedRetention := parseDuration(c.cfg.UnmanagedRetention, 90)

	// 1. Purge stale discovered APIs (no active unmanaged references)
	discoveredPurged, err := c.purgeDiscovered(ctx, discoveredRetention)
	if err != nil {
		log.Errorw("Failed to purge discovered APIs", "error", err)
	}

	// 2. Purge old deleted managed API operations
	managedOpsPurged, err := c.purgeManagedOps(ctx, managedRetention)
	if err != nil {
		log.Errorw("Failed to purge managed operations", "error", err)
	}

	// 3. Purge old deleted managed APIs
	managedPurged, err := c.purgeManaged(ctx, managedRetention)
	if err != nil {
		log.Errorw("Failed to purge managed APIs", "error", err)
	}

	// 4. Purge old resolved/stale/dismissed unmanaged entries
	unmanagedPurged, err := c.purgeUnmanaged(ctx, unmanagedRetention)
	if err != nil {
		log.Errorw("Failed to purge unmanaged APIs", "error", err)
	}

	// 5. VACUUM ANALYZE
	c.vacuum(ctx, log)

	log.Infow("Daily cleanup completed",
		"discovered_purged", discoveredPurged,
		"managed_purged", managedPurged,
		"managed_ops_purged", managedOpsPurged,
		"unmanaged_purged", unmanagedPurged,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func (c *Cleanup) purgeDiscovered(ctx context.Context, days int) (int, error) {
	tag, err := c.db.Pool.Exec(ctx, `
		DELETE FROM adc_discovered_apis d
		WHERE d.last_seen_at < NOW() - $1::interval
		  AND NOT EXISTS (
		      SELECT 1 FROM adc_unmanaged_apis u
		      WHERE u.discovered_api_id = d.id
		        AND u.status NOT IN ('RESOLVED', 'STALE', 'DISMISSED')
		  )`, fmt.Sprintf("%d days", days))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (c *Cleanup) purgeManagedOps(ctx context.Context, days int) (int, error) {
	tag, err := c.db.Pool.Exec(ctx, `
		DELETE FROM adc_managed_api_operations
		WHERE deleted_at IS NOT NULL
		  AND deleted_at < NOW() - $1::interval`, fmt.Sprintf("%d days", days))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (c *Cleanup) purgeManaged(ctx context.Context, days int) (int, error) {
	tag, err := c.db.Pool.Exec(ctx, `
		DELETE FROM adc_managed_apis
		WHERE deleted_at IS NOT NULL
		  AND deleted_at < NOW() - $1::interval`, fmt.Sprintf("%d days", days))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (c *Cleanup) purgeUnmanaged(ctx context.Context, days int) (int, error) {
	tag, err := c.db.Pool.Exec(ctx, `
		DELETE FROM adc_unmanaged_apis
		WHERE status IN ('RESOLVED', 'STALE', 'DISMISSED')
		  AND updated_at < NOW() - $1::interval`, fmt.Sprintf("%d days", days))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (c *Cleanup) vacuum(ctx context.Context, log *logging.Logger) {
	tables := []string{
		"adc_discovered_apis",
		"adc_managed_apis",
		"adc_managed_api_operations",
		"adc_unmanaged_apis",
	}
	for _, t := range tables {
		if _, err := c.db.Pool.Exec(ctx, "VACUUM ANALYZE "+t); err != nil {
			log.Warnw("VACUUM failed", "table", t, "error", err)
		}
	}
}

// parseDuration parses "30d" format to number of days.
func parseDuration(s string, defaultDays int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultDays
	}
	s = strings.TrimSuffix(s, "d")
	days, err := strconv.Atoi(s)
	if err != nil || days <= 0 {
		return defaultDays
	}
	return days
}
