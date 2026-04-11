// Package config handles TOML configuration parsing and validation for ADC.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration structure for ADC.
type Config struct {
	Server         ServerConfig         `toml:"server"`
	Discovery      DiscoveryConfig      `toml:"discovery"`
	Managed        ManagedConfig        `toml:"managed"`
	Comparison     ComparisonConfig     `toml:"comparison"`
	SpecGeneration SpecGenerationConfig `toml:"spec_generation"`
	ServiceCatalog ServiceCatalogConfig `toml:"service_catalog"`
	Catalog        CatalogConfig        `toml:"catalog"`
}

// ServerConfig holds runtime server settings.
type ServerConfig struct {
	Hostname      string          `toml:"hostname"`
	Mode          string          `toml:"mode"`
	HealthPort    int             `toml:"health_port"`
	LogLevel      string          `toml:"log_level"`
	LogFormat     string          `toml:"log_format"`
	LogOutput     string          `toml:"log_output"`
	LogMaxSizeMB  int             `toml:"log_max_size_mb"`
	LogMaxBackups int             `toml:"log_max_backups"`
	LogMaxAgeDays int             `toml:"log_max_age_days"`
	Retention     RetentionConfig `toml:"retention"`
}

// RetentionConfig holds data lifecycle policies.
type RetentionConfig struct {
	DiscoveredRetention string `toml:"discovered_retention"`
	ManagedAPIRetention string `toml:"managed_api_retention"`
	UnmanagedRetention  string `toml:"unmanaged_retention"`
}

// DiscoveryConfig holds all Phase 1 discovery settings.
type DiscoveryConfig struct {
	Source        DiscoverySourceConfig `toml:"source"`
	Schedule      ScheduleConfig        `toml:"schedule"`
	TrafficFilter TrafficFilterConfig   `toml:"traffic_filter"`
	NoiseFilter   NoiseFilterConfig     `toml:"noise_filter"`
	Normalization NormalizationConfig   `toml:"normalization"`
}

// DiscoverySourceConfig holds DeepFlow connection settings.
type DiscoverySourceConfig struct {
	Type        string           `toml:"type"`
	ServerIP    string           `toml:"server_ip"`
	QuerierPort int              `toml:"querier_port"`
	Version     string           `toml:"version"`
	QueryMode   string           `toml:"query_mode"`
	ClickHouse  ClickHouseConfig `toml:"clickhouse"`
}

// ClickHouseConfig holds direct ClickHouse connection settings.
type ClickHouseConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Database string `toml:"database"`
}

// ScheduleConfig holds Phase 1 scheduling settings.
type ScheduleConfig struct {
	PollInterval          string `toml:"poll_interval"`
	SafetyLag             string `toml:"safety_lag"`
	MaxSignaturesPerCycle int    `toml:"max_signatures_per_cycle"`
}

// TrafficFilterConfig holds ClickHouse WHERE clause filter settings.
type TrafficFilterConfig struct {
	Protocol          int      `toml:"protocol"`
	L7Protocols       []string `toml:"l7_protocols"`
	MinDirectionScore int      `toml:"min_direction_score"`
	ObservationPoint  string   `toml:"observation_point"`
}

// NoiseFilterConfig holds in-memory noise filtering settings.
type NoiseFilterConfig struct {
	PathPatterns     []string `toml:"path_patterns"`
	PathExact        []string `toml:"path_exact"`
	ExcludedPorts    []int    `toml:"excluded_ports"`
	ExcludedDomains  []string `toml:"excluded_domains"`
	ExcludedAgentIDs []int    `toml:"excluded_agent_ids"`
}

// NormalizationConfig holds path normalization settings.
type NormalizationConfig struct {
	VersionPattern  string   `toml:"version_pattern"`
	BuiltinPatterns []string `toml:"builtin_patterns"`
	UserPatterns    []string `toml:"user_patterns"`
	ExcludePatterns []string `toml:"exclude_patterns"`
}

