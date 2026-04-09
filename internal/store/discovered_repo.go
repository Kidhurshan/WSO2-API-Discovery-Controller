package store

import (
	"context"
	"fmt"

	"github.com/wso2/adc/internal/models"
)

// DiscoveredRepo handles adc_discovered_apis CRUD operations.
type DiscoveredRepo struct {
	db *DB
}

// NewDiscoveredRepo creates a new DiscoveredRepo.
func NewDiscoveredRepo(db *DB) *DiscoveredRepo {
	return &DiscoveredRepo{db: db}
}

// BatchUpsert inserts or updates discovered API records.
// Returns the number of records upserted.
func (r *DiscoveredRepo) BatchUpsert(ctx context.Context, records []*models.FusedRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}

	count := 0
	for _, rec := range records {
		err := r.upsertOne(ctx, rec)
		if err != nil {
			// Log and continue — don't fail the entire batch
			continue
		}
		count++
	}

	return count, nil
}

func (r *DiscoveredRepo) upsertOne(ctx context.Context, rec *models.FusedRecord) error {
	sql := `
INSERT INTO adc_discovered_apis (
    service_key, http_method, resource_path,
    service_type, protocol, is_tls, http_version,
    server_port, process_name,
    k8s_pod, k8s_namespace, k8s_workload,
    k8s_service, k8s_cluster, k8s_node,
    vm_hostname, request_domain, service_ip,
    source_ip, host_ip, is_external,
    source_service_ip, source_process_name,
    source_k8s_pod, source_k8s_namespace, source_k8s_service,
    source_k8s_cluster, source_vm_hostname,
    traffic_origin, sample_url, sample_path,
    agent_id, agent_name, response_code, latency_us,
    first_seen_at, last_seen_at, hit_count
) VALUES (
    $1, $2, $3,
    $4, $5, $6, $7,
    $8, $9,
    $10, $11, $12,
    $13, $14, $15,
    $16, $17, $18,
    $19, $20, $21,
    $22, $23,
    $24, $25, $26,
    $27, $28,
    $29, $30, $31,
    $32, $33, $34, $35,
    NOW(), NOW(), $36
)
ON CONFLICT (service_key, http_method, resource_path) DO UPDATE SET
    last_seen_at = NOW(),
    hit_count = adc_discovered_apis.hit_count + EXCLUDED.hit_count,
    updated_at = NOW(),
    k8s_pod = EXCLUDED.k8s_pod,
    k8s_node = EXCLUDED.k8s_node,
    service_ip = EXCLUDED.service_ip,
    source_ip = COALESCE(EXCLUDED.source_ip, adc_discovered_apis.source_ip),
    host_ip = COALESCE(EXCLUDED.host_ip, adc_discovered_apis.host_ip),
    is_external = COALESCE(EXCLUDED.is_external, adc_discovered_apis.is_external),
    source_service_ip = COALESCE(EXCLUDED.source_service_ip, adc_discovered_apis.source_service_ip),
    source_k8s_pod = COALESCE(EXCLUDED.source_k8s_pod, adc_discovered_apis.source_k8s_pod),
    source_k8s_service = COALESCE(EXCLUDED.source_k8s_service, adc_discovered_apis.source_k8s_service),
    traffic_origin = EXCLUDED.traffic_origin,
    sample_url = EXCLUDED.sample_url,
    response_code = EXCLUDED.response_code,
    latency_us = EXCLUDED.latency_us,
    agent_name = EXCLUDED.agent_name`

	var isExternal *bool
	if rec.IsExternal != nil {
		isExternal = rec.IsExternal
	}

	_, err := r.db.Pool.Exec(ctx, sql,
		rec.ServiceKey, rec.HTTPMethod, rec.ResourcePath,
		string(rec.ServiceType), rec.Protocol, rec.IsTLS, rec.HTTPVersion,
		rec.ServerPort, rec.ProcessName,
		rec.K8sPod, rec.K8sNamespace, rec.K8sWorkload,
		rec.K8sService, rec.K8sCluster, rec.K8sNode,
		rec.VMHostname, rec.RequestDomain, rec.ServiceIP,
		nullIfEmpty(rec.SourceIP), nullIfEmpty(rec.HostIP), isExternal,
		nullIfEmpty(rec.SourceServiceIP), nullIfEmpty(rec.SourceProcessName),
		nullIfEmpty(rec.SourceK8sPod), nullIfEmpty(rec.SourceK8sNamespace), nullIfEmpty(rec.SourceK8sService),
		nullIfEmpty(rec.SourceK8sCluster), nullIfEmpty(rec.SourceVMHostname),
		string(rec.TrafficOrigin), rec.SampleURL, rec.SamplePath,
		rec.AgentID, rec.AgentName, rec.ResponseCode, rec.LatencyUs,
		rec.HitCount,
	)
	if err != nil {
		return fmt.Errorf("upsert discovered API %s %s %s: %w",
			rec.ServiceKey, rec.HTTPMethod, rec.ResourcePath, err)
	}
	return nil
}

// GetAll returns all discovered APIs (for Phase 3 comparison).
func (r *DiscoveredRepo) GetAll(ctx context.Context) ([]models.DiscoveredAPI, error) {
	sql := `SELECT id, service_key, http_method, resource_path,
	               COALESCE(request_domain, ''), COALESCE(host_ip, ''),
	               server_port, hit_count
	        FROM adc_discovered_apis
	        ORDER BY service_key, http_method, resource_path`

	rows, err := r.db.Pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query all discovered APIs: %w", err)
	}
	defer rows.Close()

	var apis []models.DiscoveredAPI
	for rows.Next() {
		var d models.DiscoveredAPI
		if err := rows.Scan(&d.ID, &d.ServiceKey, &d.HTTPMethod, &d.ResourcePath,
			&d.RequestDomain, &d.HostIP, &d.ServerPort, &d.HitCount); err != nil {
			continue
		}
		apis = append(apis, d)
	}
	return apis, nil
}

// nullIfEmpty returns nil for empty strings to store NULL in PostgreSQL.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
