package models

import "time"

// DiscoveredAPI represents a unique API operation discovered from live traffic.
// Each row is uniquely identified by (service_key, http_method, resource_path).
type DiscoveredAPI struct {
	ID           int
	ServiceKey   string
	HTTPMethod   string
	ResourcePath string

	// Service metadata (from s-p observation point)
	ServiceType   ServiceType
	Protocol      string
	IsTLS         bool
	HTTPVersion   string
	ServerPort    int
	ProcessName   string
	K8sPod        string
	K8sNamespace  string
	K8sWorkload   string
	K8sService    string
	K8sCluster    string
	K8sNode       string
	VMHostname    string
	RequestDomain string
	ServiceIP     string

	// Network metadata (from s observation point)
	SourceIP   string
	HostIP     string
	IsExternal *bool

	// Source (caller) metadata (from c-p observation point)
	SourceServiceIP    string
	SourceProcessName  string
	SourceK8sPod       string
	SourceK8sNamespace string
	SourceK8sService   string
	SourceK8sCluster   string
	SourceVMHostname   string

	// Traffic classification
	TrafficOrigin TrafficOrigin

	// Sample data
	SampleURL  string
	SamplePath string

	// Agent info
	AgentID   int
	AgentName string

	// Response sample
	ResponseCode int
	LatencyUs    int64

	// Lifecycle
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	HitCount    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// APISignature represents a unique API signature from DeepFlow query results
// before enrichment (Step 1 output).
type APISignature struct {
	HTTPMethod    string
	Endpoint      string
	ServerPort    int
	AgentID       int
	HitCount      int64
	SampleURL     string
	SamplePath    string
	RequestDomain string
	IsTLS         bool
	Protocol      string
	HTTPVersion   string
	ResponseCode  int
	LatencyUs     int64
	StartTime     time.Time

	// Set after normalization (Step 3)
	ResourcePath string
}

// NormalizedSignature represents a signature after path normalization and dedup (Step 4 output).
type NormalizedSignature struct {
	HTTPMethod    string
	ResourcePath  string
	ServerPort    int
	AgentID       int
	HitCount      int64
	SampleURL     string
	SamplePath    string
	RequestDomain string
	IsTLS         bool
	Protocol      string
	HTTPVersion   string
	ResponseCode  int
	LatencyUs     int64
	StartTime     time.Time
}

// FusedRecord holds data from all three observation points after enrichment (Step 5 output).
type FusedRecord struct {
	NormalizedSignature

	// s-p observation point
	ProcessName  string
	K8sPod       string
	K8sNamespace string
	K8sWorkload  string
	K8sService   string
	K8sCluster   string
	K8sNode      string
	VMHostname   string
	ServiceIP    string

	// s observation point
	SourceIP   string
	HostIP     string
	IsExternal *bool

	// c-p observation point
	SourceServiceIP    string
	SourceProcessName  string
	SourceK8sPod       string
	SourceK8sNamespace string
	SourceK8sService   string
	SourceK8sCluster   string
	SourceVMHostname   string

	// Computed fields
	ServiceKey    string
	ServiceType   ServiceType
	TrafficOrigin TrafficOrigin
	AgentName     string
}