// ManagedConfig holds all Phase 2 managed API sync settings.
type ManagedConfig struct {
	Source   ManagedSourceConfig `toml:"source"`
	Schedule ManagedSchedule     `toml:"schedule"`
	Sync     ManagedSyncConfig   `toml:"sync"`
}

// ManagedSourceConfig holds APIM connection settings.
type ManagedSourceConfig struct {
	Type       string     `toml:"type"`
	BaseURL    string     `toml:"base_url"`
	APIVersion string     `toml:"api_version"`
	VerifySSL  bool       `toml:"verify_ssl"`
	Auth       AuthConfig `toml:"auth"`
}

// AuthConfig holds APIM authentication settings.
type AuthConfig struct {
	AuthType      string   `toml:"auth_type"`
	Username      string   `toml:"username"`
	Password      string   `toml:"password"`
	ClientID      string   `toml:"client_id"`
	ClientSecret  string   `toml:"client_secret"`
	TokenEndpoint string   `toml:"token_endpoint"`
	Scopes        []string `toml:"scopes"`
}

// ManagedSchedule holds Phase 2 scheduling settings.
type ManagedSchedule struct {
	PollInterval   string `toml:"poll_interval"`
	RequestTimeout string `toml:"request_timeout"`
	MaxRetries     int    `toml:"max_retries"`
	PageSize       int    `toml:"page_size"`
}

// ManagedSyncConfig holds Phase 2 sync behavior settings.
type ManagedSyncConfig struct {
	ContextPassThrough bool     `toml:"context_pass_through"`
	IncludeAPITypes    []string `toml:"include_api_types"`
}

// ComparisonConfig holds Phase 3 comparison settings.
type ComparisonConfig struct {
	Enabled    bool   `toml:"enabled"`
	StaleAfter string `toml:"stale_after"`
}

// SpecGenerationConfig holds Phase 4 OAS generation settings.
type SpecGenerationConfig struct {
	Enabled        bool              `toml:"enabled"`
	OpenAPIVersion string            `toml:"openapi_version"`
	Content        SpecContentConfig `toml:"content"`
}

// SpecContentConfig holds Phase 4 content inclusion settings.
type SpecContentConfig struct {
	IncludeTrafficStats    bool   `toml:"include_traffic_stats"`
	IncludeK8sMetadata     bool   `toml:"include_k8s_metadata"`
	IncludeLegacyMetadata  bool   `toml:"include_legacy_metadata"`
	IncludeNetworkMetadata bool   `toml:"include_network_metadata"`
	IncludeSampleURLs      bool   `toml:"include_sample_urls"`
	ServerURLOverride      string `toml:"server_url_override"`
}

// ServiceCatalogConfig holds Phase 5 catalog push settings.
type ServiceCatalogConfig struct {
	Enabled        bool                `toml:"enabled"`
	AutoPush       bool                `toml:"auto_push"`
	PushShadow     bool                `toml:"push_shadow"`
	PushDrift      bool                `toml:"push_drift"`
	RePushDeleted  bool                `toml:"re_push_deleted"`
	Reconciliation ReconciliationConfig `toml:"reconciliation"`
}

// ReconciliationConfig holds catalog reconciliation settings.
type ReconciliationConfig struct {
	Enabled        bool `toml:"enabled"`
	CleanupUnowned bool `toml:"cleanup_unowned"`
}

// CatalogConfig holds the datastore configuration.
type CatalogConfig struct {
	Datastore DatastoreConfig `toml:"datastore"`
}

// DatastoreConfig holds PostgreSQL connection settings.
type DatastoreConfig struct {
	Type           string `toml:"type"`
	Host           string `toml:"host"`
	Port           int    `toml:"port"`
	Database       string `toml:"database"`
	User           string `toml:"user"`
	Password       string `toml:"password"`
	MaxConnections int    `toml:"max_connections"`
	SSLMode        string `toml:"ssl_mode"`
}

// Load reads and parses a TOML configuration file, applies defaults, and validates.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	applyDefaults(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}
