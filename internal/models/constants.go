// Package models provides shared data structures and constants across all ADC packages.
package models

// ServiceType identifies the platform hosting a discovered service.
type ServiceType string

const (
	// ServiceTypeKubernetes indicates a service running in Kubernetes with Service/Deployment metadata.
	ServiceTypeKubernetes ServiceType = "KUBERNETES"
	// ServiceTypeContainer indicates a containerized service without K8s orchestration.
	ServiceTypeContainer ServiceType = "CONTAINER"
	// ServiceTypeLegacy indicates a service running directly on a VM or bare-metal host.
	ServiceTypeLegacy ServiceType = "LEGACY"
)

// TrafficOrigin classifies the source of the traffic.
type TrafficOrigin string

const (
	// TrafficOriginExternal indicates the caller is outside the monitored infrastructure.
	TrafficOriginExternal TrafficOrigin = "EXTERNAL"
	// TrafficOriginInternal indicates the caller is internal but not a monitored service.
	TrafficOriginInternal TrafficOrigin = "INTERNAL"
	// TrafficOriginServiceToService indicates the caller is another monitored service/pod.
	TrafficOriginServiceToService TrafficOrigin = "SERVICE_TO_SERVICE"
	// TrafficOriginContainerToService indicates the caller is a container calling a service.
	TrafficOriginContainerToService TrafficOrigin = "CONTAINER_TO_SERVICE"
	// TrafficOriginUnknown indicates the traffic origin could not be determined.
	TrafficOriginUnknown TrafficOrigin = "UNKNOWN"
)

// Classification categorizes an unmanaged API.
type Classification string

const (
	// ClassificationShadow indicates an entire service is unmanaged.
	ClassificationShadow Classification = "SHADOW"
	// ClassificationDrift indicates a specific operation is unmanaged on a managed service.
	ClassificationDrift Classification = "DRIFT"
)

// Confidence indicates the reliability of the classification.
type Confidence string

const (
	// ConfidenceHigh indicates a clean path match with no collision.
	ConfidenceHigh Confidence = "HIGH"
	// ConfidenceLow indicates the classification was resolved via collision logic.
	ConfidenceLow Confidence = "LOW"
	// ConfidenceAmbiguous indicates the collision resolution was tied.
	ConfidenceAmbiguous Confidence = "AMBIGUOUS"
)

// Status tracks the lifecycle of an unmanaged API.
type Status string

const (
	// StatusDetected indicates Phase 3 identified this as unmanaged.
	StatusDetected Status = "DETECTED"
	// StatusSpecGenerated indicates Phase 4 created the OpenAPI specification.
	StatusSpecGenerated Status = "SPEC_GENERATED"
	// StatusPushed indicates Phase 5 sent the spec to APIM Service Catalog.
	StatusPushed Status = "PUSHED"
	// StatusDismissed indicates the operator intentionally dismissed this finding.
	StatusDismissed Status = "DISMISSED"
	// StatusResolved indicates the API was later registered in APIM.
	StatusResolved Status = "RESOLVED"
	// StatusStale indicates traffic stopped for the stale_after period.
	StatusStale Status = "STALE"
)
