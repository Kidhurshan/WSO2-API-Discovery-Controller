// Package specgen implements Phase 4: OpenAPI Specification Generation.
package specgen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

var adcVersion = "dev"

// Phase implements engine.Phase for OpenAPI spec generation.
type Phase struct {
	cfg    *config.Config
	repos  *store.Repositories
	logger *logging.Logger
}

// New creates a new specgen Phase.
func New(cfg *config.Config, repos *store.Repositories, logger *logging.Logger) *Phase {
	return &Phase{
		cfg:    cfg,
		repos:  repos,
		logger: logger,
	}
}

// Name returns the phase name.
func (p *Phase) Name() string { return "specgen" }

// Run executes the Phase 4 OpenAPI spec generation pipeline.
func (p *Phase) Run(ctx context.Context, cycleID string) error {
	log := p.logger.WithFields("phase", "specgen", "cycle_id", cycleID)
	content := p.cfg.SpecGeneration.Content

	// Step 0: Identify service groups needing specs
	groups, err := p.repos.Unmanaged.GetDetectedGroups(ctx)
	if err != nil {
		return fmt.Errorf("step 0 get detected groups: %w", err)
	}
	if len(groups) == 0 {
		log.Infow("No service groups need spec generation")
		return nil
	}
	log.Infow("Step 0: Groups identified", "count", len(groups))

	var generated, skipped int
	for _, group := range groups {
		// Step 1: Fetch all operations with metadata
		ops, err := p.repos.Unmanaged.GetGroupOperations(ctx, group)
		if err != nil {
			log.Warnw("Failed to fetch group operations", "service_key", group.ServiceKey, "error", err)
			skipped++
			continue
		}
		if len(ops) == 0 {
			skipped++
			continue
		}

		// Steps 2-5: Build OAS document
		oas := assembleOAS(group, ops, content)

		// Validate
		if err := validateOAS(oas); err != nil {
			log.Errorw("OAS validation failed", "service_key", group.ServiceKey, "error", err)
			skipped++
			continue
		}

		// Marshal to JSON
		specJSON, err := json.Marshal(oas)
		if err != nil {
			log.Errorw("JSON marshal failed", "service_key", group.ServiceKey, "error", err)
			skipped++
			continue
		}

		// Step 6: Persist spec and update status
		updated, err := p.repos.Unmanaged.UpdateGroupSpec(ctx, group, specJSON)
		if err != nil {
			log.Errorw("Failed to persist spec", "service_key", group.ServiceKey, "error", err)
			skipped++
			continue
		}

		generated++
		log.Infow("Spec generated",
			"service_key", group.ServiceKey,
			"classification", group.Classification,
			"operations", len(ops),
			"rows_updated", updated,
			"spec_bytes", len(specJSON),
		)
	}

	log.Infow("Spec generation complete",
		"groups", len(groups),
		"generated", generated,
		"skipped", skipped,
	)
	return nil
}

func assembleOAS(group store.ServiceGroup, ops []store.OperationWithMetadata, content config.SpecContentConfig) map[string]interface{} {
	info := buildInfo(group, ops, content)
	servers := buildServers(ops, content)
	paths := buildPaths(ops, content)

	return map[string]interface{}{
		"openapi": "3.0.3",
		"info":    info,
		"servers": servers,
		"paths":   paths,
		"x-adc-discovery": map[string]interface{}{
			"generated_at": time.Now().Format(time.RFC3339),
			"adc_version":  adcVersion,
			"phase":        "4",
		},
	}
}

// ── Info Section ──

func buildInfo(group store.ServiceGroup, ops []store.OperationWithMetadata, content config.SpecContentConfig) map[string]interface{} {
	info := map[string]interface{}{
		"title":          generateTitle(group, ops),
		"description":    generateDescription(group, ops),
		"version":        time.Now().Format("2006.01.02"),
		"x-adc-metadata": buildMetadata(group, ops, content),
	}
	return info
}

