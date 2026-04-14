// Package discovery implements Phase 1: Traffic Discovery from DeepFlow.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/deepflow"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/models"
	"github.com/wso2/adc/internal/store"
)

const pipelineName = "api_discovery"

// Phase implements engine.Phase for traffic discovery.
type Phase struct {
	cfg        *config.Config
	client     deepflow.Client
	repos      *store.Repositories
	logger     *logging.Logger
	filter     *NoiseFilter
	normalizer *Normalizer
}

// New creates a new discovery Phase.
func New(cfg *config.Config, client deepflow.Client, repos *store.Repositories, logger *logging.Logger) *Phase {
	return &Phase{
		cfg:        cfg,
		client:     client,
		repos:      repos,
		logger:     logger,
		filter:     NewNoiseFilter(cfg.Discovery.NoiseFilter),
		normalizer: NewNormalizer(cfg.Discovery.Normalization, logger),
	}
}

// Name returns the phase name.
func (p *Phase) Name() string { return "discovery" }

// Run executes the Phase 1 discovery pipeline (Steps 0-6).
func (p *Phase) Run(ctx context.Context, cycleID string) error {
	log := p.logger.WithFields("phase", "discovery", "cycle_id", cycleID)

	// Step 0: Calculate sliding window
	start, end, err := p.calculateWindow(ctx, log)
	if err != nil {
		return fmt.Errorf("step 0 calculate window: %w", err)
	}
	if !end.After(start) {
		log.Infow("No new data in window, skipping", "start", start, "end", end)
		return nil
	}
	log.Infow("Discovery window", "start", start, "end", end)

	// Step 1: Query DeepFlow for unique API signatures
	signatures, err := p.querySignatures(ctx, start, end, log)
	if err != nil {
		return fmt.Errorf("step 1 query signatures: %w", err)
	}

	if len(signatures) == 0 {
		log.Infow("No signatures found, advancing watermark")
		return p.repos.PipelineState.Advance(ctx, pipelineName, end, 0)
	}
	log.Infow("Step 1 complete", "signatures", len(signatures))

	// Step 2: Noise filtering
	filtered := p.filter.Apply(signatures)
	log.Infow("Step 2 complete", "before", len(signatures), "after", len(filtered))

	if len(filtered) == 0 {
		log.Infow("All signatures filtered as noise, advancing watermark")
		return p.repos.PipelineState.Advance(ctx, pipelineName, end, 0)
	}

	// Step 3: Normalize dynamic path parameters
	for i := range filtered {
		original := filtered[i].Endpoint
		filtered[i].ResourcePath = p.normalizer.Normalize(original)
		filtered[i].SamplePath = original
	}
	log.Infow("Step 3 complete", "normalized", len(filtered))

	// Step 4: Dedup normalized signatures
	deduped := Dedup(filtered)
	log.Infow("Step 4 complete", "before", len(filtered), "after", len(deduped))

	// Step 5: Enrich via observation point fusion + compute service_key
	records, err := p.enrich(ctx, deduped, log)
	if err != nil {
		log.Errorw("Step 5 enrichment failed, advancing watermark", "error", err)
		// Advance watermark — signatures will reappear in next cycle
		_ = p.repos.PipelineState.Advance(ctx, pipelineName, end, 0)
		return fmt.Errorf("step 5 enrich: %w", err)
	}
	log.Infow("Step 5 complete", "enriched", len(records))

	// Step 5g: Batch upsert to PostgreSQL
	upserted, failed, err := p.repos.Discovered.BatchUpsert(ctx, records)
	if err != nil {
		return fmt.Errorf("step 5g upsert: %w", err)
	}
	if failed > 0 {
		log.Warnw("Step 5g upsert had failures", "upserted", upserted, "failed", failed)
	} else {
		log.Infow("Step 5g upsert complete", "upserted", upserted)
	}

	// Step 6: Advance watermark
	var totalHits int64
	for _, sig := range deduped {
		totalHits += sig.HitCount
	}
	if err := p.repos.PipelineState.Advance(ctx, pipelineName, end, totalHits); err != nil {
		return fmt.Errorf("step 6 advance watermark: %w", err)
	}

	log.Infow("Discovery cycle complete",
		"discovered", len(records),
		"total_hits", totalHits,
	)
	return nil
}

