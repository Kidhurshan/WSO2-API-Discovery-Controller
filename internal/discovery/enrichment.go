package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/deepflow"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/models"
)

// enrich performs Step 5: observation point fusion and service_key computation.
func (p *Phase) enrich(ctx context.Context, signatures []models.APISignature, log *logging.Logger) ([]*models.FusedRecord, error) {
	if len(signatures) == 0 {
		return nil, nil
	}

	// Build fusion map
	type fusionKey struct {
		SampleURL  string
		HTTPMethod string
		StartTime  time.Time
	}

	fusionMap := make(map[int]*models.FusedRecord, len(signatures))
	for i := range signatures {
		sig := &signatures[i]
		rec := &models.FusedRecord{
			NormalizedSignature: models.NormalizedSignature{
				HTTPMethod:    sig.HTTPMethod,
				ResourcePath:  sig.ResourcePath,
				ServerPort:    sig.ServerPort,
				AgentID:       sig.AgentID,
				HitCount:      sig.HitCount,
				SampleURL:     sig.SampleURL,
				SamplePath:    sig.SamplePath,
				RequestDomain: sig.RequestDomain,
				IsTLS:         sig.IsTLS,
				Protocol:      sig.Protocol,
				HTTPVersion:   sig.HTTPVersion,
				ResponseCode:  sig.ResponseCode,
				LatencyUs:     sig.LatencyUs,
				StartTime:     sig.StartTime,
			},
		}
		fusionMap[i] = rec
	}

	// Collect sample URLs and time range for batch queries
	sampleURLs := make([]string, 0, len(signatures))
	var minTime, maxTime time.Time
	for _, sig := range signatures {
		sampleURLs = append(sampleURLs, sig.SampleURL)
		if minTime.IsZero() || sig.StartTime.Before(minTime) {
			minTime = sig.StartTime
		}
		if maxTime.IsZero() || sig.StartTime.After(maxTime) {
			maxTime = sig.StartTime
		}
	}

	tz := p.cfg.Discovery.Source.ClickHouse.Timezone
	if tz == "" {
		tz = "UTC"
	}
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}

	timeStart := minTime.Add(-1 * time.Second).In(loc).Format("2006-01-02 15:04:05")
	timeEnd := maxTime.Add(1 * time.Second).In(loc).Format("2006-01-02 15:04:05")
	urlFilter := buildURLFilter(sampleURLs)

	// Step 5a: Query s-p records
	spRows, err := querySP(ctx, p.client, urlFilter, timeStart, timeEnd)
	if err != nil {
		log.Errorw("Step 5a s-p query failed", "error", err)
	} else {
		attachSP(fusionMap, signatures, spRows)
	}

	// Step 5b: Query s records
	sRows, err := queryS(ctx, p.client, urlFilter, timeStart, timeEnd)
	if err != nil {
		log.Warnw("Step 5b s query failed, proceeding without network data", "error", err)
	} else {
		attachS(fusionMap, signatures, sRows)
	}

	// Step 5c: Query c-p records
	cpRows, err := queryCP(ctx, p.client, urlFilter, timeStart, timeEnd)
	if err != nil {
		log.Warnw("Step 5c c-p query failed, proceeding without caller data", "error", err)
	} else {
		attachCP(fusionMap, signatures, cpRows)
	}

	// Compute derived fields
	records := make([]*models.FusedRecord, 0, len(fusionMap))
	for _, rec := range fusionMap {
		rec.ServiceKey = ComputeServiceKey(rec)
		rec.ServiceType = ComputeServiceType(rec)
		rec.TrafficOrigin = ComputeTrafficOrigin(rec)
		records = append(records, rec)
	}

	return records, nil
}

func querySP(ctx context.Context, client deepflow.Client, urlFilter, timeStart, timeEnd string) ([]map[string]interface{}, error) {
	sql := fmt.Sprintf(`SELECT
    request_resource, request_type, start_time,
    request_domain, endpoint, l7_protocol_str, is_tls, version,
    response_code, response_duration, server_port, agent_id,
    ip4_1 AS server_ip, pod_service_1 AS k8s_service, pod_ns_1 AS k8s_namespace,
    pod_group_1 AS k8s_workload, pod_1 AS k8s_pod, pod_cluster_1 AS k8s_cluster,
    pod_node_1 AS k8s_node, chost_1 AS vm_hostname, process_kname_1 AS process_name
FROM l7_flow_log
WHERE observation_point = "s-p"
  AND request_resource IN (%s)
  AND start_time >= "%s"
  AND start_time <= "%s"`, urlFilter, timeStart, timeEnd)

	return client.Query(ctx, sql)
}

func queryS(ctx context.Context, client deepflow.Client, urlFilter, timeStart, timeEnd string) ([]map[string]interface{}, error) {
	sql := fmt.Sprintf(`SELECT
    request_resource, request_type, start_time,
    ip4_0 AS source_ip, ip4_1 AS host_ip,
    is_internet_0 AS is_external
FROM l7_flow_log
WHERE observation_point = "s"
  AND request_resource IN (%s)
  AND start_time >= "%s"
  AND start_time <= "%s"`, urlFilter, timeStart, timeEnd)

	return client.Query(ctx, sql)
}