func generateTitle(group store.ServiceGroup, ops []store.OperationWithMetadata) string {
	if group.Classification == "DRIFT" && len(ops) > 0 && ops[0].ParentAPIName != "" {
		return ops[0].ParentAPIName + "-Drift"
	}
	name := strings.ReplaceAll(group.ServiceKey, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

func generateDescription(group store.ServiceGroup, ops []store.OperationWithMetadata) string {
	if group.Classification == "SHADOW" {
		return "Automatically discovered by WSO2 API Discovery Controller (ADC). " +
			"This service was detected from live traffic captured by DeepFlow eBPF agents. " +
			"Classification: SHADOW — this service has no operations registered in WSO2 APIM."
	}
	parentName, parentVersion := "", ""
	if len(ops) > 0 {
		parentName = ops[0].ParentAPIName
		parentVersion = ops[0].ParentAPIVersion
	}
	return fmt.Sprintf(
		"Automatically discovered by WSO2 API Discovery Controller (ADC). "+
			"These operations were discovered on the %s service which is managed via "+
			"WSO2 APIM as '%s %s'. However, these specific operations are NOT registered "+
			"in the APIM definition. Classification: DRIFT — operations that exist in the "+
			"live service but are missing from the API governance layer.",
		group.ServiceKey, parentName, parentVersion,
	)
}

func buildMetadata(group store.ServiceGroup, ops []store.OperationWithMetadata, content config.SpecContentConfig) map[string]interface{} {
	op := ops[0]
	serviceType := op.ServiceType
	if serviceType == "" {
		serviceType = "UNKNOWN"
	}

	meta := map[string]interface{}{
		"service_key":      op.ServiceKey,
		"service_type":     serviceType,
		"classification":   group.Classification,
		"first_seen_at":    earliest(ops).Format(time.RFC3339),
		"last_seen_at":     latest(ops).Format(time.RFC3339),
		"total_hit_count":  sumHits(ops),
		"total_operations": len(ops),
		"discovery_source": "deepflow-ebpf",
	}

	addIfNotEmpty(meta, "process_name", op.ProcessName)

	if group.Classification == "DRIFT" {
		addIfNotEmpty(meta, "parent_api_name", op.ParentAPIName)
		addIfNotEmpty(meta, "parent_api_version", op.ParentAPIVersion)
	}

	if content.IncludeK8sMetadata && serviceType == "KUBERNETES" {
		addIfNotEmpty(meta, "k8s_namespace", op.K8sNamespace)
		addIfNotEmpty(meta, "k8s_service", op.K8sService)
		addIfNotEmpty(meta, "k8s_workload", op.K8sWorkload)
		addIfNotEmpty(meta, "k8s_cluster", op.K8sCluster)
		addIfNotEmpty(meta, "k8s_pod", op.K8sPod)
		addIfNotEmpty(meta, "k8s_node", op.K8sNode)
	}

	if content.IncludeLegacyMetadata && serviceType == "LEGACY" {
		addIfNotEmpty(meta, "vm_hostname", op.VMHostname)
	}

	if content.IncludeNetworkMetadata {
		addIfNotEmpty(meta, "service_ip", op.ServiceIP)
		addIfNotEmpty(meta, "host_ip", op.HostIP)
		addIfNotEmpty(meta, "traffic_origin", op.TrafficOrigin)
		addIfNotEmpty(meta, "agent_name", op.AgentName)
	}

	return meta
}

// ── Servers Section ──

func buildServers(ops []store.OperationWithMetadata, content config.SpecContentConfig) []map[string]interface{} {
	if content.ServerURLOverride != "" {
		return []map[string]interface{}{
			{"url": content.ServerURLOverride, "description": "Server URL configured by ADC administrator"},
		}
	}

	hasTLS, hasPlain := false, false
	for _, op := range ops {
		if op.IsTLS {
			hasTLS = true
		} else {
			hasPlain = true
		}
	}

	hostname := resolveHostname(ops)
	var servers []map[string]interface{}

	if hasTLS {
		servers = append(servers, map[string]interface{}{
			"url":         fmt.Sprintf("https://%s", hostname),
			"description": "Discovered HTTPS endpoint (TLS detected at application layer)",
		})
	}
	if hasPlain {
		servers = append(servers, map[string]interface{}{
			"url":         fmt.Sprintf("http://%s", hostname),
			"description": "Discovered HTTP endpoint (no TLS at application layer)",
		})
	}
	if len(servers) == 0 {
		servers = append(servers, map[string]interface{}{
			"url":         fmt.Sprintf("http://%s", hostname),
			"description": "Discovered endpoint (TLS status unknown)",
		})
	}

	return servers
}

func resolveHostname(ops []store.OperationWithMetadata) string {
	op := ops[0]

	if op.K8sService != "" && op.K8sNamespace != "" {
		return op.K8sService + "." + op.K8sNamespace
	}
	if op.K8sService != "" {
		return op.K8sService
	}

	if op.RequestDomain != "" {
		domain := stripPort(op.RequestDomain)
		if isDNSName(domain) && domain != "localhost" && domain != "0.0.0.0" {
			return domain
		}
		if domain != "" && domain != "127.0.0.1" && domain != "0.0.0.0" {
			return domain
		}
	}

	if op.HostIP != "" && op.HostIP != "127.0.0.1" && op.HostIP != "0.0.0.0" {
		return op.HostIP
	}

	if op.ServiceIP != "" && op.ServiceIP != "127.0.0.1" && op.ServiceIP != "0.0.0.0" {
		return op.ServiceIP
	}

	return "unknown-host"
}

func isDNSName(s string) bool {
	hasLetter, hasDot := false, false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		}
		if c == '.' {
			hasDot = true
		}
	}
	return hasLetter && hasDot
}

