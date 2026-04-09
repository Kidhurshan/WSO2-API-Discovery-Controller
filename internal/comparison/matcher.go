// Package comparison implements Phase 3: Unmanaged API Detection.
package comparison

import "github.com/wso2/adc/internal/models"

// CandidateResult holds the Phase 3 LEFT JOIN result for one discovered API.
type CandidateResult struct {
	DiscoveredID   int
	ServiceKey     string
	HTTPMethod     string
	ResourcePath   string
	RequestDomain  string
	HostIP         string
	ServerPort     int
	HitCount       int64
	ManagedOpID    *int    // non-nil if matched a managed operation
	APIMApiID      *string // non-nil if matched
	ManagedAPIPK   *int    // adc_managed_apis.id if matched
}

// MatchKey is the comparison key: (http_method, path).
type MatchKey struct {
	HTTPMethod string
	Path       string
}

// Match performs the LEFT JOIN comparison between discovered APIs and managed operations.
// Returns all discovered APIs annotated with their managed match (if any).
func Match(discovered []models.DiscoveredAPI, managedOps []models.ManagedAPIOperation) []CandidateResult {
	// Build managed ops index: (http_method, match_path) → managed op
	managedIndex := make(map[MatchKey]models.ManagedAPIOperation, len(managedOps))
	for _, op := range managedOps {
		key := MatchKey{HTTPMethod: op.HTTPMethod, Path: op.MatchPath}
		managedIndex[key] = op
	}

	results := make([]CandidateResult, 0, len(discovered))
	for _, d := range discovered {
		cr := CandidateResult{
			DiscoveredID:  d.ID,
			ServiceKey:    d.ServiceKey,
			HTTPMethod:    d.HTTPMethod,
			ResourcePath:  d.ResourcePath,
			RequestDomain: d.RequestDomain,
			HostIP:        d.HostIP,
			ServerPort:    d.ServerPort,
			HitCount:      d.HitCount,
		}

		key := MatchKey{HTTPMethod: d.HTTPMethod, Path: d.ResourcePath}
		if op, ok := managedIndex[key]; ok {
			cr.ManagedOpID = &op.ID
			cr.APIMApiID = &op.APIMApiID
			cr.ManagedAPIPK = &op.ManagedAPIPK
		}

		results = append(results, cr)
	}

	return results
}
