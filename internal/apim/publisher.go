package apim

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/httputil"
)

// PublisherClient defines the interface for interacting with WSO2 APIM Publisher API.
type PublisherClient interface {
	// ListPublishedAPIs returns all PUBLISHED APIs matching the configured types.
	ListPublishedAPIs(ctx context.Context) ([]APISummary, error)
	// GetAPIDetail returns the full detail for a specific API.
	GetAPIDetail(ctx context.Context, apiID string) (*APIDetailResponse, error)
}

// publisherClient implements PublisherClient.
type publisherClient struct {
	baseURL    string
	apiVersion string
	auth       AuthProvider
	httpClient *http.Client
	pageSize   int
	apiTypes   []string
	retryCfg   httputil.RetryConfig
}

// NewPublisherClient creates a new publisher client.
func NewPublisherClient(cfg config.ManagedConfig, auth AuthProvider) PublisherClient {
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

	return &publisherClient{
		baseURL:    cfg.Source.BaseURL,
		apiVersion: cfg.Source.APIVersion,
		auth:       auth,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		pageSize: cfg.Schedule.PageSize,
		apiTypes: cfg.Sync.IncludeAPITypes,
		retryCfg: retryCfg,
	}
}

// ListPublishedAPIs fetches all published APIs with pagination.
func (c *publisherClient) ListPublishedAPIs(ctx context.Context) ([]APISummary, error) {
	var allAPIs []APISummary
	offset := 0
	typeSet := make(map[string]bool, len(c.apiTypes))
	for _, t := range c.apiTypes {
		typeSet[t] = true
	}

	for {
		path := fmt.Sprintf("/api/am/publisher/%s/apis?query=status:PUBLISHED&limit=%d&offset=%d",
			c.apiVersion, c.pageSize, offset)

		body, err := c.doGet(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("list APIs (offset=%d): %w", offset, err)
		}

		var resp APIListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parse API list response: %w", err)
		}

		for _, api := range resp.List {
			if len(typeSet) == 0 || typeSet[api.Type] {
				allAPIs = append(allAPIs, api)
			}
		}

		if resp.Pagination.Total <= offset+c.pageSize {
			break
		}
		offset += c.pageSize
	}

	return allAPIs, nil
}

// GetAPIDetail fetches the full detail for a specific API.
func (c *publisherClient) GetAPIDetail(ctx context.Context, apiID string) (*APIDetailResponse, error) {
	path := fmt.Sprintf("/api/am/publisher/%s/apis/%s", c.apiVersion, url.PathEscape(apiID))

	body, err := c.doGet(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("get API detail %s: %w", apiID, err)
	}

	var detail APIDetailResponse
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, fmt.Errorf("parse API detail %s: %w", apiID, err)
	}

	return &detail, nil
}

func (c *publisherClient) doGet(ctx context.Context, path string) ([]byte, error) {
	fullURL := c.baseURL + path

	reqFactory := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		authHeader, err := c.auth.AuthHeader()
		if err != nil {
			return nil, fmt.Errorf("get auth header: %w", err)
		}
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Accept", "application/json")
		return req, nil
	}

	resp, err := httputil.DoWithRetry(ctx, c.httpClient, reqFactory, c.retryCfg)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found (HTTP 404)")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		truncated := string(body)
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncated)
	}

	return body, nil
}
