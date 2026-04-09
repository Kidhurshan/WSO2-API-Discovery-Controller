-- Migration 004: Unmanaged APIs table
-- Stores APIs detected as SHADOW or DRIFT by Phase 3.

CREATE TABLE IF NOT EXISTS adc_unmanaged_apis (
    id                    SERIAL PRIMARY KEY,

    -- Identity (denormalized from adc_discovered_apis)
    service_key           VARCHAR(255) NOT NULL,
    http_method           VARCHAR(10) NOT NULL,
    resource_path         VARCHAR(1000) NOT NULL,

    -- References
    discovered_api_id     INTEGER NOT NULL REFERENCES adc_discovered_apis(id),
    managed_api_id        INTEGER REFERENCES adc_managed_apis(id),

    -- Classification
    classification        VARCHAR(20) NOT NULL,
    confidence            VARCHAR(20) NOT NULL DEFAULT 'HIGH',

    -- Lifecycle
    status                VARCHAR(30) NOT NULL DEFAULT 'DETECTED',

    -- OpenAPI Specification (Phase 4 output)
    openapi_spec          JSONB,

    -- Service Catalog Tracking (Phase 5 output)
    catalog_service_id    VARCHAR(255),
    catalog_version       VARCHAR(20),

    -- Timestamps
    first_detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_confirmed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at           TIMESTAMPTZ,
    spec_generated_at     TIMESTAMPTZ,
    pushed_at             TIMESTAMPTZ,
    dismissed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ DEFAULT NOW(),
    updated_at            TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (discovered_api_id)
);

CREATE INDEX IF NOT EXISTS idx_unmanaged_needs_spec ON adc_unmanaged_apis(status)
  WHERE status = 'DETECTED';

CREATE INDEX IF NOT EXISTS idx_unmanaged_needs_push ON adc_unmanaged_apis(status)
  WHERE status = 'SPEC_GENERATED';

CREATE INDEX IF NOT EXISTS idx_unmanaged_service ON adc_unmanaged_apis(service_key, classification);

CREATE INDEX IF NOT EXISTS idx_unmanaged_managed ON adc_unmanaged_apis(managed_api_id)
  WHERE managed_api_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_unmanaged_lifecycle ON adc_unmanaged_apis(status, last_confirmed_at);
