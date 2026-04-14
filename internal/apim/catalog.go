package apim

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/httputil"
)

// CatalogClient defines the interface for WSO2 APIM Service Catalog API.
type CatalogClient interface {
	// GetService checks if a catalog entry exists by serviceId.
	// Returns the service ID if found, empty string if 404, or error.
	GetService(ctx context.Context, serviceID string) (string, error)
	// SearchByName searches for a catalog entry by exact name.
	// Returns the service ID if found, empty string if not found, or error.
	SearchByName(ctx context.Context, name string) (string, error)
	// CreateService creates a new catalog entry via multipart POST.
	// Returns the assigned service ID.
	CreateService(ctx context.Context, metadata ServiceMetadata, definitionJSON []byte) (string, error)
	// UpdateService updates an existing catalog entry via multipart PUT.
	UpdateService(ctx context.Context, serviceID string, metadata ServiceMetadata, definitionJSON []byte) error
	// ListServices returns a page of catalog entries. Returns entries, total count, error.
	ListServices(ctx context.Context, limit, offset int) ([]ServiceListEntry, int, error)
	// DeleteService deletes a catalog entry by ID.
	DeleteService(ctx context.Context, serviceID string) error
}

// ServiceListEntry is a single entry from the list services response.
type ServiceListEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// ServiceMetadata is the JSON payload for the serviceMetadata part.
type ServiceMetadata struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Description    string `json:"description"`
	ServiceURL     string `json:"serviceUrl"`
	DefinitionType string `json:"definitionType"`
	MutualSSL      bool   `json:"mutualSSLEnabled"`
}

// CatalogServiceResponse is the response from POST/GET /services.
type CatalogServiceResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// CatalogListResponse is the response from GET /services (search/list).
type CatalogListResponse struct {
	Count      int                      `json:"count"`
	List       []CatalogServiceResponse `json:"list"`
	Pagination CatalogPagination        `json:"pagination"`
}

// CatalogPagination holds pagination metadata from service catalog responses.
type CatalogPagination struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// catalogClient implements CatalogClient.
type catalogClient struct {
	baseURL    string
	auth       AuthProvider
	httpClient *http.Client
	retryCfg   httputil.RetryConfig
}

// NewCatalogClient creates a new catalog client that reuses the managed auth provider.
func NewCatalogClient(cfg config.ManagedConfig, auth AuthProvider) CatalogClient {
	timeout, err := time.ParseDuration(cfg.Schedule.RequestTimeout)
	if err != nil {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.Source.VerifySSL},
	}

	retryCfg := httputil.DefaultRetryConfig()
	if cfg.Schedule.MaxRetries > 0 {
		retryCfg.MaxAttempts = cfg.Schedule.MaxRetries
	}

	return &catalogClient{
		baseURL: cfg.Source.BaseURL,
		auth:    auth,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		retryCfg: retryCfg,
	}
}

// GetService checks if a service exists in the catalog by ID.
func (c *catalogClient) GetService(ctx context.Context, serviceID string) (string, error) {
	path := fmt.Sprintf("/api/am/service-catalog/v1/services/%s", serviceID)
	body, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", fmt.Errorf("get service %s: %w", serviceID, err)
	}

	if statusCode == http.StatusNotFound {
		return "", nil
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("get service %s: HTTP %d", serviceID, statusCode)
	}

	var resp CatalogServiceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse get service response: %w", err)
	}
	return resp.ID, nil
}

// SearchByName searches for a catalog entry by name.
func (c *catalogClient) SearchByName(ctx context.Context, name string) (string, error) {
	path := fmt.Sprintf("/api/am/service-catalog/v1/services?name=%s&limit=1", url.QueryEscape(name))
	body, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", fmt.Errorf("search by name %s: %w", name, err)
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("search by name %s: HTTP %d", name, statusCode)
	}

	var resp CatalogListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse search response: %w", err)
	}
	if resp.Count > 0 && len(resp.List) > 0 {
		return resp.List[0].ID, nil
	}
	return "", nil
}

// CreateService creates a new service via multipart POST.
func (c *catalogClient) CreateService(ctx context.Context, metadata ServiceMetadata, definitionJSON []byte) (string, error) {
	body, contentType, err := buildMultipartPayload(metadata, definitionJSON)
	if err != nil {
		return "", fmt.Errorf("build multipart for %s: %w", metadata.Name, err)
	}

	path := "/api/am/service-catalog/v1/services"
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, path, body, contentType)
	if err != nil {
		return "", fmt.Errorf("create service %s: %w", metadata.Name, err)
	}

	if statusCode == http.StatusConflict {
		return "", &ConflictError{Name: metadata.Name}
	}
	if statusCode != http.StatusCreated && statusCode != http.StatusOK {
		truncated := string(respBody)
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		return "", fmt.Errorf("create service %s: HTTP %d: %s", metadata.Name, statusCode, truncated)
	}

	var resp CatalogServiceResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("parse create response for %s: %w", metadata.Name, err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("create service %s: empty ID in response", metadata.Name)
	}
	return resp.ID, nil
}

