// WSO2 API Discovery Controller — Entry Point
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wso2/adc/internal/apim"
	"github.com/wso2/adc/internal/catalog"
	"github.com/wso2/adc/internal/comparison"
	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/deepflow"
	"github.com/wso2/adc/internal/discovery"
	"github.com/wso2/adc/internal/engine"
	"github.com/wso2/adc/internal/health"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/managed"
	"github.com/wso2/adc/internal/specgen"
	"github.com/wso2/adc/internal/store"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "/etc/adc/config.toml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Print version and exit")
	validateOnly := flag.Bool("validate", false, "Validate configuration and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("WSO2 API Discovery Controller %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// ── Stage 1: Configuration ──
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}

	if *validateOnly {
		fmt.Println("Configuration valid.")
		os.Exit(0)
	}

	// ── Stage 2: Logging ──
	logger := logging.New(cfg.Server)
	defer logger.Sync()

	// ── Stage 3: PostgreSQL (with startup retry) ──
	ctx := context.Background()
	db, err := store.ConnectWithRetry(ctx, cfg.Catalog.Datastore, logger)
	if err != nil {
		logger.Fatalw("PostgreSQL connection failed", "error", err)
	}
	defer db.Close()

	// ── Stage 4: Schema Migration ──
	if err := store.Migrate(ctx, db, logger); err != nil {
		logger.Fatalw("Schema migration failed", "error", err)
	}

	// ── Stage 5: DeepFlow client (if discovery enabled, non-fatal) ──
	var dfClient deepflow.Client
	if cfg.Discovery.Source.Type != "" {
		dfClient, err = deepflow.NewClient(cfg.Discovery.Source)
		if err != nil {
			logger.Errorw("DeepFlow unavailable at startup — discovery phase disabled",
				"error", err,
				"server", cfg.Discovery.Source.ServerIP,
				"port", cfg.Discovery.Source.QuerierPort,
			)
		} else {
			defer dfClient.Close()
			logger.Infow("DeepFlow client initialized",
				"server", cfg.Discovery.Source.ServerIP,
				"port", cfg.Discovery.Source.QuerierPort,
				"mode", cfg.Discovery.Source.QueryMode,
			)
		}
	}

	// ── Stage 5b: APIM clients (if managed sync enabled) ──
	var apimPublisher apim.PublisherClient
	var apimCatalog apim.CatalogClient
	if cfg.Managed.Source.Type != "" {
		auth := apim.NewAuthProvider(cfg.Managed)
		apimPublisher = apim.NewPublisherClient(cfg.Managed, auth)
		logger.Infow("APIM publisher client initialized",
			"base_url", cfg.Managed.Source.BaseURL,
			"auth_type", cfg.Managed.Source.Auth.AuthType,
		)
		if cfg.ServiceCatalog.Enabled {
			apimCatalog = apim.NewCatalogClient(cfg.Managed, auth)
			logger.Infow("APIM catalog client initialized",
				"base_url", cfg.Managed.Source.BaseURL,
			)
		}
	}

	// ── Stage 6: Build repositories ──
	repos := store.NewRepositories(db, logger)

	// ── Stage 7: Build phases ──
	phases := engine.Phases{}
	if dfClient != nil {
		phases.Discovery = discovery.New(cfg, dfClient, repos, logger)
	}
	if apimPublisher != nil {
		phases.Managed = managed.New(cfg, apimPublisher, repos, logger)
	}
	if cfg.Comparison.Enabled {
		phases.Comparison = comparison.New(cfg, repos, logger)
		logger.Infow("Comparison phase enabled", "stale_after", cfg.Comparison.StaleAfter)
	}
	if cfg.SpecGeneration.Enabled {
		phases.SpecGen = specgen.New(cfg, repos, logger)
		logger.Infow("Spec generation phase enabled")
	}
	if cfg.ServiceCatalog.Enabled && apimCatalog != nil {
		phases.Catalog = catalog.New(cfg, apimCatalog, repos, logger)
		logger.Infow("Catalog push phase enabled",
			"auto_push", cfg.ServiceCatalog.AutoPush,
			"push_shadow", cfg.ServiceCatalog.PushShadow,
			"push_drift", cfg.ServiceCatalog.PushDrift,
		)

		rec := catalog.BuildReconciler(cfg, apimCatalog, repos, logger)
		if rec != nil {
			phases.Reconciliation = catalog.NewReconcilerPhase(rec)
			logger.Infow("Catalog reconciliation enabled",
				"cleanup_unowned", cfg.ServiceCatalog.Reconciliation.CleanupUnowned,
			)
		}
	}

	// ── Stage 8: Health server ──
	healthSrv := health.New(db, cfg.Server.HealthPort, logger)
	healthSrv.Start()
	defer healthSrv.Stop()

	// ── Stage 9: Engine ──
	eng := engine.New(cfg, phases, repos, logger)

	// ── Graceful shutdown ──
	ctx, cancel := context.WithCancel(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		logger.Infow("Received signal, initiating shutdown", "signal", sig.String())
		cancel()
	}()

	// ── Run ──
	logger.Infow("ADC started",
		"version", version,
		"mode", cfg.Server.Mode,
		"hostname", cfg.Server.Hostname,
	)
	eng.Run(ctx)
	logger.Infow("ADC stopped")
}
