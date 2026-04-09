package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wso2/adc/internal/models"
)

// UnmanagedRepo handles adc_unmanaged_apis CRUD operations.
type UnmanagedRepo struct {
	db *DB
}

// NewUnmanagedRepo creates a new UnmanagedRepo.
func NewUnmanagedRepo(db *DB) *UnmanagedRepo {
	return &UnmanagedRepo{db: db}
}

// Upsert inserts or updates an unmanaged API entry.
// On conflict (discovered_api_id), it updates classification, confidence,
// managed_api_id, and handles status transitions per spec.
func (r *UnmanagedRepo) Upsert(ctx context.Context, u *models.UnmanagedAPI) error {
	sql := `
INSERT INTO adc_unmanaged_apis (
    service_key, http_method, resource_path,
    discovered_api_id, managed_api_id,
    classification, confidence, status,
    first_detected_at, last_confirmed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'DETECTED', NOW(), NOW())
ON CONFLICT (discovered_api_id) DO UPDATE SET
    last_confirmed_at = NOW(),
    updated_at = NOW(),
    classification = EXCLUDED.classification,
    confidence = EXCLUDED.confidence,
    managed_api_id = EXCLUDED.managed_api_id,
    status = CASE
        WHEN adc_unmanaged_apis.classification != EXCLUDED.classification THEN 'DETECTED'
        WHEN COALESCE(adc_unmanaged_apis.managed_api_id, -1) != COALESCE(EXCLUDED.managed_api_id, -1) THEN 'DETECTED'
        WHEN adc_unmanaged_apis.status IN ('RESOLVED', 'STALE') THEN 'DETECTED'
        ELSE adc_unmanaged_apis.status
    END,
    resolved_at = CASE
        WHEN adc_unmanaged_apis.classification != EXCLUDED.classification THEN NULL
        WHEN COALESCE(adc_unmanaged_apis.managed_api_id, -1) != COALESCE(EXCLUDED.managed_api_id, -1) THEN NULL
        WHEN adc_unmanaged_apis.status IN ('RESOLVED', 'STALE') THEN NULL
        ELSE adc_unmanaged_apis.resolved_at
    END`

	_, err := r.db.Pool.Exec(ctx, sql,
		u.ServiceKey, u.HTTPMethod, u.ResourcePath,
		u.DiscoveredAPIID, u.ManagedAPIID,
		string(u.Classification), string(u.Confidence),
	)
	if err != nil {
		return fmt.Errorf("upsert unmanaged API %s %s %s: %w",
			u.ServiceKey, u.HTTPMethod, u.ResourcePath, err)
	}
	return nil
}