func stripPort(hostPort string) string {
	if idx := strings.LastIndex(hostPort, ":"); idx != -1 {
		return hostPort[:idx]
	}
	return hostPort
}

// ── Paths Section ──

type pathParam struct {
	Name     string
	Position int
}

func buildPaths(ops []store.OperationWithMetadata, content config.SpecContentConfig) map[string]interface{} {
	type pathGroup struct {
		oasPath    string
		params     []pathParam
		operations []store.OperationWithMetadata
	}

	groups := map[string]*pathGroup{}
	for _, op := range ops {
		if _, exists := groups[op.ResourcePath]; !exists {
			oasPath, params := convertToOASPath(op.ResourcePath)
			groups[op.ResourcePath] = &pathGroup{oasPath: oasPath, params: params}
		}
		groups[op.ResourcePath].operations = append(groups[op.ResourcePath].operations, op)
	}

	paths := map[string]interface{}{}
	for _, pg := range groups {
		pathItem := map[string]interface{}{}

		if len(pg.params) > 0 {
			var params []map[string]interface{}
			for _, p := range pg.params {
				params = append(params, map[string]interface{}{
					"name":        p.Name,
					"in":          "path",
					"required":    true,
					"schema":      map[string]string{"type": "string"},
					"description": "Dynamic path parameter (auto-detected from traffic patterns)",
				})
			}
			pathItem["parameters"] = params
		}

		for _, op := range pg.operations {
			method := strings.ToLower(op.HTTPMethod)
			pathItem[method] = buildOperation(op, content)
		}

		paths[pg.oasPath] = pathItem
	}

	return paths
}

func convertToOASPath(discoveredPath string) (string, []pathParam) {
	segments := strings.Split(strings.Trim(discoveredPath, "/"), "/")
	var params []pathParam
	idCount := 0

	for i, seg := range segments {
		if seg == "{id}" {
			idCount++
			paramName := "id"
			if idCount > 1 {
				paramName = fmt.Sprintf("id%d", idCount)
			}
			segments[i] = "{" + paramName + "}"
			params = append(params, pathParam{Name: paramName, Position: i})
		}
	}

	return "/" + strings.Join(segments, "/"), params
}

