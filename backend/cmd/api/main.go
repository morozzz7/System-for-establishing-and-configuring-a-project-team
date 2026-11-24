// Package main is the entrypoint for the teamopt backend API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/handler"
	"github.com/teamopt/backend/internal/mlclient"
	"github.com/teamopt/backend/internal/platform/config"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/service"
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
		workflow := service.NewWorkflowService(mlclient.New(cfg.MLServiceURL))
		(&handler.WorkflowHandler{Svc: workflow}).Register(r)
		if pool != nil {
			handler.NewTaxonomyHandler(service.NewTaxonomyService(repository.NewPgTaxonomyRepo(pool))).Register(r)
		} else {
			handler.NewTaxonomyHandler(nil).Register(r)
		}
		handler.NewReferenceHandler().Register(r)

		// CRUD routes (task 2.3) + capacity routes (task 2.4).
		if pool != nil {
			empRepo := repository.NewPgEmployeeRepo(pool)
			compRepo := repository.NewPgEmployeeCompetencyRepo(pool)
			prjRepo := repository.NewPgProjectRepo(pool)
			asgRepo := repository.NewPgAssignmentRepo(pool)
			absRepo := repository.NewPgAbsenceRepo(pool)
			audRepo := repository.NewPgAuditRepo(pool)
			capRepo := repository.NewPgCapacityRepo(pool)

			capSvc := service.NewCapacityService(empRepo, asgRepo, absRepo, capRepo)
			workflow.SetCandidateSource(service.NewPgCandidateSource(empRepo, compRepo, capRepo, asgRepo))

			empSvc := &service.EmployeeService{
				EmployeeRepo: empRepo,
				CompRepo:     compRepo,
				AuditRepo:    audRepo,
				AsgRepo:      asgRepo,
				AbsRepo:      absRepo,
				ProjectRepo:  prjRepo,
				CapacitySvc:  capSvc,
			}
			prjSvc := &service.ProjectService{ProjectRepo: prjRepo, AuditRepo: audRepo}
			asgSvc := &service.AssignmentService{
				AssignmentRepo: asgRepo,
				EmployeeRepo:   empRepo,
				AbsenceRepo:    absRepo,
				ProjectRepo:    prjRepo,
				AuditRepo:      audRepo,
				CapacitySvc:    capSvc,
			}
			absSvc := &service.AbsenceService{AbsenceRepo: absRepo, EmployeeRepo: empRepo, AuditRepo: audRepo}

			crudRouter := handler.NewRouter(empSvc, prjSvc, asgSvc, absSvc, capSvc)
			r.Mount("/", crudRouter)
			logger.Info("CRUD + capacity routes registered")
		} else {
			logger.Warn("CRUD routes not registered: no database pool")
		}
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

const (
	dependencyPostgres = "postgres"
	dependencyML       = "ml_service"
	dependencyTaxonomy = "taxonomy"

	stateUp   = "up"
	stateDown = "down"

	readinessTimeout = 2 * time.Second
)

var (
	// Briefs are stored before extraction and every run reads its candidate pool from
	// postgres, so a database outage disables the whole pipeline.
	postgresCapabilities = []string{
		"brief_extraction", "normalization", "optimization", "explanations", "recalculation",
	}
	// The ML service performs structured extraction and solves variants, so neither
	// capability is available when it is down.
	mlCapabilities = []string{"brief_extraction", "optimization"}
)

type healthStatus struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
	UptimeS int64  `json:"uptime_s"`
}

type dependencyState struct {
	Name        string   `json:"name"`
	State       string   `json:"state"`
	LatencyMS   *int64   `json:"latency_ms,omitempty"`
	ReasonCodes []string `json:"reason_codes,omitempty"`
}

type readinessReport struct {
	Ready                bool              `json:"ready"`
	Dependencies         []dependencyState `json:"dependencies"`
	DegradedCapabilities []string          `json:"degraded_capabilities"`
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthStatus{
		Status: "ok", Service: "core-api", Version: version,
		UptimeS: int64(time.Since(startTime).Seconds()),
	})
}

func handleReadyz(pool *pgxpool.Pool, mlURL string) http.HandlerFunc {
	return newReadinessHandler(postgresProbe(pool), mlProbe(mlURL))
}

// newReadinessHandler runs both remote probes under one deadline so readiness never
// takes longer than readinessTimeout even when both dependencies hang.
func newReadinessHandler(probePostgres, probeML func(context.Context) dependencyState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		pgCh := make(chan dependencyState, 1)
		mlCh := make(chan dependencyState, 1)
		go func() { pgCh <- probePostgres(ctx) }()
		go func() { mlCh <- probeML(ctx) }()
		postgres, ml := <-pgCh, <-mlCh

		report := readinessReport{
			Dependencies:         []dependencyState{postgres, ml, {Name: dependencyTaxonomy, State: stateUp}},
			DegradedCapabilities: make([]string, 0, len(postgresCapabilities)+len(mlCapabilities)),
			Ready:                true,
		}
		for _, dep := range []struct {
			state        dependencyState
			capabilities []string
		}{
			{postgres, postgresCapabilities}, {ml, mlCapabilities},
		} {
			if dep.state.State == stateUp {
				continue
			}
			report.Ready = false
			report.DegradedCapabilities = append(report.DegradedCapabilities, dep.capabilities...)
		}
		status := http.StatusOK
		if !report.Ready {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, report)
	}
}

func postgresProbe(pool *pgxpool.Pool) func(context.Context) dependencyState {
	return func(ctx context.Context) dependencyState {
		state := dependencyState{Name: dependencyPostgres, State: stateDown}
		if pool == nil {
			return state
		}
		start := time.Now()
		err := pool.Ping(ctx)
		ms := time.Since(start).Milliseconds()
		state.LatencyMS = &ms
		if err != nil {
			slog.WarnContext(ctx, "postgres readiness ping failed", "error", err)
			return state
		}
		state.State = stateUp
		return state
	}
}

func mlProbe(mlURL string) func(context.Context) dependencyState {
	return func(ctx context.Context) dependencyState {
		state := dependencyState{Name: dependencyML, State: stateDown, ReasonCodes: []string{"ML_UNAVAILABLE"}}
		if mlURL == "" {
			return state
		}
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			strings.TrimRight(mlURL, "/")+"/healthz", nil)
		if err != nil {
			return state
		}
		resp, err := http.DefaultClient.Do(req)
		ms := time.Since(start).Milliseconds()
		state.LatencyMS = &ms
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				state.ReasonCodes = []string{"ML_TIMEOUT"}
			}
			slog.WarnContext(ctx, "ml readiness probe failed", "error", err)
			return state
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			state.State = stateUp
			state.ReasonCodes = nil
		}
		return state
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
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
