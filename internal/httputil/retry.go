// Package httputil provides shared HTTP utilities for ADC clients.
package httputil

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

// RetryConfig controls retry behavior for HTTP requests.
type RetryConfig struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
	Multiplier  float64
}

// DefaultRetryConfig returns a sensible default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		InitialWait: 1 * time.Second,
		MaxWait:     10 * time.Second,
		Multiplier:  2.0,
	}
}

// DoWithRetry executes an HTTP request with retry on transient errors.
// The reqFactory is called for each attempt to create a fresh request
// (request bodies are consumed on read, and auth headers may need refreshing).
func DoWithRetry(ctx context.Context, client *http.Client, reqFactory func() (*http.Request, error), cfg RetryConfig) (*http.Response, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	backoff := cfg.InitialWait

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		req, err := reqFactory()
		if err != nil {
			return nil, fmt.Errorf("create request (attempt %d): %w", attempt, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < cfg.MaxAttempts {
				if waitErr := sleepWithJitter(ctx, backoff); waitErr != nil {
					return nil, fmt.Errorf("interrupted during retry backoff: %w", waitErr)
				}
				backoff = nextBackoff(backoff, cfg.Multiplier, cfg.MaxWait)
				continue
			}
			return nil, fmt.Errorf("request failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
		}

		if isRetryableStatus(resp.StatusCode) && attempt < cfg.MaxAttempts {
			// Only retry idempotent methods (GET, HEAD, PUT, DELETE, OPTIONS).
			// Non-idempotent methods like POST could create duplicate resources.
			if !isIdempotentMethod(req.Method) {
				return resp, nil
			}
			// Drain and close the body before retrying.
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)

			if waitErr := sleepWithJitter(ctx, backoff); waitErr != nil {
				return nil, fmt.Errorf("interrupted during retry backoff: %w", waitErr)
			}
			backoff = nextBackoff(backoff, cfg.Multiplier, cfg.MaxWait)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// isIdempotentMethod returns true for HTTP methods that are safe to retry.
func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

// isRetryableStatus returns true for transient HTTP status codes.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,     // 429
		http.StatusBadGateway,           // 502
		http.StatusServiceUnavailable,   // 503
		http.StatusGatewayTimeout:       // 504
		return true
	}
	return false
}

// sleepWithJitter waits for the given duration plus 0-25% random jitter,
// respecting context cancellation.
func sleepWithJitter(ctx context.Context, d time.Duration) error {
	jitter := time.Duration(rand.Int63n(int64(d) / 4))
	select {
	case <-time.After(d + jitter):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nextBackoff calculates the next backoff duration.
func nextBackoff(current time.Duration, multiplier float64, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > max {
		return max
	}
	return next
}
