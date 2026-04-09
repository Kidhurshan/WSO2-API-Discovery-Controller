-- Migration 003: Managed APIs + Operations tables
-- Stores APIs published in WSO2 APIM and their operations.

CREATE TABLE IF NOT EXISTS adc_managed_apis (
    id                    SERIAL PRIMARY KEY,

    -- APIM Identity
    apim_api_id           VARCHAR(255) UNIQUE NOT NULL,
    api_name              VARCHAR(255) NOT NULL,
    api_version           VARCHAR(50) NOT NULL,
    context               VARCHAR(500) NOT NULL,
    api_type              VARCHAR(50) DEFAULT 'HTTP',
    lifecycle_status      VARCHAR(50) NOT NULL,
    provider              VARCHAR(255),

    -- Endpoint Configuration
    endpoint_type         VARCHAR(50),
    endpoint_url          VARCHAR(2000),
    endpoint_hostname     VARCHAR(255),
    endpoint_port         INTEGER,
    endpoint_basepath     VARCHAR(500) DEFAULT '',

    -- Change Detection
    apim_last_updated_at  TIMESTAMPTZ,
    apim_created_at       TIMESTAMPTZ,

    -- Deletion Tracking
    deleted_at            TIMESTAMPTZ,

    -- ADC Timestamps
    last_synced_at        TIMESTAMPTZ DEFAULT NOW(),
    created_at            TIMESTAMPTZ DEFAULT NOW(),
    updated_at            TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_managed_active ON adc_managed_apis(apim_api_id)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_managed_by_id ON adc_managed_apis(id);

CREATE TABLE IF NOT EXISTS adc_managed_api_operations (
    id                    SERIAL PRIMARY KEY,

    -- Parent API Reference
    apim_api_id           VARCHAR(255) NOT NULL,

    -- Operation Identity
    http_method           VARCHAR(10) NOT NULL,
    raw_target            VARCHAR(1000) NOT NULL,
    normalized_target     VARCHAR(1000) NOT NULL,

    -- Computed Match Path (Phase 3 comparison key)
    match_path            VARCHAR(1500) NOT NULL,

    -- Deletion Tracking
    deleted_at            TIMESTAMPTZ,

    -- ADC Timestamps
    created_at            TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (apim_api_id, http_method, raw_target)
);

CREATE INDEX IF NOT EXISTS idx_managed_ops_match ON adc_managed_api_operations(http_method, match_path)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_managed_ops_api_id ON adc_managed_api_operations(apim_api_id);

CREATE INDEX IF NOT EXISTS idx_managed_ops_api_id_active ON adc_managed_api_operations(apim_api_id)
  WHERE deleted_at IS NULL;
