package discovery

import (
	"strings"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/models"
)

// NoiseFilter removes non-API traffic signatures.
type NoiseFilter struct {
	excludedAgentIDs map[int]bool
	excludedPorts    map[int]bool
	excludedDomains  map[string]bool
	pathExact        map[string]bool
	pathPatterns     []string
}

// NewNoiseFilter creates a NoiseFilter from config.
func NewNoiseFilter(cfg config.NoiseFilterConfig) *NoiseFilter {
	agentIDs := make(map[int]bool, len(cfg.ExcludedAgentIDs))
	for _, id := range cfg.ExcludedAgentIDs {
		agentIDs[id] = true
	}

	ports := make(map[int]bool, len(cfg.ExcludedPorts))
	for _, p := range cfg.ExcludedPorts {
		ports[p] = true
	}

	domains := make(map[string]bool, len(cfg.ExcludedDomains))
	for _, d := range cfg.ExcludedDomains {
		domains[strings.ToLower(d)] = true
	}

	exact := make(map[string]bool, len(cfg.PathExact))
	for _, p := range cfg.PathExact {
		exact[p] = true
	}

	return &NoiseFilter{
		excludedAgentIDs: agentIDs,
		excludedPorts:    ports,
		excludedDomains:  domains,
		pathExact:        exact,
		pathPatterns:     cfg.PathPatterns,
	}
}

// Apply filters signatures in order: agent → port → domain → exact path → pattern path.
func (f *NoiseFilter) Apply(signatures []models.APISignature) []models.APISignature {
	result := make([]models.APISignature, 0, len(signatures))
	for _, sig := range signatures {
		if f.shouldFilter(sig) {
			continue
		}
		result = append(result, sig)
	}
	return result
}

func (f *NoiseFilter) shouldFilter(sig models.APISignature) bool {
	// 1. Agent ID filter
	if f.excludedAgentIDs[sig.AgentID] {
		return true
	}

	// 2. Port filter
	if f.excludedPorts[sig.ServerPort] {
		return true
	}

	// 3. Domain filter
	domain := strings.ToLower(sig.RequestDomain)
	// Strip port from domain
	if idx := strings.LastIndex(domain, ":"); idx > 0 {
		domain = domain[:idx]
	}
	if f.excludedDomains[domain] {
		return true
	}

	// 4. Path exact match
	if f.pathExact[sig.Endpoint] {
		return true
	}

	// 5. Path pattern match (case-insensitive substring)
	lowerEndpoint := strings.ToLower(sig.Endpoint)
	for _, pattern := range f.pathPatterns {
		if strings.Contains(lowerEndpoint, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}
