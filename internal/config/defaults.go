package config

func applyDefaults(cfg *Config) {
	// Server defaults
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "standalone"
	}
	if cfg.Server.LogLevel == "" {
		cfg.Server.LogLevel = "info"
	}
	if cfg.Server.LogFormat == "" {
		cfg.Server.LogFormat = "json"
	}
	if cfg.Server.LogMaxSizeMB <= 0 {
		cfg.Server.LogMaxSizeMB = 50
	}
	if cfg.Server.LogMaxBackups <= 0 {
		cfg.Server.LogMaxBackups = 3
	}
	if cfg.Server.LogMaxAgeDays <= 0 {
		cfg.Server.LogMaxAgeDays = 7
	}
	if cfg.Server.HealthPort <= 0 {
		cfg.Server.HealthPort = 8090
	}

	// Retention defaults
	if cfg.Server.Retention.DiscoveredRetention == "" {
		cfg.Server.Retention.DiscoveredRetention = "30d"
	}
	if cfg.Server.Retention.ManagedAPIRetention == "" {
		cfg.Server.Retention.ManagedAPIRetention = "90d"
	}
	if cfg.Server.Retention.UnmanagedRetention == "" {
		cfg.Server.Retention.UnmanagedRetention = "90d"
	}

	// Discovery defaults
	if cfg.Discovery.Schedule.PollInterval == "" && cfg.Discovery.Source.Type != "" {
		cfg.Discovery.Schedule.PollInterval = "5m"
	}
	if cfg.Discovery.Schedule.SafetyLag == "" && cfg.Discovery.Source.Type != "" {
		cfg.Discovery.Schedule.SafetyLag = "2m"
	}
	if cfg.Discovery.Schedule.MaxSignaturesPerCycle <= 0 {
		cfg.Discovery.Schedule.MaxSignaturesPerCycle = 1000
	}
	if cfg.Discovery.TrafficFilter.Protocol <= 0 {
		cfg.Discovery.TrafficFilter.Protocol = 6
	}
	if cfg.Discovery.TrafficFilter.MinDirectionScore <= 0 {
		cfg.Discovery.TrafficFilter.MinDirectionScore = 200
	}
	if cfg.Discovery.TrafficFilter.ObservationPoint == "" {
		cfg.Discovery.TrafficFilter.ObservationPoint = "s-p"
	}

	// Managed defaults
	if cfg.Managed.Schedule.PollInterval == "" && cfg.Managed.Source.Type != "" {
		cfg.Managed.Schedule.PollInterval = "10m"
	}
	if cfg.Managed.Schedule.RequestTimeout == "" {
		cfg.Managed.Schedule.RequestTimeout = "30s"
	}
	if cfg.Managed.Schedule.MaxRetries <= 0 {
		cfg.Managed.Schedule.MaxRetries = 3
	}
	if cfg.Managed.Schedule.PageSize <= 0 {
		cfg.Managed.Schedule.PageSize = 500
	}

	// Comparison defaults
	if cfg.Comparison.StaleAfter == "" {
		cfg.Comparison.StaleAfter = "7d"
	}

	// Spec generation defaults
	if cfg.SpecGeneration.OpenAPIVersion == "" {
		cfg.SpecGeneration.OpenAPIVersion = "3.0.3"
	}

	// Datastore defaults
	if cfg.Catalog.Datastore.Port <= 0 {
		cfg.Catalog.Datastore.Port = 5432
	}
	if cfg.Catalog.Datastore.MaxConnections <= 0 {
		cfg.Catalog.Datastore.MaxConnections = 10
	}
	if cfg.Catalog.Datastore.SSLMode == "" {
		cfg.Catalog.Datastore.SSLMode = "disable"
	}
}
