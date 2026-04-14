package deepflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/httputil"
)

// querierClient implements Client using DeepFlow's SQL API.
type querierClient struct {
	baseURL    string
	httpClient *http.Client
	retryCfg   httputil.RetryConfig
}

func newQuerierClient(cfg config.DiscoverySourceConfig) (*querierClient, error) {
	baseURL := fmt.Sprintf("http://%s:%d", cfg.ServerIP, cfg.QuerierPort)
	return &querierClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		retryCfg: httputil.DefaultRetryConfig(),
	}, nil
}

// Query executes a SQL query against DeepFlow's SQL API.
func (c *querierClient) Query(ctx context.Context, sql string) ([]map[string]interface{}, error) {
	body := map[string]string{
		"db":  "flow_log",
		"sql": sql,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	queryURL := c.baseURL + "/v1/query/"
	reqFactory := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, queryURL, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return req, nil
	}

	resp, err := httputil.DoWithRetry(ctx, c.httpClient, reqFactory, c.retryCfg)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputil.ReadResponseBody(resp, 0)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result querierResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if result.OptStatus != "" && result.OptStatus != "SUCCESS" {
		return nil, fmt.Errorf("query error: %s - %s", result.OptStatus, result.Description)
	}

	return convertResult(result.Result), nil
}

// Ping checks connectivity to the DeepFlow querier.
func (c *querierClient) Ping(ctx context.Context) error {
	_, err := c.Query(ctx, "SELECT 1")
	return err
}

// Close releases resources.
func (c *querierClient) Close() {}

// querierResponse represents the DeepFlow SQL API response format.
type querierResponse struct {
	OptStatus   string          `json:"OPT_STATUS"`
	Description string          `json:"DESCRIPTION"`
	Result      json.RawMessage `json:"result"`
}

// convertResult converts DeepFlow's column-based response into row maps.
func convertResult(raw json.RawMessage) []map[string]interface{} {
	// DeepFlow returns {"columns":["col1","col2"], "values":[[v1,v2],[v3,v4]]}
	var structured struct {
		Columns []string        `json:"columns"`
		Values  [][]interface{} `json:"values"`
	}

	if err := json.Unmarshal(raw, &structured); err != nil {
		// Try as direct array of maps
		var rows []map[string]interface{}
		if err2 := json.Unmarshal(raw, &rows); err2 == nil {
			return rows
		}
		return nil
	}

	rows := make([]map[string]interface{}, 0, len(structured.Values))
	for _, vals := range structured.Values {
		row := make(map[string]interface{}, len(structured.Columns))
		for i, col := range structured.Columns {
			if i < len(vals) {
				row[col] = vals[i]
			}
		}
		rows = append(rows, row)
	}
	return rows
}
