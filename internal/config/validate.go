package config

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var dayDurationRegex = regexp.MustCompile(`^(\d+)d$`)

// Validate performs cross-field validation on the configuration.
func (c *Config) Validate() error {
	// Server
	if err := validateEnum(c.Server.Mode, []string{"standalone", "kubernetes"}, "[server].mode"); err != nil {
		return err
	}
	if err := validateEnum(c.Server.LogLevel, []string{"debug", "info", "warn", "error"}, "[server].log_level"); err != nil {
		return err
	}
	if err := validateEnum(c.Server.LogFormat, []string{"json", "text"}, "[server].log_format"); err != nil {
		return err
	}

	// Discovery (optional — empty source.type means disabled)
	if c.Discovery.Source.Type != "" {
		if err := validateRequired(c.Discovery.Source.ServerIP, "[discovery.source].server_ip"); err != nil {
			return err
		}
		if c.Discovery.Source.QuerierPort <= 0 {
			return fmt.Errorf("[discovery.source].querier_port must be a positive integer")
		}
		if err := validateEnum(c.Discovery.Source.QueryMode, []string{"sql_api", "clickhouse"}, "[discovery.source].query_mode"); err != nil {
			return err
		}
		if err := validateDuration(c.Discovery.Schedule.PollInterval, "[discovery.schedule].poll_interval"); err != nil {
			return err
		}
		if err := validateDuration(c.Discovery.Schedule.SafetyLag, "[discovery.schedule].safety_lag"); err != nil {
			return err
		}
	}

	// Managed (optional)
	if c.Managed.Source.Type != "" {
		if err := validateRequired(c.Managed.Source.BaseURL, "[managed.source].base_url"); err != nil {
			return err
		}
		if err := validateAuth(c.Managed.Source.Auth); err != nil {
			return err
		}
		if err := validateDuration(c.Managed.Schedule.PollInterval, "[managed.schedule].poll_interval"); err != nil {
			return err
		}
	}

	// Cross-phase dependencies
	if c.Comparison.Enabled && c.Managed.Source.Type == "" {
		return fmt.Errorf("[comparison].enabled=true requires [managed.source] to be configured")
	}
	if c.ServiceCatalog.Enabled && c.Managed.Source.Type == "" {
		return fmt.Errorf("[service_catalog].enabled=true requires [managed.source] for authentication")
	}
	if c.ServiceCatalog.Reconciliation.CleanupUnowned && !c.ServiceCatalog.Reconciliation.Enabled {
		return fmt.Errorf("[service_catalog.reconciliation].cleanup_unowned=true requires reconciliation.enabled=true")
	}

	// Comparison
	if c.Comparison.Enabled && c.Comparison.StaleAfter != "" {
		if err := validateDuration(c.Comparison.StaleAfter, "[comparison].stale_after"); err != nil {
			return err
		}
	}

	// Database (always required)
	if err := validateRequired(c.Catalog.Datastore.Host, "[catalog.datastore].host"); err != nil {
		return err
	}
	if c.Catalog.Datastore.Port <= 0 {
		return fmt.Errorf("[catalog.datastore].port must be a positive integer")
	}
	if err := validateRequired(c.Catalog.Datastore.Database, "[catalog.datastore].database"); err != nil {
		return err
	}
	if err := validateRequired(c.Catalog.Datastore.User, "[catalog.datastore].user"); err != nil {
		return err
	}
	if err := validateRequired(c.Catalog.Datastore.Password, "[catalog.datastore].password"); err != nil {
		return err
	}
	if err := validateEnum(c.Catalog.Datastore.SSLMode,
		[]string{"disable", "require", "verify-ca", "verify-full"},
		"[catalog.datastore].ssl_mode"); err != nil {
		return err
	}

	return nil
}

// SecurityWarnings returns non-fatal security advisories for the current configuration.
func (c *Config) SecurityWarnings() []string {
	var warnings []string

	if c.Managed.Source.Type != "" && !c.Managed.Source.VerifySSL {
		warnings = append(warnings,
			"[managed.source].verify_ssl is false — TLS certificate verification "+
				"disabled for all APIM connections. Set to true for production")
	}

	if c.Catalog.Datastore.SSLMode == "disable" &&
		c.Catalog.Datastore.Host != "localhost" &&
		c.Catalog.Datastore.Host != "127.0.0.1" {
		warnings = append(warnings,
			fmt.Sprintf("[catalog.datastore].ssl_mode is \"disable\" for host %q — "+
				"database traffic is unencrypted. Set to \"require\" or stronger "+
				"for non-localhost databases", c.Catalog.Datastore.Host))
	}

	return warnings
}

func validateRequired(value, field string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

func validateEnum(value string, allowed []string, field string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %v, got %q", field, allowed, value)
}

func validateDuration(value, field string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, err := ParseDuration(value); err != nil {
		return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
	}
	return nil
}

// ParseDuration parses a duration string that supports Go's standard format
// plus day-based durations like "7d", "30d", "90d".
func ParseDuration(s string) (time.Duration, error) {
	if matches := dayDurationRegex.FindStringSubmatch(s); matches != nil {
		days, _ := strconv.Atoi(matches[1])
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func validateAuth(auth AuthConfig) error {
	if err := validateEnum(auth.AuthType, []string{"basic", "oauth2"}, "[managed.source.auth].auth_type"); err != nil {
		return err
	}
	switch auth.AuthType {
	case "basic":
		if err := validateRequired(auth.Username, "[managed.source.auth].username"); err != nil {
			return err
		}
		if err := validateRequired(auth.Password, "[managed.source.auth].password"); err != nil {
			return err
		}
	case "oauth2":
		if err := validateRequired(auth.ClientID, "[managed.source.auth].client_id"); err != nil {
			return err
		}
		if err := validateRequired(auth.ClientSecret, "[managed.source.auth].client_secret"); err != nil {
			return err
		}
		if err := validateRequired(auth.TokenEndpoint, "[managed.source.auth].token_endpoint"); err != nil {
			return err
		}
	}
	return nil
}
