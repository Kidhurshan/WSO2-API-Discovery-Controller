package models

import "time"

// PipelineState tracks the sliding window watermark position for a pipeline.
type PipelineState struct {
	ID               int
	PipelineName     string
	LastWatermarkEnd time.Time
	LastProcessedAt  *time.Time
	RecordsProcessed int64
}
