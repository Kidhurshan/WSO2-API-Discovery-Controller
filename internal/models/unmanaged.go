package models

import (
	"encoding/json"
	"time"
)

// UnmanagedAPI represents an API operation detected as unmanaged (SHADOW or DRIFT).
type UnmanagedAPI struct {
	ID               int
	ServiceKey       string
	HTTPMethod       string
	ResourcePath     string
	DiscoveredAPIID  int
	ManagedAPIID     *int
	Classification   Classification
	Confidence       Confidence
	Status           Status
	OpenAPISpec      json.RawMessage
	CatalogServiceID *string
	CatalogVersion   *string
	FirstDetectedAt  time.Time
	LastConfirmedAt  time.Time
	ResolvedAt       *time.Time
	SpecGeneratedAt  *time.Time
	PushedAt         *time.Time
	DismissedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
