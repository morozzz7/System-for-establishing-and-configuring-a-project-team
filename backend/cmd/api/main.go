// Package main is the entrypoint for the teamopt backend API.
// In phase 1.3 it serves only /api/v1/healthz and /api/v1/readyz.
// Business endpoints are added in phase 2.x.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/platform/config"
)

const version = "1.0.0"

var startTime = time.Now()

func main() {
	// healthcheck sub-command: used by Docker HEALTHCHECK in distroless (no shell).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}

	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Database pool (optional: if DATABASE_URL is empty, readyz reports postgres=unknown).
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		p, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			logger.Error("failed to create db pool", "error", err)
			os.Exit(1)
		}
		defer p.Close()
		pool = p

		// Eager ping to fail fast on bad DSN.
		pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			logger.Warn("initial db ping failed (will retry on readyz)", "error", err)
		} else {
			logger.Info("database connected")
		}
	} else {
		logger.Warn("DATABASE_URL is empty; postgres dependency will be reported as unknown")
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", handleHealthz)
		r.Get("/readyz", handleReadyz(pool, cfg.MLServiceURL))
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("backend listening", "port", cfg.Port, "version", version)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
}

// handleHealthz returns {"status":"ok"} per acceptance criteria for 1.3.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
	})
}

// handleReadyz returns the stub readiness shape.
// Phase 2.x will align this with contracts/openapi.yaml ReadinessReport.
func handleReadyz(pool *pgxpool.Pool, mlURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deps := map[string]string{
			"postgres": probePostgres(r.Context(), pool),
			"ml":       probeML(r.Context(), mlURL),
		}

		status := "ok"
		for _, v := range deps {
			if v == "error" {
				status = "degraded"
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       status,
			"dependencies": deps,
		})
	}
}

func probePostgres(ctx context.Context, pool *pgxpool.Pool) string {
	if pool == nil {
		return "unknown"
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		slog.Warn("postgres ping failed", "error", err)
		return "error"
	}
	return "ok"
}

func probeML(ctx context.Context, mlURL string) string {
	if mlURL == "" {
		return "unknown"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, mlURL+"/healthz", nil)
	if err != nil {
		return "unknown"
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// ML not reachable is reported as "unknown" in the stub phase,
		// not "error", because the ML capabilities are not yet required.
		return "unknown"
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return "ok"
	}
	return "unknown"
}

// runHealthcheck is used by Docker HEALTHCHECK (exec form, works on distroless/alpine).
// The server always listens on 8080 inside the container; host port mapping is separate.
func runHealthcheck() {
	resp, err := http.Get("http://localhost:8080/api/v1/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: unexpected status %d\n", resp.StatusCode)
		os.Exit(1)
	}
	fmt.Println("healthy")
}
