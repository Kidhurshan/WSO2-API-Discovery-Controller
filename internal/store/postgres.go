// Package store handles all PostgreSQL database operations for ADC using the repository pattern.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
)

// DB wraps a pgx connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect creates a new PostgreSQL connection pool and verifies connectivity.
func Connect(ctx context.Context, cfg config.DatastoreConfig) (*DB, error) {
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&pool_max_conns=%d",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode, cfg.MaxConnections,
	)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// ConnectWithRetry attempts to connect to PostgreSQL with exponential backoff.
// It retries up to 30 times (~5 minutes total) before giving up.
func ConnectWithRetry(ctx context.Context, cfg config.DatastoreConfig, logger *logging.Logger) (*DB, error) {
	const maxAttempts = 30
	backoff := 2 * time.Second
	const multiplier = 1.5
	const maxBackoff = 30 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		db, err := Connect(ctx, cfg)
		if err == nil {
			if attempt > 1 {
				logger.Infow("PostgreSQL connected after retry", "attempt", attempt)
			}
			return db, nil
		}

		if attempt == maxAttempts {
			return nil, fmt.Errorf("PostgreSQL unavailable after %d attempts: %w", maxAttempts, err)
		}

		logger.Warnw("PostgreSQL not ready, retrying",
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"error", err,
			"next_backoff", backoff,
		)

		select {
		case <-time.After(backoff):
			backoff = time.Duration(float64(backoff) * multiplier)
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("shutdown during PostgreSQL retry: %w", ctx.Err())
		}
	}

	return nil, fmt.Errorf("PostgreSQL unavailable after %d attempts", maxAttempts)
}

// Close shuts down the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// Healthy checks if the database connection is alive.
func (db *DB) Healthy(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}
