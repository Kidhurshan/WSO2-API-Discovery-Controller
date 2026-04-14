// Package engine orchestrates the ADC pipeline cycle loop and phase scheduling.
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/adc/internal/config"
	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

// Phase defines the interface for each pipeline phase.
type Phase interface {
	// Name returns the human-readable phase name.
	Name() string
	// Run executes the phase for the given cycle.
	Run(ctx context.Context, cycleID string) error
}

// Phases holds all pipeline phases.
type Phases struct {
	Discovery      Phase
	Managed        Phase
	Comparison     Phase
	SpecGen        Phase
	Catalog        Phase
	Reconciliation Phase
}

// Engine orchestrates the ADC pipeline cycle loop.
type Engine struct {
	cfg                  *config.Config
	phases               Phases
	repos                *store.Repositories
	logger               *logging.Logger
	phase2LastRun        time.Time
	phase2LastSuccess    time.Time
	cleanupLastRun       time.Time
	reconcileLastRun     time.Time
	cleanup              *Cleanup
	breakers             map[string]*CircuitBreaker
	firstFullCycleComplete bool
}

// New creates a new Engine.
func New(cfg *config.Config, phases Phases, repos *store.Repositories, logger *logging.Logger) *Engine {
	e := &Engine{
		cfg:    cfg,
		phases: phases,
		repos:  repos,
		logger: logger,
		breakers: map[string]*CircuitBreaker{
			"discovery": NewCircuitBreaker("discovery", 3, 30*time.Minute, logger),
			"managed":   NewCircuitBreaker("managed", 3, 30*time.Minute, logger),
			"catalog":   NewCircuitBreaker("catalog", 3, 30*time.Minute, logger),
		},
		cleanup: NewCleanup(cfg.Server.Retention, repos.DB(), logger),
	}
	return e
}

// Run starts the main cycle loop. It blocks until the context is cancelled.
func (e *Engine) Run(ctx context.Context) {
	interval, err := time.ParseDuration(e.cfg.Discovery.Schedule.PollInterval)
	if err != nil {
		// Fallback if discovery is not configured
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// First cycle immediately
	e.runCycle(ctx, "startup")

	for {
		select {
		case <-ticker.C:
			e.runCycle(ctx, "tick")
		case <-ctx.Done():
			e.logger.Infow("Shutdown signal received — stopping engine")
			return
		}
	}
}

func (e *Engine) runCycle(ctx context.Context, trigger string) {
	cycleID := fmt.Sprintf("cycle-%s", time.Now().Format("20060102-150405"))
	start := time.Now()
	var phasesRun []string

	// Phase 1: Traffic Discovery
	if e.phases.Discovery != nil && e.cfg.Discovery.Source.Type != "" {
		if e.breakers["discovery"].ShouldAttempt() {
			if err := e.runPhase(ctx, cycleID, e.phases.Discovery); err != nil {
				e.breakers["discovery"].RecordFailure()
			} else {
				e.breakers["discovery"].RecordSuccess()
			}
			phasesRun = append(phasesRun, "discovery")
		}
	}

	// Phase 2: Managed API Sync (on its own interval)
	if e.phases.Managed != nil && e.cfg.Managed.Source.Type != "" && e.shouldRunPhase2() {
		if e.breakers["managed"].ShouldAttempt() {
			if err := e.runPhase(ctx, cycleID, e.phases.Managed); err != nil {
				e.breakers["managed"].RecordFailure()
			} else {
				e.breakers["managed"].RecordSuccess()
				e.phase2LastRun = time.Now()
				e.phase2LastSuccess = time.Now()
			}
			phasesRun = append(phasesRun, "managed")
		}
	}

	// Phase 3: Comparison (only when Phase 2 data is fresh)
	managedInterval, _ := time.ParseDuration(e.cfg.Managed.Schedule.PollInterval)
	if managedInterval == 0 {
		managedInterval = 10 * time.Minute
	}
	phase2Fresh := !e.phase2LastSuccess.IsZero() && time.Since(e.phase2LastSuccess) < 3*managedInterval
	if e.phases.Comparison != nil && e.cfg.Comparison.Enabled && phase2Fresh {
		if err := e.runPhase(ctx, cycleID, e.phases.Comparison); err != nil {
			e.logger.Errorw("Phase failed", "phase", "comparison", "cycle_id", cycleID, "error", err)
		}
		phasesRun = append(phasesRun, "comparison")
	}

	// Phase 4: OAS Generation
	if e.phases.SpecGen != nil && e.cfg.SpecGeneration.Enabled {
		if err := e.runPhase(ctx, cycleID, e.phases.SpecGen); err != nil {
			e.logger.Errorw("Phase failed", "phase", "specgen", "cycle_id", cycleID, "error", err)
		}
		phasesRun = append(phasesRun, "specgen")
	}

	// Phase 5: Service Catalog Push
	if e.phases.Catalog != nil && e.cfg.ServiceCatalog.Enabled && e.cfg.ServiceCatalog.AutoPush {
		if e.breakers["catalog"].ShouldAttempt() {
			if err := e.runPhase(ctx, cycleID, e.phases.Catalog); err != nil {
				e.breakers["catalog"].RecordFailure()
			} else {
				e.breakers["catalog"].RecordSuccess()
			}
			phasesRun = append(phasesRun, "catalog")
		}
	}

	// Track first full cycle completion (all 5 phases have run at least once)
	if !e.firstFullCycleComplete && !e.phase2LastRun.IsZero() {
		e.firstFullCycleComplete = true
	}

	// Daily cleanup
	if time.Since(e.cleanupLastRun) >= 24*time.Hour {
		e.cleanup.Run(ctx, cycleID)
		e.cleanupLastRun = time.Now()
	}

	// Catalog reconciliation (runs after first full cycle, then daily alongside cleanup)
	if e.phases.Reconciliation != nil && e.firstFullCycleComplete {
		if e.reconcileLastRun.IsZero() || time.Since(e.reconcileLastRun) >= 24*time.Hour {
			if err := e.runPhase(ctx, cycleID, e.phases.Reconciliation); err != nil {
				e.logger.Errorw("Reconciliation failed", "cycle_id", cycleID, "error", err)
			}
			e.reconcileLastRun = time.Now()
			phasesRun = append(phasesRun, "reconciliation")
		}
	}

	e.logger.Infow("Cycle completed",
		"cycle_id", cycleID,
		"trigger", trigger,
		"phases", phasesRun,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func (e *Engine) runPhase(ctx context.Context, cycleID string, phase Phase) error {
	start := time.Now()
	err := phase.Run(ctx, cycleID)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		e.logger.Errorw("Phase failed",
			"phase", phase.Name(),
			"cycle_id", cycleID,
			"error", err,
			"duration_ms", duration,
		)
		return err
	}

	e.logger.Infow("Phase completed",
		"phase", phase.Name(),
		"cycle_id", cycleID,
		"duration_ms", duration,
	)
	return nil
}

func (e *Engine) shouldRunPhase2() bool {
	if e.phase2LastRun.IsZero() {
		return true
	}

	interval, err := time.ParseDuration(e.cfg.Managed.Schedule.PollInterval)
	if err != nil {
		interval = 10 * time.Minute
	}
	return time.Since(e.phase2LastRun) >= interval
}
