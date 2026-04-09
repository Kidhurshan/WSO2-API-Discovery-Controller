package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/wso2/adc/internal/models"
)

// ManagedRepo handles adc_managed_apis CRUD operations.
type ManagedRepo struct {
	db *DB
}

// NewManagedRepo creates a new ManagedRepo.
func NewManagedRepo(db *DB) *ManagedRepo {
	return &ManagedRepo{db: db}
}

// GetByAPIMApiID looks up a managed API by its APIM API ID.
func (r *ManagedRepo) GetByAPIMApiID(ctx context.Context, apimApiID string) (*models.ManagedAPI, error) {
	sql := `SELECT id, apim_api_id, api_name, api_version, context, api_type,
	               lifecycle_status, provider,
	               endpoint_type, endpoint_url, endpoint_hostname, endpoint_port, endpoint_basepath,
	               apim_last_updated_at, apim_created_at, deleted_at, last_synced_at
	        FROM adc_managed_apis WHERE apim_api_id = $1`

	row := r.db.Pool.QueryRow(ctx, sql, apimApiID)
	api := &models.ManagedAPI{}
	err := row.Scan(
		&api.ID, &api.APIMApiID, &api.APIName, &api.APIVersion, &api.Context, &api.APIType,
		&api.LifecycleStatus, &api.Provider,
		&api.EndpointType, &api.EndpointURL, &api.EndpointHostname, &api.EndpointPort, &api.EndpointBasepath,
		&api.APIMLastUpdatedAt, &api.APIMCreatedAt, &api.DeletedAt, &api.LastSyncedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get managed API %s: %w", apimApiID, err)
	}
	return api, nil
}

// UpsertAPI inserts or updates a managed API and its operations in a transaction.
func (r *ManagedRepo) UpsertAPI(ctx context.Context, api *models.ManagedAPI, ops []models.ManagedAPIOperation) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Upsert parent API
	upsertSQL := `
INSERT INTO adc_managed_apis (
    apim_api_id, api_name, api_version, context, api_type,
    lifecycle_status, provider,
    endpoint_type, endpoint_url, endpoint_hostname, endpoint_port, endpoint_basepath,
    apim_last_updated_at, apim_created_at, last_synced_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW())
ON CONFLICT (apim_api_id) DO UPDATE SET
    api_name = EXCLUDED.api_name,
    api_version = EXCLUDED.api_version,
    context = EXCLUDED.context,
    lifecycle_status = EXCLUDED.lifecycle_status,
    endpoint_type = EXCLUDED.endpoint_type,
    endpoint_url = EXCLUDED.endpoint_url,
    endpoint_hostname = EXCLUDED.endpoint_hostname,
    endpoint_port = EXCLUDED.endpoint_port,
    endpoint_basepath = EXCLUDED.endpoint_basepath,
    apim_last_updated_at = EXCLUDED.apim_last_updated_at,
    deleted_at = NULL,
    last_synced_at = NOW(),
    updated_at = NOW()`

	_, err = tx.Exec(ctx, upsertSQL,
		api.APIMApiID, api.APIName, api.APIVersion, api.Context, api.APIType,
		api.LifecycleStatus, api.Provider,
		api.EndpointType, api.EndpointURL, api.EndpointHostname, api.EndpointPort, api.EndpointBasepath,
		api.APIMLastUpdatedAt, api.APIMCreatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert managed API %s: %w", api.APIMApiID, err)
	}

	// Delete old operations and insert new ones
	_, err = tx.Exec(ctx, "DELETE FROM adc_managed_api_operations WHERE apim_api_id = $1", api.APIMApiID)
	if err != nil {
		return fmt.Errorf("delete old operations for %s: %w", api.APIMApiID, err)
	}

	insertOpSQL := `INSERT INTO adc_managed_api_operations (
	    apim_api_id, http_method, raw_target, normalized_target, match_path
	) VALUES ($1, $2, $3, $4, $5)`

	for _, op := range ops {
		_, err = tx.Exec(ctx, insertOpSQL,
			op.APIMApiID, op.HTTPMethod, op.RawTarget, op.NormalizedTarget, op.MatchPath,
		)
		if err != nil {
			return fmt.Errorf("insert operation %s %s for %s: %w", op.HTTPMethod, op.RawTarget, api.APIMApiID, err)
		}
	}

	return tx.Commit(ctx)
}

// UpdateLastSynced updates only the last_synced_at timestamp for an unchanged API.
func (r *ManagedRepo) UpdateLastSynced(ctx context.Context, id int) error {
	_, err := r.db.Pool.Exec(ctx,
		"UPDATE adc_managed_apis SET last_synced_at = NOW() WHERE id = $1", id)
	return err
}

// MarkDeleted soft-deletes APIs that are no longer in the current APIM list.
// Returns the number of APIs marked as deleted.
func (r *ManagedRepo) MarkDeleted(ctx context.Context, currentAPIIDs map[string]bool) (int, error) {
	// Get all active API IDs from the database
	rows, err := r.db.Pool.Query(ctx,
		"SELECT apim_api_id FROM adc_managed_apis WHERE deleted_at IS NULL")
	if err != nil {
		return 0, fmt.Errorf("query active APIs: %w", err)
	}
	defer rows.Close()

	var toDelete []string
	for rows.Next() {
		var apimID string
		if err := rows.Scan(&apimID); err != nil {
			continue
		}
		if !currentAPIIDs[apimID] {
			toDelete = append(toDelete, apimID)
		}
	}

	if len(toDelete) == 0 {
		return 0, nil
	}

	// Soft-delete parent APIs
	tag, err := r.db.Pool.Exec(ctx,
		"UPDATE adc_managed_apis SET deleted_at = NOW(), updated_at = NOW() WHERE apim_api_id = ANY($1) AND deleted_at IS NULL",
		toDelete)
	if err != nil {
		return 0, fmt.Errorf("mark APIs deleted: %w", err)
	}

	// Cascade to operations
	_, err = r.db.Pool.Exec(ctx,
		"UPDATE adc_managed_api_operations SET deleted_at = NOW() WHERE apim_api_id = ANY($1) AND deleted_at IS NULL",
		toDelete)
	if err != nil {
		return 0, fmt.Errorf("mark operations deleted: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

// GetEndpointHostname returns the endpoint hostname for a managed API by its PK.
func (r *ManagedRepo) GetEndpointHostname(ctx context.Context, id int) (string, error) {
	var hostname string
	err := r.db.Pool.QueryRow(ctx,
		"SELECT COALESCE(endpoint_hostname, '') FROM adc_managed_apis WHERE id = $1", id).Scan(&hostname)
	if err != nil {
		return "", fmt.Errorf("get endpoint hostname for managed API %d: %w", id, err)
	}
	return hostname, nil
}

// CountActive returns the number of active (non-deleted) managed APIs.
func (r *ManagedRepo) CountActive(ctx context.Context) (int, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM adc_managed_apis WHERE deleted_at IS NULL").Scan(&count)
	return count, err
}