// UpdateService updates an existing service via multipart PUT.
func (c *catalogClient) UpdateService(ctx context.Context, serviceID string, metadata ServiceMetadata, definitionJSON []byte) error {
	body, contentType, err := buildMultipartPayload(metadata, definitionJSON)
	if err != nil {
		return fmt.Errorf("build multipart for %s: %w", metadata.Name, err)
	}

	path := fmt.Sprintf("/api/am/service-catalog/v1/services/%s", serviceID)
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPut, path, body, contentType)
	if err != nil {
		return fmt.Errorf("update service %s: %w", serviceID, err)
	}

	if statusCode != http.StatusOK {
		truncated := string(respBody)
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		return fmt.Errorf("update service %s: HTTP %d: %s", serviceID, statusCode, truncated)
	}
	return nil
}

// ListServices returns a paginated list of catalog entries.
func (c *catalogClient) ListServices(ctx context.Context, limit, offset int) ([]ServiceListEntry, int, error) {
	path := fmt.Sprintf("/api/am/service-catalog/v1/services?limit=%d&offset=%d", limit, offset)
	body, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, 0, fmt.Errorf("list services: %w", err)
	}
	if statusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("list services: HTTP %d", statusCode)
	}

	var resp CatalogListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("parse list services response: %w", err)
	}

	entries := make([]ServiceListEntry, 0, len(resp.List))
	for _, s := range resp.List {
		entries = append(entries, ServiceListEntry{
			ID:          s.ID,
			Name:        s.Name,
			Version:     s.Version,
			Description: s.Description,
		})
	}
	return entries, resp.Pagination.Total, nil
}

// DeleteService deletes a catalog entry by ID.
func (c *catalogClient) DeleteService(ctx context.Context, serviceID string) error {
	path := fmt.Sprintf("/api/am/service-catalog/v1/services/%s", serviceID)
	_, statusCode, err := c.doRequest(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return fmt.Errorf("delete service %s: %w", serviceID, err)
	}
	if statusCode != http.StatusNoContent && statusCode != http.StatusOK {
		return fmt.Errorf("delete service %s: HTTP %d", serviceID, statusCode)
	}
	return nil
}

// ConflictError indicates a 409 Conflict (name already exists).
type ConflictError struct {
	Name string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("service catalog conflict: name %q already exists", e.Name)
}

// doRequest executes an HTTP request with auth and retry. Returns body, status code, error.
func (c *catalogClient) doRequest(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, int, error) {
	fullURL := c.baseURL + path

	reqFactory := func() (*http.Request, error) {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		authHeader, err := c.auth.AuthHeader()
		if err != nil {
			return nil, fmt.Errorf("get auth header: %w", err)
		}
		req.Header.Set("Authorization", authHeader)

		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if method == http.MethodGet {
			req.Header.Set("Accept", "application/json")
		}
		return req, nil
	}

	resp, err := httputil.DoWithRetry(ctx, c.httpClient, reqFactory, c.retryCfg)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputil.ReadResponseBody(resp, 0)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// buildMultipartPayload creates the multipart/form-data body for POST/PUT.
func buildMultipartPayload(metadata ServiceMetadata, definitionJSON []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Part 1: serviceMetadata (application/json)
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Disposition", `form-data; name="serviceMetadata"`)
	metaHeader.Set("Content-Type", "application/json")

	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return nil, "", fmt.Errorf("create metadata part: %w", err)
	}

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", fmt.Errorf("marshal metadata: %w", err)
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return nil, "", fmt.Errorf("write metadata: %w", err)
	}

	// Part 2: definitionFile (application/json, with filename)
	defHeader := make(textproto.MIMEHeader)
	defHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="definitionFile"; filename="%s.json"`, metadata.Name))
	defHeader.Set("Content-Type", "application/json")

	defPart, err := writer.CreatePart(defHeader)
	if err != nil {
		return nil, "", fmt.Errorf("create definition part: %w", err)
	}
	if _, err := defPart.Write(definitionJSON); err != nil {
		return nil, "", fmt.Errorf("write definition: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}
