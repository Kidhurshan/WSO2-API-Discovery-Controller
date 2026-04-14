-- Migration 006: Add index on discovered_api_id FK in adc_unmanaged_apis.
-- Speeds up cleanup DELETE joins and Phase 3 lifecycle queries.

CREATE INDEX IF NOT EXISTS idx_unmanaged_discovered
    ON adc_unmanaged_apis(discovered_api_id);
