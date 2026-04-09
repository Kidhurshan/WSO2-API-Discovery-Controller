package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wso2/adc/internal/models"
)

// PipelineStateRepo handles adc_pipeline_state CRUD operations.
type PipelineStateRepo struct {
	db *DB
}

// NewPipelineStateRepo creates a new PipelineStateRepo.
func NewPipelineStateRepo(db *DB) *PipelineStateRepo {
	return &PipelineStateRepo{db: db}
}

// Get retrieves the pipeline state for the given pipeline name.
// Returns nil if no state exists (first run).
func (r *PipelineStateRepo) Get(ctx context.Context, pipelineName string) (*models.PipelineState, error) {
	var state models.PipelineState
	err := r.db.Pool.QueryRow(ctx,
		`SELECT id, pipeline_name, last_watermark_end, last_processed_at, records_processed
		 FROM adc_pipeline_state WHERE pipeline_name = $1`,
		pipelineName,
	).Scan(&state.ID, &state.PipelineName, &state.LastWatermarkEnd, &state.LastProcessedAt, &state.RecordsProcessed)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pipeline state %s: %w", pipelineName, err)
	}
	return &state, nil
}

// Advance updates or creates the watermark for the given pipeline.
func (r *PipelineStateRepo) Advance(ctx context.Context, pipelineName string, watermarkEnd time.Time, recordsProcessed int64) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO adc_pipeline_state (pipeline_name, last_watermark_end, last_processed_at, records_processed)
		 VALUES ($1, $2, NOW(), $3)
		 ON CONFLICT (pipeline_name) DO UPDATE SET
		   last_watermark_end = EXCLUDED.last_watermark_end,
		   last_processed_at = NOW(),
		   records_processed = adc_pipeline_state.records_processed + EXCLUDED.records_processed`,
		pipelineName, watermarkEnd, recordsProcessed,
	)
	if err != nil {
		return fmt.Errorf("advance pipeline state %s: %w", pipelineName, err)
	}
	return nil
}
