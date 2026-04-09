package models

import "time"

// ManagedAPI represents an API published in WSO2 APIM.
type ManagedAPI struct {
	ID                int
	APIMApiID         string
	APIName           string
	APIVersion        string
	Context           string
	APIType           string
	LifecycleStatus   string
	Provider          string
	EndpointType      string
	EndpointURL       string
	EndpointHostname  string
	EndpointPort      int
	EndpointBasepath  string
	APIMLastUpdatedAt *time.Time
	APIMCreatedAt     *time.Time
	DeletedAt         *time.Time
	LastSyncedAt      time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ManagedAPIOperation represents a single operation (verb + target) of a managed API.
type ManagedAPIOperation struct {
	ID               int
	APIMApiID        string
	HTTPMethod       string
	RawTarget        string
	NormalizedTarget string
	MatchPath        string
	ManagedAPIPK     int // adc_managed_apis.id (populated by Phase 3 queries)
	DeletedAt        *time.Time
	CreatedAt        time.Time
}
