package engine

import (
	"math"
	"sync"
	"time"

	"github.com/wso2/adc/internal/logging"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half-open"
)

// CircuitBreaker implements a per-phase circuit breaker with exponential backoff.
type CircuitBreaker struct {
	mu              sync.Mutex
	Name            string
	State           CircuitState
	FailureCount    int
	LastFailure     time.Time
	BackoffDuration time.Duration
	MaxBackoff      time.Duration
	Threshold       int
	logger          *logging.Logger
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(name string, threshold int, maxBackoff time.Duration, logger *logging.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		Name:       name,
		State:      CircuitClosed,
		Threshold:  threshold,
		MaxBackoff: maxBackoff,
		logger:     logger,
	}
}

// ShouldAttempt returns true if the phase should be attempted.
func (cb *CircuitBreaker) ShouldAttempt() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.State == CircuitClosed {
		return true
	}
	if cb.State == CircuitOpen && time.Since(cb.LastFailure) > cb.BackoffDuration {
		cb.State = CircuitHalfOpen
		cb.logger.Infow("Circuit breaker HALF-OPEN — testing recovery", "breaker", cb.Name)
		return true
	}
	return false
}

// RecordSuccess records a successful execution.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.State != CircuitClosed {
		cb.logger.Infow("Circuit breaker CLOSED — recovered",
			"breaker", cb.Name,
			"prior_failures", cb.FailureCount,
		)
	}
	cb.FailureCount = 0
	cb.State = CircuitClosed
	cb.BackoffDuration = 0
}

// RecordFailure records a failed execution.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.FailureCount++
	cb.LastFailure = time.Now()

	if cb.FailureCount >= cb.Threshold {
		cb.State = CircuitOpen
		// Exponential backoff: 1m * 3^(failures-threshold), capped at MaxBackoff.
		// Cap exponent at 20: 3^20 ≈ 3.5B, safely within float64 range before math.Min clamps to MaxBackoff.
		exponent := math.Min(float64(cb.FailureCount-cb.Threshold), 20)
		backoff := time.Duration(math.Min(
			float64(time.Minute)*math.Pow(3, exponent),
			float64(cb.MaxBackoff),
		))
		cb.BackoffDuration = backoff
		cb.logger.Warnw("Circuit breaker OPEN — backing off",
			"breaker", cb.Name,
			"failures", cb.FailureCount,
			"backoff", cb.BackoffDuration,
		)
	}
}
