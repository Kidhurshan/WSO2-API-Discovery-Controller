package comparison

import (
	"sort"
	"strings"

	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/models"
)

// Collision represents a case where multiple service_keys match the same managed operation.
type Collision struct {
	HTTPMethod  string
	MatchPath   string
	APIMApiID   string
	ManagedAPIPK int
	ServiceKeys []string
}

// DiscoveredKey is a lookup key for the discovered set.
type DiscoveredKey struct {
	ServiceKey   string
	HTTPMethod   string
	ResourcePath string
}

// DetectCollisions finds cases where multiple service_keys match the same managed operation.
func DetectCollisions(candidates []CandidateResult) []Collision {
	// Group managed matches by (http_method, resource_path, apim_api_id)
	type collisionKey struct {
		HTTPMethod string
		Path       string
		APIMApiID  string
	}

	groups := make(map[collisionKey]map[string]bool) // key → set of service_keys
	pkMap := make(map[collisionKey]int)               // key → managed_api_pk

	for _, c := range candidates {
		if c.ManagedOpID == nil {
			continue
		}
		key := collisionKey{
			HTTPMethod: c.HTTPMethod,
			Path:       c.ResourcePath,
			APIMApiID:  *c.APIMApiID,
		}
		if groups[key] == nil {
			groups[key] = make(map[string]bool)
		}
		groups[key][c.ServiceKey] = true
		pkMap[key] = *c.ManagedAPIPK
	}

	var collisions []Collision
	for key, sks := range groups {
		if len(sks) <= 1 {
			continue
		}
		var serviceKeys []string
		for sk := range sks {
			serviceKeys = append(serviceKeys, sk)
		}
		collisions = append(collisions, Collision{
			HTTPMethod:   key.HTTPMethod,
			MatchPath:    key.Path,
			APIMApiID:    key.APIMApiID,
			ManagedAPIPK: pkMap[key],
			ServiceKeys:  serviceKeys,
		})
	}

	return collisions
}

// ResolveCollisions resolves collisions using affinity scoring and domain tiebreaker.
// Returns the set of (discovered_id) that should be reclassified as UNMANAGED.
func ResolveCollisions(
	collisions []Collision,
	candidates []CandidateResult,
	managedOps []models.ManagedAPIOperation,
	endpointHostnameFn func(managedAPIPK int) string,
	logger *logging.Logger,
) (reclassify map[int]bool, ambiguous map[int]bool) {
	reclassify = make(map[int]bool)
	ambiguous = make(map[int]bool)

	if len(collisions) == 0 {
		return
	}

	// Build discovered set for affinity lookup
	discoveredSet := make(map[DiscoveredKey]bool, len(candidates))
	for _, c := range candidates {
		discoveredSet[DiscoveredKey{c.ServiceKey, c.HTTPMethod, c.ResourcePath}] = true
	}

	// Build index: apim_api_id → managed ops
	opsForAPI := make(map[string][]models.ManagedAPIOperation)
	for _, op := range managedOps {
		opsForAPI[op.APIMApiID] = append(opsForAPI[op.APIMApiID], op)
	}

	// Build index: service_key → request_domain (first seen)
	domainForSK := make(map[string]string)
	for _, c := range candidates {
		if _, ok := domainForSK[c.ServiceKey]; !ok && c.RequestDomain != "" {
			domainForSK[c.ServiceKey] = c.RequestDomain
		}
	}

	for _, col := range collisions {
		logger.Warnw("Collision detected",
			"method", col.HTTPMethod,
			"path", col.MatchPath,
			"service_keys", col.ServiceKeys,
		)

		apiOps := opsForAPI[col.APIMApiID]
		scores := make(map[string]int)

		for _, sk := range col.ServiceKeys {
			for _, mOp := range apiOps {
				if discoveredSet[DiscoveredKey{sk, mOp.HTTPMethod, mOp.MatchPath}] {
					scores[sk]++
				}
			}
		}

		// Find highest score
		maxScore := 0
		for _, s := range scores {
			if s > maxScore {
				maxScore = s
			}
		}

		// Check for tie — sort for deterministic resolution across cycles
		var tiedKeys []string
		for sk, s := range scores {
			if s == maxScore {
				tiedKeys = append(tiedKeys, sk)
			}
		}
		sort.Strings(tiedKeys)

		winner := ""
		if len(tiedKeys) == 1 {
			winner = tiedKeys[0]
		} else {
			// Domain tiebreaker
			endpointHost := endpointHostnameFn(col.ManagedAPIPK)
			for _, sk := range tiedKeys {
				domain := domainForSK[sk]
				if stripPort(domain) == endpointHost {
					winner = sk
					break
				}
			}

			if winner == "" {
				logger.Warnw("AMBIGUOUS collision — manual review needed",
					"method", col.HTTPMethod,
					"path", col.MatchPath,
					"tied_keys", tiedKeys,
				)
				// Mark all colliding discovered APIs for these service_keys as AMBIGUOUS
				for _, c := range candidates {
					if c.HTTPMethod == col.HTTPMethod && c.ResourcePath == col.MatchPath {
						ambiguous[c.DiscoveredID] = true
					}
				}
				continue
			}
		}

		logger.Infow("Collision resolved",
			"method", col.HTTPMethod,
			"path", col.MatchPath,
			"winner", winner,
			"scores", scores,
		)

		// Reclassify losers — mark their discovered_ids for this collision point
		for _, c := range candidates {
			if c.HTTPMethod == col.HTTPMethod && c.ResourcePath == col.MatchPath && c.ServiceKey != winner {
				reclassify[c.DiscoveredID] = true
			}
		}
	}

	return reclassify, ambiguous
}

func stripPort(hostPort string) string {
	if idx := strings.LastIndex(hostPort, ":"); idx != -1 {
		return hostPort[:idx]
	}
	return hostPort
}
