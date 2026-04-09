-- Migration 001: Pipeline state tracking + schema version
-- Tracks sliding window watermark position for each pipeline.

CREATE TABLE IF NOT EXISTS adc_schema_version (
    version     INTEGER PRIMARY KEY,
    applied_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS adc_pipeline_state (
    id                 SERIAL PRIMARY KEY,
    pipeline_name      VARCHAR(100) UNIQUE NOT NULL,
    last_watermark_end TIMESTAMPTZ NOT NULL,
    last_processed_at  TIMESTAMPTZ,
    records_processed  BIGINT DEFAULT 0
);
