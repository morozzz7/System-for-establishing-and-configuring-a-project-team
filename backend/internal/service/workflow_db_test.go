package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/explain"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/mlclient"
	"github.com/teamopt/backend/internal/platform/actor"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/runinput"
)

func setupDBWorkflowService(t *testing.T) *WorkflowService {
	t.Helper()
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "teamopt_workflow_service_test",
			"POSTGRES_USER":     "teamopt",
			"POSTGRES_PASSWORD": "teamopt_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(30 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req, Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)
	dsn := fmt.Sprintf("postgres://teamopt:teamopt_test@%s:%s/teamopt_workflow_service_test?sslmode=disable",
		host, port.Port())

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", "db", "migrations"))
	require.NoError(t, err)
	require.NoError(t, goose.Up(db, dir))
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	svc := NewWorkflowService(mlclient.NewWithHTTPClient("http://fixture", &http.Client{
		Transport: &recalcFakeML{extract: recalcExtract, optimize: recalcOptimize},
	}))
	svc.SetStores(WorkflowStores{
		Briefs:       repository.NewPgBriefRepo(pool),
		Runs:         repository.NewPgRunRepo(pool),
		Snapshots:    repository.NewPgSnapshotRepo(pool),
		Variants:     repository.NewPgVariantRepo(pool),
		Decisions:    repository.NewPgDecisionRepo(pool),
		Explanations: repository.NewPgExplanationRepo(pool),
		Overrides:    repository.NewPgConstraintOverrideRepo(pool),
		Audit:        repository.NewPgAuditRepo(pool),
		ComputedRuns: repository.NewPgComputedRunRepo(pool),
	})
	return svc
}

// TestWorkflowPersistenceRoundTrip proves that all read paths are served from
// PostgreSQL after the in-memory maps are discarded (a restart simulation).
// TestBriefRequirementsSurviveRestart closes the gap where only the brief identity row was
// persisted: after a process restart a brand-new run must still be creatable from the brief.
func TestBriefRequirementsSurviveRestart(t *testing.T) {
	svc := setupDBWorkflowService(t)
	ctx := context.Background()

	brief, err := svc.CreateBrief(ctx, BriefInput{
		Title: "Restart", SourceType: "text", Language: "ru", Text: "Go инженер",
	})
	require.NoError(t, err)
	require.NotEmpty(t, brief.Requirements)
	_, ok := svc.ConfirmBrief(brief.BriefID)
	require.True(t, ok)

	svc.mu.Lock()
	svc.briefs = map[string]*BriefRecord{}
	svc.mu.Unlock()

	restored, ok := svc.GetBrief(brief.BriefID)
	require.True(t, ok, "brief must be rehydrated from the database")
	assert.Equal(t, "confirmed", restored.Status)
	assert.Equal(t, brief.Text, restored.Text)
	require.Len(t, restored.Requirements, len(brief.Requirements))
	assert.Equal(t, brief.Requirements[0]["requirement_id"], restored.Requirements[0]["requirement_id"])

	listed := svc.ListBriefs()
	found := false
	for _, item := range listed {
		if item.BriefID == brief.BriefID {
			found = true
		}
	}
	assert.True(t, found, "rehydrated brief must appear in the list")

	run, err := svc.CreateRun(ctx, brief.BriefID, map[string]any{
		"horizon": map[string]any{"weeks": []any{"2026-W10", "2026-W11"}},
	})
	require.NoError(t, err, "a new run must be possible from a persisted brief after restart")
	assert.Equal(t, brief.BriefID, run["brief_id"])
}

func TestWorkflowPersistenceRoundTrip(t *testing.T) {
	svc := setupDBWorkflowService(t)
	ctx := actor.WithContext(context.Background(), "emp_ab01")

	brief, err := svc.CreateBrief(ctx, BriefInput{
		Title: "Synthetic", SourceType: "text", Language: "ru", Text: "Go инженер",
	})
	require.NoError(t, err)
	_, ok := svc.ConfirmBrief(brief.BriefID)
	require.True(t, ok)
	run, err := svc.CreateRun(ctx, brief.BriefID, map[string]any{
		"horizon": map[string]any{"weeks": []any{"2026-W10", "2026-W11"}},
	})
	require.NoError(t, err)
	runID, _ := run["run_id"].(string)
	require.NotEmpty(t, runID)

	svc.mu.Lock()
	svc.briefs = map[string]*BriefRecord{}
	svc.runs = map[string]map[string]any{}
	svc.snapshots = map[string]*runinput.Snapshot{}
	svc.variants = map[string]map[string]map[string]any{}
	svc.details = map[string]map[string]explain.VariantDetail{}
	svc.varresults = map[string]map[string]metrics.VariantResult{}
	svc.decisions = map[string]map[string]any{}
	svc.audits = map[string][]domain.AuditEntry{}
	svc.mu.Unlock()

	storedRun, ok := svc.GetRun(runID)
	require.True(t, ok, "run must be served from the database")
	assert.Equal(t, runID, storedRun["run_id"])
	assert.Equal(t, brief.BriefID, storedRun["brief_id"])

	storedVariant, ok := svc.GetVariant(runID, "var_alt1")
	require.True(t, ok, "variant must be served from the database")
	assert.Equal(t, runID, storedVariant["run_id"])

	explanation, ok := svc.GetExplanation(runID, "var_alt1", "ru")
	require.True(t, ok, "explanation must be served from the database")
	assert.Equal(t, "ru", explanation["locale"])

	inputs, ok := svc.GetRunInputs(runID)
	require.True(t, ok, "inputs must be rebuilt from the persisted snapshot after a restart")
	assert.Equal(t, runID, inputs["run_id"])

	variantPage, ok := svc.ListVariants(ctx, runID, 10, 0)
	require.True(t, ok)
	assert.NotEmpty(t, variantPage["items"])

	auditPage, ok := svc.ListAudit(ctx, runID, 10, 0)
	require.True(t, ok)
	auditItems := auditPage["items"].([]any)
	assert.NotEmpty(t, auditItems)

	resp, err := svc.RecordDecision(ctx, runID, DecisionInput{
		VariantID: "var_alt1", DecidedBy: "emp_zz99",
	})
	require.NoError(t, err)
	assert.Equal(t, "emp_ab01", resp.DecidedBy)
	assert.NotEmpty(t, resp.AuditEventID)
	_, err = svc.RecordDecision(ctx, runID, DecisionInput{VariantID: "var_alt1"})
	assert.ErrorIs(t, err, ErrDecisionExists, "idempotency must be enforced by the database")

	decision, ok := svc.GetDecision(runID)
	require.True(t, ok, "decision must be served from the database")
	assert.Equal(t, "emp_ab01", decision["decided_by"])
	assert.Equal(t, resp.AuditEventID, decision["audit_event_id"])

	recalc, err := svc.Recalculate(ctx, runID, RecalculateRequest{
		BaseVariantID: "var_alt1",
		Overrides:     []ConstraintOverride{{Type: "force_include", EmployeeID: "emp_demo0001"}},
	})
	require.NoError(t, err, "parent snapshot must be readable from the database")
	childID, _ := recalc["run_id"].(string)
	assert.NotEmpty(t, childID)
	assert.Equal(t, runID, recalc["parent_run_id"])
	child, ok := svc.GetRun(childID)
	require.True(t, ok)
	assert.Equal(t, runID, child["parent_run_id"])
}