// MarkResolved marks unmanaged APIs that were not confirmed this cycle as RESOLVED.
// cycleStart is the time when the current comparison cycle began.
func (r *UnmanagedRepo) MarkResolved(ctx context.Context, cycleStart time.Time) (int, error) {
	tag, err := r.db.Pool.Exec(ctx, `
		UPDATE adc_unmanaged_apis
		SET status = 'RESOLVED', resolved_at = NOW(), updated_at = NOW()
		WHERE status NOT IN ('DISMISSED', 'RESOLVED', 'STALE')
		  AND last_confirmed_at < $1`, cycleStart)
	if err != nil {
		return 0, fmt.Errorf("mark resolved: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// MarkStale marks unmanaged APIs where traffic has stopped (last_seen_at is older than staleAfter).
func (r *UnmanagedRepo) MarkStale(ctx context.Context, staleAfter time.Duration) (int, error) {
	tag, err := r.db.Pool.Exec(ctx, `
		UPDATE adc_unmanaged_apis u
		SET status = 'STALE', resolved_at = NOW(), updated_at = NOW()
		FROM adc_discovered_apis d
		WHERE u.discovered_api_id = d.id
		    AND u.status NOT IN ('DISMISSED', 'RESOLVED', 'STALE')
		    AND d.last_seen_at < NOW() - $1::interval`, fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("mark stale: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ServiceGroup represents a group of unmanaged ops needing spec generation.
type ServiceGroup struct {
	ServiceKey     string
	Classification string
	ManagedAPIID   *int
}

// GetDetectedGroups returns distinct service groups that have DETECTED operations.
func (r *UnmanagedRepo) GetDetectedGroups(ctx context.Context) ([]ServiceGroup, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT DISTINCT service_key, classification, managed_api_id
		FROM adc_unmanaged_apis
		WHERE status = 'DETECTED'
		ORDER BY service_key, classification`)
	if err != nil {
		return nil, fmt.Errorf("query detected groups: %w", err)
	}
	defer rows.Close()

	var groups []ServiceGroup
	for rows.Next() {
		var g ServiceGroup
		if err := rows.Scan(&g.ServiceKey, &g.Classification, &g.ManagedAPIID); err != nil {
			continue
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// OperationWithMetadata combines unmanaged + discovered + managed data for spec gen.
type OperationWithMetadata struct {
	UnmanagedID      int
	ServiceKey       string
	HTTPMethod       string
	ResourcePath     string
	Classification   string
	Confidence       string
	ServiceType      string
	Protocol         string
	IsTLS            bool
	HTTPVersion      string
	ServerPort       int
	ProcessName      string
	K8sNamespace     string
	K8sService       string
	K8sWorkload      string
	K8sCluster       string
	K8sPod           string
	K8sNode          string
	VMHostname       string
	RequestDomain    string
	ServiceIP        string
	HostIP           string
	TrafficOrigin    string
	SampleURL        string
	SamplePath       string
	AgentName        string
	ResponseCode     int
	LatencyUS        int64
	HitCount         int64
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
	ParentAPIName    string
	ParentAPIVersion string
}

// GetGroupOperations fetches all active operations for a service group with full metadata.
func (r *UnmanagedRepo) GetGroupOperations(ctx context.Context, g ServiceGroup) ([]OperationWithMetadata, error) {
	sql := `
SELECT
    u.id, u.service_key, u.http_method, u.resource_path,
    u.classification, u.confidence,
    COALESCE(d.service_type, ''), COALESCE(d.protocol, ''),
    COALESCE(d.is_tls, false), COALESCE(d.http_version, ''),
    COALESCE(d.server_port, 0), COALESCE(d.process_name, ''),
    COALESCE(d.k8s_namespace, ''), COALESCE(d.k8s_service, ''),
    COALESCE(d.k8s_workload, ''), COALESCE(d.k8s_cluster, ''),
    COALESCE(d.k8s_pod, ''), COALESCE(d.k8s_node, ''),
    COALESCE(d.vm_hostname, ''), COALESCE(d.request_domain, ''),
    COALESCE(d.service_ip, ''), COALESCE(d.host_ip, ''),
    COALESCE(d.traffic_origin, ''), COALESCE(d.sample_url, ''),
    COALESCE(d.sample_path, ''), COALESCE(d.agent_name, ''),
    COALESCE(d.response_code, 0), COALESCE(d.latency_us, 0),
    COALESCE(d.hit_count, 0),
    COALESCE(d.first_seen_at, NOW()), COALESCE(d.last_seen_at, NOW()),
    COALESCE(ma.api_name, ''), COALESCE(ma.api_version, '')
FROM adc_unmanaged_apis u
JOIN adc_discovered_apis d ON u.discovered_api_id = d.id
LEFT JOIN adc_managed_apis ma ON u.managed_api_id = ma.id
WHERE u.service_key = $1
  AND u.classification = $2
  AND ($3::INTEGER IS NULL OR u.managed_api_id = $3)
  AND u.status NOT IN ('DISMISSED', 'STALE', 'RESOLVED')
ORDER BY u.resource_path, u.http_method`

	rows, err := r.db.Pool.Query(ctx, sql, g.ServiceKey, g.Classification, g.ManagedAPIID)
	if err != nil {
		return nil, fmt.Errorf("query group ops for %s: %w", g.ServiceKey, err)
	}
	defer rows.Close()

	var ops []OperationWithMetadata
	for rows.Next() {
		var op OperationWithMetadata
		if err := rows.Scan(
			&op.UnmanagedID, &op.ServiceKey, &op.HTTPMethod, &op.ResourcePath,
			&op.Classification, &op.Confidence,
			&op.ServiceType, &op.Protocol, &op.IsTLS, &op.HTTPVersion,
			&op.ServerPort, &op.ProcessName,
			&op.K8sNamespace, &op.K8sService, &op.K8sWorkload, &op.K8sCluster,
			&op.K8sPod, &op.K8sNode,
			&op.VMHostname, &op.RequestDomain, &op.ServiceIP, &op.HostIP,
			&op.TrafficOrigin, &op.SampleURL, &op.SamplePath, &op.AgentName,
			&op.ResponseCode, &op.LatencyUS, &op.HitCount,
			&op.FirstSeenAt, &op.LastSeenAt,
			&op.ParentAPIName, &op.ParentAPIVersion,
		); err != nil {
			continue
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// UpdateGroupSpec stores the generated OAS spec and updates status to SPEC_GENERATED.
func (r *UnmanagedRepo) UpdateGroupSpec(ctx context.Context, g ServiceGroup, spec json.RawMessage) (int, error) {
	tag, err := r.db.Pool.Exec(ctx, `
		UPDATE adc_unmanaged_apis
		SET openapi_spec = $1::JSONB,
		    status = 'SPEC_GENERATED',
		    spec_generated_at = NOW(),
		    updated_at = NOW()
		WHERE service_key = $2
		  AND classification = $3
		  AND ($4::INTEGER IS NULL OR managed_api_id = $4)
		  AND status NOT IN ('DISMISSED', 'STALE', 'RESOLVED')`,
		spec, g.ServiceKey, g.Classification, g.ManagedAPIID)
	if err != nil {
		return 0, fmt.Errorf("update group spec for %s: %w", g.ServiceKey, err)
	}
	return int(tag.RowsAffected()), nil
}

// CatalogPushGroup represents a service group ready for catalog push.
type CatalogPushGroup struct {
	ServiceKey       string
	Classification   string
	ManagedAPIID     *int
	CatalogServiceID *string
	OpenAPISpec      json.RawMessage
}

// GetSpecGeneratedGroups returns distinct service groups with SPEC_GENERATED status.
func (r *UnmanagedRepo) GetSpecGeneratedGroups(ctx context.Context) ([]CatalogPushGroup, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT DISTINCT ON (service_key, classification, managed_api_id)
		    service_key, classification, managed_api_id,
		    catalog_service_id, openapi_spec
		FROM adc_unmanaged_apis
		WHERE status = 'SPEC_GENERATED'
		  AND openapi_spec IS NOT NULL
		ORDER BY service_key, classification, managed_api_id,
		         catalog_service_id NULLS LAST`)
	if err != nil {
		return nil, fmt.Errorf("query spec_generated groups: %w", err)
	}
	defer rows.Close()

	var groups []CatalogPushGroup
	for rows.Next() {
		var g CatalogPushGroup
		if err := rows.Scan(&g.ServiceKey, &g.Classification, &g.ManagedAPIID,
			&g.CatalogServiceID, &g.OpenAPISpec); err != nil {
			continue
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// MarkPushed updates a service group to PUSHED status with catalog metadata.
func (r *UnmanagedRepo) MarkPushed(ctx context.Context, g CatalogPushGroup, catalogServiceID, catalogVersion string) (int, error) {
	tag, err := r.db.Pool.Exec(ctx, `
		UPDATE adc_unmanaged_apis
		SET status = 'PUSHED',
		    pushed_at = NOW(),
		    catalog_service_id = $1,
		    catalog_version = $2,
		    updated_at = NOW()
		WHERE service_key = $3
		  AND classification = $4
		  AND ($5::INTEGER IS NULL OR managed_api_id = $5)
		  AND status NOT IN ('DISMISSED', 'STALE', 'RESOLVED')`,
		catalogServiceID, catalogVersion, g.ServiceKey, g.Classification, g.ManagedAPIID)
	if err != nil {
		return 0, fmt.Errorf("mark pushed for %s: %w", g.ServiceKey, err)
	}
	return int(tag.RowsAffected()), nil
}

// MarkPushedSkipped marks a group as PUSHED without changing catalog_service_id.
// Used when an operator-deleted catalog entry should not be re-created.
func (r *UnmanagedRepo) MarkPushedSkipped(ctx context.Context, g CatalogPushGroup) (int, error) {
	tag, err := r.db.Pool.Exec(ctx, `
		UPDATE adc_unmanaged_apis
		SET status = 'PUSHED',
		    updated_at = NOW()
		WHERE service_key = $1
		  AND classification = $2
		  AND ($3::INTEGER IS NULL OR managed_api_id = $3)
		  AND status = 'SPEC_GENERATED'`,
		g.ServiceKey, g.Classification, g.ManagedAPIID)
	if err != nil {
		return 0, fmt.Errorf("mark pushed skipped for %s: %w", g.ServiceKey, err)
	}
	return int(tag.RowsAffected()), nil
}

// TrackedCatalogServiceIDs returns the set of catalog_service_id values tracked by this ADC instance.
func (r *UnmanagedRepo) TrackedCatalogServiceIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT DISTINCT catalog_service_id
		FROM adc_unmanaged_apis
		WHERE catalog_service_id IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query tracked catalog IDs: %w", err)
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids[id] = true
	}
	return ids, nil
}

// CountByClassification returns counts grouped by classification for active (non-resolved) entries.
func (r *UnmanagedRepo) CountByClassification(ctx context.Context) (shadow, drift int, err error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT classification, COUNT(*)
		FROM adc_unmanaged_apis
		WHERE status NOT IN ('RESOLVED', 'STALE')
		GROUP BY classification`)
	if err != nil {
		return 0, 0, fmt.Errorf("count by classification: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cls string
		var count int
		if err := rows.Scan(&cls, &count); err != nil {
			continue
		}
		switch cls {
		case "SHADOW":
			shadow = count
		case "DRIFT":
			drift = count
		}
	}
	return shadow, drift, nil
}