func queryCP(ctx context.Context, client deepflow.Client, urlFilter, timeStart, timeEnd string) ([]map[string]interface{}, error) {
	sql := fmt.Sprintf(`SELECT
    request_resource, request_type, start_time,
    ip4_0 AS source_service_ip, process_kname_0 AS source_process_name,
    pod_0 AS source_k8s_pod, pod_ns_0 AS source_k8s_namespace,
    pod_service_0 AS source_k8s_service, pod_cluster_0 AS source_k8s_cluster,
    chost_0 AS source_vm_hostname
FROM l7_flow_log
WHERE observation_point = "c-p"
  AND request_resource IN (%s)
  AND start_time >= "%s"
  AND start_time <= "%s"`, urlFilter, timeStart, timeEnd)

	return client.Query(ctx, sql)
}

func attachSP(fusionMap map[int]*models.FusedRecord, signatures []models.APISignature, rows []map[string]interface{}) {
	for _, row := range rows {
		idx := matchRow(signatures, row)
		if idx < 0 {
			continue
		}
		rec := fusionMap[idx]
		rec.K8sService = getString(row, "k8s_service")
		rec.K8sNamespace = getString(row, "k8s_namespace")
		rec.K8sWorkload = getString(row, "k8s_workload")
		rec.K8sPod = getString(row, "k8s_pod")
		rec.K8sCluster = getString(row, "k8s_cluster")
		rec.K8sNode = getString(row, "k8s_node")
		rec.VMHostname = getString(row, "vm_hostname")
		rec.ProcessName = getString(row, "process_name")
		rec.ServiceIP = getString(row, "server_ip")
		if rec.RequestDomain == "" {
			rec.RequestDomain = getString(row, "request_domain")
		}
	}
}

func attachS(fusionMap map[int]*models.FusedRecord, signatures []models.APISignature, rows []map[string]interface{}) {
	for _, row := range rows {
		idx := matchRow(signatures, row)
		if idx < 0 {
			continue
		}
		rec := fusionMap[idx]
		if rec.SourceIP == "" {
			rec.SourceIP = getString(row, "source_ip")
		}
		if rec.HostIP == "" {
			rec.HostIP = getString(row, "host_ip")
		}
		isExt := getBool(row, "is_external")
		if rec.IsExternal == nil {
			rec.IsExternal = &isExt
		}
	}
}

func attachCP(fusionMap map[int]*models.FusedRecord, signatures []models.APISignature, rows []map[string]interface{}) {
	for _, row := range rows {
		idx := matchRow(signatures, row)
		if idx < 0 {
			continue
		}
		rec := fusionMap[idx]
		if rec.SourceServiceIP == "" {
			rec.SourceServiceIP = getString(row, "source_service_ip")
		}
		if rec.SourceProcessName == "" {
			rec.SourceProcessName = getString(row, "source_process_name")
		}
		if rec.SourceK8sPod == "" {
			rec.SourceK8sPod = getString(row, "source_k8s_pod")
		}
		if rec.SourceK8sNamespace == "" {
			rec.SourceK8sNamespace = getString(row, "source_k8s_namespace")
		}
		if rec.SourceK8sService == "" {
			rec.SourceK8sService = getString(row, "source_k8s_service")
		}
		if rec.SourceK8sCluster == "" {
			rec.SourceK8sCluster = getString(row, "source_k8s_cluster")
		}
		if rec.SourceVMHostname == "" {
			rec.SourceVMHostname = getString(row, "source_vm_hostname")
		}
	}
}

// matchRow finds the signature index that matches the given observation row
// using composite fusion key (request_resource, request_type, start_time ± 500ms).
func matchRow(signatures []models.APISignature, row map[string]interface{}) int {
	resource := getString(row, "request_resource")
	method := getString(row, "request_type")
	rowTime := getTime(row, "start_time")

	for i, sig := range signatures {
		if sig.SampleURL != resource {
			continue
		}
		if sig.HTTPMethod != method {
			continue
		}
		if !sig.StartTime.IsZero() && !rowTime.IsZero() {
			diff := sig.StartTime.Sub(rowTime)
			if diff < 0 {
				diff = -diff
			}
			if diff > 500*time.Millisecond {
				continue
			}
		}
		return i
	}
	return -1
}

func buildURLFilter(urls []string) string {
	if len(urls) == 0 {
		return `""`
	}
	unique := make(map[string]bool, len(urls))
	var builder strings.Builder
	first := true
	for _, u := range urls {
		if unique[u] {
			continue
		}
		unique[u] = true
		if !first {
			builder.WriteString(", ")
		}
		builder.WriteString(`"`)
		builder.WriteString(strings.ReplaceAll(u, `"`, `\"`))
		builder.WriteString(`"`)
		first = false
	}
	return builder.String()
}

// getClickHouseConfig returns the ClickHouse timezone config.
func getClickHouseConfig(cfg *config.Config) config.ClickHouseConfig {
	return cfg.Discovery.Source.ClickHouse
}
