package comparison

import "github.com/wso2/adc/internal/models"

// ClassificationResult holds the final classification for one unmanaged discovered API.
type ClassificationResult struct {
	DiscoveredID   int
	ServiceKey     string
	HTTPMethod     string
	ResourcePath   string
	Classification models.Classification
	Confidence     models.Confidence
	ManagedAPIID   *int // non-nil for DRIFT
}

// Classify determines SHADOW vs DRIFT for unmanaged APIs.
// An API is DRIFT if other operations from the same service_key are MANAGED.
// An API is SHADOW if no operations from the service_key are managed.
func Classify(
	candidates []CandidateResult,
	reclassified map[int]bool,
	ambiguousIDs map[int]bool,
) []ClassificationResult {
	// Build set of service_keys that have at least one managed operation
	// (excluding reclassified losers)
	managedServiceKeys := make(map[string]int) // service_key → managed_api_pk (first found)
	for _, c := range candidates {
		if c.ManagedOpID != nil && !reclassified[c.DiscoveredID] {
			if _, ok := managedServiceKeys[c.ServiceKey]; !ok {
				managedServiceKeys[c.ServiceKey] = *c.ManagedAPIPK
			}
		}
	}

	// Classify each unmanaged API
	var results []ClassificationResult
	for _, c := range candidates {
		// Skip managed (matched and not reclassified)
		if c.ManagedOpID != nil && !reclassified[c.DiscoveredID] {
			continue
		}

		confidence := models.ConfidenceHigh
		if ambiguousIDs[c.DiscoveredID] {
			confidence = models.ConfidenceAmbiguous
		} else if reclassified[c.DiscoveredID] {
			confidence = models.ConfidenceLow
		}

		cr := ClassificationResult{
			DiscoveredID: c.DiscoveredID,
			ServiceKey:   c.ServiceKey,
			HTTPMethod:   c.HTTPMethod,
			ResourcePath: c.ResourcePath,
			Confidence:   confidence,
		}

		if managedAPIPK, hasManagedSibling := managedServiceKeys[c.ServiceKey]; hasManagedSibling {
			cr.Classification = models.ClassificationDrift
			cr.ManagedAPIID = &managedAPIPK
		} else {
			cr.Classification = models.ClassificationShadow
		}

		results = append(results, cr)
	}

	return results
}
