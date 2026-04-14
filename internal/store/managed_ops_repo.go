package store

import (
	"context"
	"fmt"

	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/models"
)

// ManagedOpsRepo handles adc_managed_api_operations queries.
type ManagedOpsRepo struct {
	db     *DB
	logger *logging.Logger
}

// NewManagedOpsRepo creates a new ManagedOpsRepo.
func NewManagedOpsRepo(db *DB, logger *logging.Logger) *ManagedOpsRepo {
	return &ManagedOpsRepo{db: db, logger: logger}
}

// GetByAPIMApiID returns all active operations for a given APIM API ID.
func (r *ManagedOpsRepo) GetByAPIMApiID(ctx context.Context, apimApiID string) ([]models.ManagedAPIOperation, error) {
	sql := `SELECT id, apim_api_id, http_method, raw_target, normalized_target, match_path
	        FROM adc_managed_api_operations
	        WHERE apim_api_id = $1 AND deleted_at IS NULL`

	rows, err := r.db.Pool.Query(ctx, sql, apimApiID)
	if err != nil {
		return nil, fmt.Errorf("query ops for %s: %w", apimApiID, err)
	}
	defer rows.Close()

	var ops []models.ManagedAPIOperation
	for rows.Next() {
		var op models.ManagedAPIOperation
		if err := rows.Scan(&op.ID, &op.APIMApiID, &op.HTTPMethod, &op.RawTarget, &op.NormalizedTarget, &op.MatchPath); err != nil {
			r.logger.Warnw("row scan error in GetByAPIMApiID, skipping", "error", err)
			continue
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration in GetByAPIMApiID: %w", err)
	}
	return ops, nil
}

// GetAllActive returns all active (non-deleted) managed operations.
func (r *ManagedOpsRepo) GetAllActive(ctx context.Context) ([]models.ManagedAPIOperation, error) {
	sql := `SELECT mo.id, mo.apim_api_id, mo.http_method, mo.raw_target,
	               mo.normalized_target, mo.match_path, ma.id AS managed_api_pk
	        FROM adc_managed_api_operations mo
	        JOIN adc_managed_apis ma ON mo.apim_api_id = ma.apim_api_id AND ma.deleted_at IS NULL
	        WHERE mo.deleted_at IS NULL
	        ORDER BY mo.apim_api_id, mo.http_method`

	rows, err := r.db.Pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query all active managed ops: %w", err)
	}
	defer rows.Close()

	var ops []models.ManagedAPIOperation
	for rows.Next() {
		var op models.ManagedAPIOperation
		var managedAPIPK int
		if err := rows.Scan(&op.ID, &op.APIMApiID, &op.HTTPMethod, &op.RawTarget,
			&op.NormalizedTarget, &op.MatchPath, &managedAPIPK); err != nil {
			r.logger.Warnw("row scan error in GetAllActive, skipping", "error", err)
			continue
		}
		op.ManagedAPIPK = managedAPIPK
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration in GetAllActive: %w", err)
	}
	return ops, nil
}

// CountAll returns total active operations count.
func (r *ManagedOpsRepo) CountAll(ctx context.Context) (int, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM adc_managed_api_operations WHERE deleted_at IS NULL").Scan(&count)
	return count, err
}
