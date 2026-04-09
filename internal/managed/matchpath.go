// Package managed implements Phase 2: Managed API Sync from WSO2 APIM.
package managed

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/wso2/adc/internal/apim"
	"github.com/wso2/adc/internal/models"
)

// NormalizeTarget replaces all {paramName} with {id}.
func NormalizeTarget(target string) string {
	if target == "" {
		return target
	}

	segments := strings.Split(strings.TrimPrefix(target, "/"), "/")
	changed := false

	for i, seg := range segments {
		if len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			segments[i] = "{id}"
			changed = true
		}
	}

	if !changed {
		return target
	}
	return "/" + strings.Join(segments, "/")
}

// ComputeMatchPath computes the path that DeepFlow captures at the backend.
func ComputeMatchPath(basepath, context, normalizedTarget string, passThrough bool) string {
	var parts []string

	if basepath != "" {
		parts = append(parts, strings.TrimSuffix(basepath, "/"))
	}

	if passThrough {
		ctx := normalizeContext(context)
		if ctx != "" {
			parts = append(parts, strings.TrimSuffix(ctx, "/"))
		}
	}

	parts = append(parts, normalizedTarget)

	result := strings.Join(parts, "")

	// Ensure leading slash
	if !strings.HasPrefix(result, "/") {
		result = "/" + result
	}
	// Remove trailing slash (unless root)
	if len(result) > 1 {
		result = strings.TrimSuffix(result, "/")
	}
	// Clean double slashes
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}

	return result
}

func normalizeContext(context string) string {
	if context == "" || context == "/" {
		return ""
	}
	if !strings.HasPrefix(context, "/") {
		context = "/" + context
	}
	return strings.TrimSuffix(context, "/")
}

// ParseEndpointConfig extracts the production endpoint URL from APIM's endpointConfig.
func ParseEndpointConfig(raw json.RawMessage) (epType, epURL string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}

	var cfg apim.EndpointConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", ""
	}

	epType = cfg.EndpointType
	if epType == "" {
		epType = "http"
	}

	// Try as single object: {"url": "..."}
	var single apim.EndpointURL
	if err := json.Unmarshal(cfg.ProductionEndpoints, &single); err == nil && single.URL != "" {
		return epType, single.URL
	}

	// Try as array: [{"url": "..."}, ...]
	var arr []apim.EndpointURL
	if err := json.Unmarshal(cfg.ProductionEndpoints, &arr); err == nil && len(arr) > 0 {
		return epType, arr[0].URL
	}

	return epType, ""
}

// ParseEndpointURL extracts hostname, port, and basepath from a URL string.
func ParseEndpointURL(rawURL string) (hostname string, port int, basepath string) {
	if rawURL == "" {
		return "", 0, ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, ""
	}

	hostname = parsed.Hostname()
	portStr := parsed.Port()
	if portStr != "" {
		fmt.Sscanf(portStr, "%d", &port)
	} else if parsed.Scheme == "https" {
		port = 443
	} else {
		port = 80
	}

	basepath = strings.TrimSuffix(parsed.Path, "/")
	return hostname, port, basepath
}

// ProcessAPIDetail converts an APIM detail response into a ManagedAPI model.
func ProcessAPIDetail(detail *apim.APIDetailResponse, passThrough bool) *models.ManagedAPI {
	epType, epURL := ParseEndpointConfig(detail.EndpointConfig)
	hostname, port, basepath := ParseEndpointURL(epURL)

	api := &models.ManagedAPI{
		APIMApiID:       detail.ID,
		APIName:         detail.Name,
		APIVersion:      detail.Version,
		Context:         detail.Context,
		APIType:         detail.Type,
		LifecycleStatus: detail.LifeCycleStatus,
		Provider:        detail.Provider,
		EndpointType:    epType,
		EndpointURL:     epURL,
		EndpointHostname: hostname,
		EndpointPort:    port,
		EndpointBasepath: basepath,
	}

	// Parse timestamps
	if detail.LastUpdatedTime != "" {
		if t, err := parseAPIMTime(detail.LastUpdatedTime); err == nil {
			api.APIMLastUpdatedAt = &t
		}
	}
	if detail.CreatedTime != "" {
		if t, err := parseAPIMTime(detail.CreatedTime); err == nil {
			api.APIMCreatedAt = &t
		}
	}

	return api
}

// ProcessOperations normalizes operations and computes match paths.
func ProcessOperations(detail *apim.APIDetailResponse, basepath string, passThrough bool) []models.ManagedAPIOperation {
	ops := make([]models.ManagedAPIOperation, 0, len(detail.Operations))

	for _, op := range detail.Operations {
		verb := strings.ToUpper(op.Verb)
		if verb == "" || verb == "OPTIONS" || verb == "HEAD" {
			continue
		}

		normalized := NormalizeTarget(op.Target)
		matchPath := ComputeMatchPath(basepath, detail.Context, normalized, passThrough)

		ops = append(ops, models.ManagedAPIOperation{
			APIMApiID:        detail.ID,
			HTTPMethod:       verb,
			RawTarget:        op.Target,
			NormalizedTarget: normalized,
			MatchPath:        matchPath,
		})
	}

	return ops
}
