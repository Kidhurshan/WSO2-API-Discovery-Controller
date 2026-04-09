// Package deepflow provides client interfaces for querying DeepFlow's l7_flow_log data.
package deepflow

import (
	"context"

	"github.com/wso2/adc/internal/config"
)

// Client defines the interface for querying DeepFlow traffic data.
type Client interface {
	// Query executes a SQL query and returns rows as maps.
	Query(ctx context.Context, sql string) ([]map[string]interface{}, error)
	// Ping checks DeepFlow connectivity.
	Ping(ctx context.Context) error
	// Close releases resources.
	Close()
}

// NewClient creates a new DeepFlow client based on the configured query mode.
func NewClient(cfg config.DiscoverySourceConfig) (Client, error) {
	switch cfg.QueryMode {
	case "sql_api":
		return newQuerierClient(cfg)
	default:
		return newQuerierClient(cfg)
	}
}
