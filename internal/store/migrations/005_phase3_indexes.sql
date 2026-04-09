-- Migration 005: Phase 3 indexes on discovered_apis for comparison
-- These indexes support the LEFT JOIN matching, collision detection, and affinity resolution.

-- Phase 3 LEFT JOIN: match discovered APIs by method + path
CREATE INDEX IF NOT EXISTS idx_discovered_method_path
  ON adc_discovered_apis(http_method, resource_path);

-- Phase 3 collision detection: group by method + path across service_keys
CREATE INDEX IF NOT EXISTS idx_discovered_collision
  ON adc_discovered_apis(http_method, resource_path, service_key);

-- Phase 3 service affinity: lookup all operations for a service_key
CREATE INDEX IF NOT EXISTS idx_discovered_service
  ON adc_discovered_apis(service_key);
