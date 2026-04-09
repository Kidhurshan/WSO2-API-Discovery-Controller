-- Migration 002: Discovered APIs table
-- Main catalog of APIs discovered from live traffic via DeepFlow.
-- Each row = unique (service_key, http_method, resource_path).

CREATE TABLE IF NOT EXISTS adc_discovered_apis (
    id                    SERIAL PRIMARY KEY,

    -- Catalog Key (unique identity)
    service_key           VARCHAR(255) NOT NULL,
    http_method           VARCHAR(10) NOT NULL,
    resource_path         VARCHAR(1000) NOT NULL,

    -- Service metadata (from s-p observation point)
    service_type          VARCHAR(20),
    protocol              VARCHAR(20),
    is_tls                BOOLEAN DEFAULT FALSE,
    http_version          VARCHAR(10),
    server_port           INTEGER,
    process_name          VARCHAR(255),
    k8s_pod               VARCHAR(255),
    k8s_namespace         VARCHAR(255),
    k8s_workload          VARCHAR(255),
    k8s_service           VARCHAR(255),
    k8s_cluster           VARCHAR(255),
    k8s_node              VARCHAR(255),
    vm_hostname           VARCHAR(255),
    request_domain        VARCHAR(255),
    service_ip            VARCHAR(45),

    -- Network metadata (from s observation point)
    source_ip             VARCHAR(45),
    host_ip               VARCHAR(45),
    is_external           BOOLEAN,

    -- Source (caller) metadata (from c-p observation point, nullable)
    source_service_ip     VARCHAR(45),
    source_process_name   VARCHAR(255),
    source_k8s_pod        VARCHAR(255),
    source_k8s_namespace  VARCHAR(255),
    source_k8s_service    VARCHAR(255),
    source_k8s_cluster    VARCHAR(255),
    source_vm_hostname    VARCHAR(255),

    -- Traffic classification (computed)
    traffic_origin        VARCHAR(30),

    -- Sample data (one representative request)
    sample_url            VARCHAR(2000),
    sample_path           VARCHAR(1000),

    -- Agent info
    agent_id              INTEGER,
    agent_name            VARCHAR(255),

    -- Response sample
    response_code         INTEGER,
    latency_us            BIGINT,

    -- Lifecycle timestamps
    first_seen_at         TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at          TIMESTAMPTZ DEFAULT NOW(),
    hit_count             BIGINT DEFAULT 0,
    created_at            TIMESTAMPTZ DEFAULT NOW(),
    updated_at            TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE (service_key, http_method, resource_path)
);

CREATE INDEX IF NOT EXISTS index_adc_discovered_service ON adc_discovered_apis(service_key);
CREATE INDEX IF NOT EXISTS index_adc_discovered_last_seen ON adc_discovered_apis(last_seen_at);
CREATE INDEX IF NOT EXISTS index_adc_discovered_method_path ON adc_discovered_apis(http_method, resource_path);
CREATE INDEX IF NOT EXISTS index_adc_discovered_collision ON adc_discovered_apis(http_method, resource_path, service_key);