func (p *Phase) calculateWindow(ctx context.Context, log *logging.Logger) (time.Time, time.Time, error) {
	pollInterval, _ := time.ParseDuration(p.cfg.Discovery.Schedule.PollInterval)
	safetyLag, _ := time.ParseDuration(p.cfg.Discovery.Schedule.SafetyLag)

	state, err := p.repos.PipelineState.Get(ctx, pipelineName)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("get pipeline state: %w", err)
	}

	now := time.Now()
	watermarkEnd := now.Add(-safetyLag)

	var watermarkStart time.Time
	if state == nil {
		// First run
		watermarkStart = now.Add(-(pollInterval + safetyLag))
		log.Infow("First run, using initial window", "start", watermarkStart)
	} else {
		watermarkStart = state.LastWatermarkEnd
	}

	return watermarkStart, watermarkEnd, nil
}

func (p *Phase) querySignatures(ctx context.Context, start, end time.Time, log *logging.Logger) ([]models.APISignature, error) {
	filter := p.cfg.Discovery.TrafficFilter
	sql := fmt.Sprintf(`SELECT
    request_type,
    endpoint,
    server_port,
    agent_id,
    Count(row) AS hit_count,
    any(request_resource) AS sample_url,
    toUnixTimestamp(any(start_time)) AS sample_start_time,
    any(request_domain) AS sample_request_domain,
    any(is_tls) AS sample_is_tls,
    any(l7_protocol_str) AS sample_protocol,
    any(version) AS sample_http_version,
    any(response_code) AS sample_response_code,
    any(response_duration) AS sample_latency_us
FROM l7_flow_log
WHERE observation_point = "%s"
  AND protocol = %d
  AND l7_protocol_str IN (%s)
  AND direction_score >= %d
  AND toUnixTimestamp(start_time) >= %d
  AND toUnixTimestamp(start_time) < %d
  AND endpoint != ""
  AND request_type != ""
GROUP BY request_type, endpoint, server_port, agent_id
ORDER BY hit_count DESC
LIMIT %d`,
		filter.ObservationPoint,
		filter.Protocol,
		joinProtocolsDoubleQuote(filter.L7Protocols),
		filter.MinDirectionScore,
		start.Unix(),
		end.Unix(),
		p.cfg.Discovery.Schedule.MaxSignaturesPerCycle,
	)

	rows, err := p.client.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query l7_flow_log: %w", err)
	}

	signatures := make([]models.APISignature, 0, len(rows))
	for _, row := range rows {
		sig := parseSignature(row)
		if sig.HTTPMethod == "" || sig.Endpoint == "" || sig.HitCount == 0 {
			continue
		}
		signatures = append(signatures, sig)
	}

	return signatures, nil
}

func joinProtocols(protocols []string) string {
	result := ""
	for i, p := range protocols {
		if i > 0 {
			result += "', '"
		}
		result += p
	}
	return result
}

func joinProtocolsDoubleQuote(protocols []string) string {
	result := ""
	for i, p := range protocols {
		if i > 0 {
			result += ", "
		}
		escaped := strings.ReplaceAll(p, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		result += `"` + escaped + `"`
	}
	return result
}

func parseSignature(row map[string]interface{}) models.APISignature {
	sig := models.APISignature{}
	sig.HTTPMethod = getString(row, "request_type")
	sig.Endpoint = getString(row, "endpoint")
	sig.ServerPort = getInt(row, "server_port")
	sig.AgentID = getInt(row, "agent_id")
	sig.HitCount = getInt64(row, "hit_count")
	sig.SampleURL = getString(row, "sample_url")
	sig.RequestDomain = getString(row, "sample_request_domain")
	sig.IsTLS = getBool(row, "sample_is_tls")
	sig.Protocol = getString(row, "sample_protocol")
	if sig.Protocol == "" {
		sig.Protocol = "HTTP"
	}
	sig.HTTPVersion = getString(row, "sample_http_version")
	sig.ResponseCode = getInt(row, "sample_response_code")
	sig.LatencyUs = getInt64(row, "sample_latency_us")
	sig.StartTime = getTime(row, "sample_start_time")

	if sig.SampleURL == "" {
		sig.SampleURL = sig.Endpoint
	}

	return sig
}

func getString(row map[string]interface{}, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getInt(row map[string]interface{}, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func getInt64(row map[string]interface{}, key string) int64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func getBool(row map[string]interface{}, key string) bool {
	v, ok := row[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	default:
		return false
	}
}

func getTime(row map[string]interface{}, key string) time.Time {
	v, ok := row[key]
	if !ok || v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case string:
		parsed, err := time.Parse("2006-01-02 15:04:05", t)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, t)
			if err != nil {
				return time.Time{}
			}
		}
		return parsed
	case float64:
		// Unix timestamp in seconds
		return time.Unix(int64(t), 0)
	default:
		return time.Time{}
	}
}
