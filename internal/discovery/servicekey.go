package discovery

import (
	"fmt"
	"strings"

	"github.com/wso2/adc/internal/models"
)

// ComputeServiceKey determines the service_key using the 6-priority chain.
func ComputeServiceKey(rec *models.FusedRecord) string {
	// Priority 1: K8s Service name
	if rec.K8sService != "" {
		if rec.K8sNamespace != "" {
			return rec.K8sNamespace + "/" + rec.K8sService
		}
		return rec.K8sService
	}

	// Priority 2: K8s Workload name
	if rec.K8sWorkload != "" {
		if rec.K8sNamespace != "" {
			return rec.K8sNamespace + "/" + rec.K8sWorkload
		}
		return rec.K8sWorkload
	}

	// Priority 3: DNS name from Host header
	domain := rec.RequestDomain
	if domain != "" {
		// Strip port
		if idx := strings.LastIndex(domain, ":"); idx > 0 {
			domain = domain[:idx]
		}
		if isValidDNSName(domain) {
			return domain
		}
	}

	// Priority 4: Host IP:port (from s observation)
	if rec.HostIP != "" && !isLoopback(rec.HostIP) {
		return fmt.Sprintf("%s:%d", rec.HostIP, rec.ServerPort)
	}

	// Priority 5: Service IP:port (from s-p observation)
	if rec.ServiceIP != "" && !isLoopback(rec.ServiceIP) {
		return fmt.Sprintf("%s:%d", rec.ServiceIP, rec.ServerPort)
	}

	// Priority 6: Agent fallback
	if rec.AgentName != "" {
		return fmt.Sprintf("%s:%d", rec.AgentName, rec.ServerPort)
	}
	return fmt.Sprintf("agent-%d:%d", rec.AgentID, rec.ServerPort)
}

// ComputeServiceType determines the service_type from metadata.
func ComputeServiceType(rec *models.FusedRecord) models.ServiceType {
	if rec.K8sService != "" || rec.K8sWorkload != "" {
		return models.ServiceTypeKubernetes
	}
	if rec.K8sPod != "" {
		return models.ServiceTypeContainer
	}
	return models.ServiceTypeLegacy
}

// ComputeTrafficOrigin determines the traffic_origin from observation point data.
func ComputeTrafficOrigin(rec *models.FusedRecord) models.TrafficOrigin {
	// Check c-p data (client-pod)
	if rec.SourceK8sService != "" {
		return models.TrafficOriginServiceToService
	}
	if rec.SourceK8sPod != "" {
		return models.TrafficOriginContainerToService
	}
	if rec.SourceProcessName != "" || rec.SourceServiceIP != "" {
		return models.TrafficOriginInternal
	}

	// Check s data (server network)
	if rec.IsExternal != nil {
		if *rec.IsExternal {
			return models.TrafficOriginExternal
		}
		return models.TrafficOriginInternal
	}

	return models.TrafficOriginUnknown
}

func isValidDNSName(s string) bool {
	if s == "" || s == "localhost" || s == "0.0.0.0" {
		return false
	}
	// Must contain at least one dot and one letter
	hasDot := strings.Contains(s, ".")
	hasLetter := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
			break
		}
	}
	return hasDot && hasLetter
}

func isLoopback(ip string) bool {
	return ip == "" || ip == "127.0.0.1" || ip == "0.0.0.0" || ip == "::1"
}