func buildOperation(op store.OperationWithMetadata, content config.SpecContentConfig) map[string]interface{} {
	method := strings.ToUpper(op.HTTPMethod)
	operation := map[string]interface{}{
		"summary":     fmt.Sprintf("%s %s", method, op.ResourcePath),
		"operationId": generateOperationID(op.HTTPMethod, op.ResourcePath),
	}

	if content.IncludeSampleURLs && op.SampleURL != "" {
		operation["description"] = fmt.Sprintf("Discovered from live traffic. Sample: %s %s", method, op.SampleURL)
	} else {
		operation["description"] = "Discovered from live traffic."
	}

	if op.ResponseCode > 0 {
		statusStr := fmt.Sprintf("%d", op.ResponseCode)
		operation["responses"] = map[string]interface{}{
			statusStr: map[string]interface{}{
				"description": fmt.Sprintf("Observed response (sample status code: %d)", op.ResponseCode),
			},
		}
	} else {
		operation["responses"] = map[string]interface{}{
			"default": map[string]interface{}{"description": "Observed response"},
		}
	}

	if content.IncludeTrafficStats {
		traffic := map[string]interface{}{
			"hit_count":    op.HitCount,
			"observed_tls": op.IsTLS,
		}
		if !op.FirstSeenAt.IsZero() {
			traffic["first_seen_at"] = op.FirstSeenAt.Format(time.RFC3339)
		}
		if !op.LastSeenAt.IsZero() {
			traffic["last_seen_at"] = op.LastSeenAt.Format(time.RFC3339)
		}
		if op.ResponseCode > 0 {
			traffic["sample_response_code"] = op.ResponseCode
		}
		if op.LatencyUS > 0 {
			traffic["sample_latency_us"] = op.LatencyUS
		}
		operation["x-adc-traffic"] = traffic
	}

	return operation
}

func generateOperationID(method, path string) string {
	id := strings.ToLower(method) + "-" + path
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, "{", "")
	id = strings.ReplaceAll(id, "}", "")
	id = strings.Trim(id, "-")
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	return id
}

// ── Validation ──

func validateOAS(oas map[string]interface{}) error {
	if oas["openapi"] == nil {
		return fmt.Errorf("missing openapi version")
	}
	info, ok := oas["info"].(map[string]interface{})
	if !ok || info["title"] == nil || info["version"] == nil {
		return fmt.Errorf("missing info.title or info.version")
	}
	paths, ok := oas["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		return fmt.Errorf("paths is empty")
	}

	if _, err := json.Marshal(oas); err != nil {
		return fmt.Errorf("JSON serialization failed: %w", err)
	}

	// Check unique operationIds
	opIDs := map[string]bool{}
	for _, pathItem := range paths {
		pi, ok := pathItem.(map[string]interface{})
		if !ok {
			continue
		}
		for key, opData := range pi {
			if key == "parameters" {
				continue
			}
			op, ok := opData.(map[string]interface{})
			if !ok {
				continue
			}
			if opID, ok := op["operationId"].(string); ok {
				if opIDs[opID] {
					return fmt.Errorf("duplicate operationId: %s", opID)
				}
				opIDs[opID] = true
			}
		}
	}

	return nil
}

// ── Helpers ──

func addIfNotEmpty(m map[string]interface{}, key, value string) {
	if value != "" {
		m[key] = value
	}
}

func earliest(ops []store.OperationWithMetadata) time.Time {
	t := ops[0].FirstSeenAt
	for _, op := range ops[1:] {
		if op.FirstSeenAt.Before(t) {
			t = op.FirstSeenAt
		}
	}
	return t
}

func latest(ops []store.OperationWithMetadata) time.Time {
	t := ops[0].LastSeenAt
	for _, op := range ops[1:] {
		if op.LastSeenAt.After(t) {
			t = op.LastSeenAt
		}
	}
	return t
}

func sumHits(ops []store.OperationWithMetadata) int64 {
	var total int64
	for _, op := range ops {
		total += op.HitCount
	}
	return total
}
