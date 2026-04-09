package store

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/wso2/adc/internal/logging"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate runs all pending SQL migrations in order.
func Migrate(ctx context.Context, db *DB, logger *logging.Logger) error {
	// Ensure schema version table exists (bootstrap — migration 001 creates it,
	// but we need it to track which migrations have run)
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS adc_schema_version (
			version    INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migration directory: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for i, entry := range entries {
		version := i + 1

		var applied bool
		err := db.Pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM adc_schema_version WHERE version = $1)", version,
		).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied {
			continue
		}

		sql, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction for migration %d: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", entry.Name(), err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO adc_schema_version (version) VALUES ($1) ON CONFLICT DO NOTHING", version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}

		logger.Infow("Migration applied", "version", version, "file", entry.Name())
	}

	return nil
}
