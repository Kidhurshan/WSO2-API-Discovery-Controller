// Package health provides /healthz and /readyz HTTP endpoints.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/wso2/adc/internal/logging"
	"github.com/wso2/adc/internal/store"
)

// Server serves health check endpoints.
type Server struct {
	db     *store.DB
	port   int
	logger *logging.Logger
	server *http.Server
}

// New creates a new health server.
func New(db *store.DB, port int, logger *logging.Logger) *Server {
	return &Server{
		db:     db,
		port:   port,
		logger: logger,
	}
}

// Start starts the health server in the background.
func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleLiveness)
	mux.HandleFunc("/readyz", s.handleReadiness)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		s.logger.Infow("Health server started", "port", s.port)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorw("Health server failed", "error", err)
		}
	}()
}

// Stop gracefully shuts down the health server.
func (s *Server) Stop() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

// handleLiveness returns 200 if the process is alive.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleReadiness returns 200 if PostgreSQL is reachable.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	if err := s.db.Healthy(ctx); err != nil {
		checks["postgresql"] = err.Error()
		healthy = false
	} else {
		checks["postgresql"] = "ok"
	}

	w.Header().Set("Content-Type", "application/json")
	if healthy {
		w.WriteHeader(http.StatusOK)
		checks["status"] = "ok"
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		checks["status"] = "unavailable"
	}
	json.NewEncoder(w).Encode(checks)
}
