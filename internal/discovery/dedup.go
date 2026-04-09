package discovery

import "github.com/wso2/adc/internal/models"

type dedupKey struct {
	HTTPMethod   string
	ResourcePath string
	ServerPort   int
	AgentID      int
}

// Dedup merges normalized signatures that collapsed to the same (method, path, port, agent).
func Dedup(signatures []models.APISignature) []models.APISignature {
	groups := make(map[dedupKey]*models.APISignature)
	order := make([]dedupKey, 0, len(signatures))

	for i := range signatures {
		sig := &signatures[i]
		key := dedupKey{
			HTTPMethod:   sig.HTTPMethod,
			ResourcePath: sig.ResourcePath,
			ServerPort:   sig.ServerPort,
			AgentID:      sig.AgentID,
		}

		if existing, ok := groups[key]; ok {
			existing.HitCount += sig.HitCount
			// Keep the representative with highest original count
			if sig.HitCount > existing.HitCount-sig.HitCount {
				existing.SampleURL = sig.SampleURL
				existing.SamplePath = sig.SamplePath
				existing.StartTime = sig.StartTime
				existing.RequestDomain = sig.RequestDomain
			}
		} else {
			clone := *sig
			groups[key] = &clone
			order = append(order, key)
		}
	}

	result := make([]models.APISignature, 0, len(groups))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	return result
}
