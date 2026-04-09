package apim

import "encoding/json"

// APIListResponse represents the response from GET /apis.
type APIListResponse struct {
	Count      int          `json:"count"`
	List       []APISummary `json:"list"`
	Pagination Pagination   `json:"pagination"`
}

// APISummary is the lightweight API summary from the list endpoint.
type APISummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Context         string `json:"context"`
	LifeCycleStatus string `json:"lifeCycleStatus"`
	Type            string `json:"type"`
	Provider        string `json:"provider"`
}

// Pagination holds pagination info from APIM responses.
type Pagination struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// APIDetailResponse represents the full API detail from GET /apis/{apiId}.
type APIDetailResponse struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	Context          string          `json:"context"`
	LifeCycleStatus  string          `json:"lifeCycleStatus"`
	Type             string          `json:"type"`
	Provider         string          `json:"provider"`
	Operations       []Operation     `json:"operations"`
	EndpointConfig   json.RawMessage `json:"endpointConfig"`
	LastUpdatedTime  string          `json:"lastUpdatedTime"`
	CreatedTime      string          `json:"createdTime"`
}

// Operation represents a single API operation from the detail response.
type Operation struct {
	Target string `json:"target"`
	Verb   string `json:"verb"`
}

// EndpointConfig represents the parsed endpoint configuration.
type EndpointConfig struct {
	EndpointType        string          `json:"endpoint_type"`
	ProductionEndpoints json.RawMessage `json:"production_endpoints"`
}

// EndpointURL represents a single endpoint URL entry.
type EndpointURL struct {
	URL string `json:"url"`
}
