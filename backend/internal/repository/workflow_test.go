package repository_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/teamopt/backend/internal/repository"
)

func setupWorkflowRepos(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "teamopt_workflow_test",
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
	dsn := fmt.Sprintf("postgres://teamopt:teamopt_test@%s:%s/teamopt_workflow_test?sslmode=disable",
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
	return pool
}

func TestWorkflowRepositoriesPersistChain(t *testing.T) {
	ctx := context.Background()
	pool := setupWorkflowRepos(t)

	briefRepo := repository.NewPgBriefRepo(pool)
	runRepo := repository.NewPgRunRepo(pool)
	snapshotRepo := repository.NewPgSnapshotRepo(pool)
	variantRepo := repository.NewPgVariantRepo(pool)
	explanationRepo := repository.NewPgExplanationRepo(pool)
	overrideRepo := repository.NewPgConstraintOverrideRepo(pool)
	decisionRepo := repository.NewPgDecisionRepo(pool)
	auditRepo := repository.NewPgAuditRepo(pool)

	created := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	require.NoError(t, briefRepo.Create(ctx, domain.Brief{
		BriefID: "brf_a1b2c3d4", Title: "Synthetic brief", SourceType: "text", Language: "ru",
		Status: "confirmed", Text: "Go engineer needed", TextSHA256: strings64Repeat('0'),
		RequestedStartWeek: "2026-W10", DeadlineWeek: "2026-W11", CreatedAt: created,
	}))
	brief, err := briefRepo.Get(ctx, "brf_a1b2c3d4")
	require.NoError(t, err)
	assert.Equal(t, "confirmed", brief.Status)
	require.NoError(t, briefRepo.SetStatus(ctx, brief.BriefID, "confirmed"))

	data := json.RawMessage(`{"run_id":"run_a1b2c3d4","brief_id":"brf_a1b2c3d4"}`)
	require.NoError(t, runRepo.Create(ctx, domain.Run{
		RunID: "run_a1b2c3d4", BriefID: brief.BriefID, Status: "succeeded", Preset: "balanced",
		K: 2, Seed: 42, TimeLimitMS: 10000, StartWeek: "2026-W10", EndWeek: "2026-W11",
		Weights: json.RawMessage(`{"M1":1}`), Constraints: json.RawMessage(`{}`),
		MetricsVersion: "m1-m12@2026-10", ContractVersion: "v1",
		CreatedAt: created, Payload: json.RawMessage(`{"run_id":"run_a1b2c3d4","status":"succeeded"}`),
	}))
	run, err := runRepo.Get(ctx, "run_a1b2c3d4")
	require.NoError(t, err)
	assert.JSONEq(t, `{"run_id":"run_a1b2c3d4","status":"succeeded"}`, string(run.Payload))
	require.NoError(t, runRepo.Finish(ctx, run.RunID, "partial", created.Add(time.Hour),
		json.RawMessage(`{"run_id":"run_a1b2c3d4","status":"partial"}`)))
	runs, err := runRepo.ListByBrief(ctx, brief.BriefID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "partial", runs[0].Status)

	require.NoError(t, snapshotRepo.Create(ctx, domain.Snapshot{
		SnapshotID: "snp_a1b2c3d4", RunID: run.RunID,
		SnapshotSHA256: strings64Repeat('a'), HorizonStart: "2026-W10", HorizonEnd: "2026-W11",
		Weights: json.RawMessage(`{"M1":1}`), Constraints: json.RawMessage(`{}`),
		RequirementCount: 1, CandidateCount: 1, CapacityRows: 2, Data: data,
		Candidates: []domain.SnapshotCandidate{{
			EmployeeID: "emp_a1b2c3d4", Grade: "M", CostCoefficient: 1.4,
			CompetencyIDs: []string{"lang_go"}, CapacityDigest: strings64Repeat('b'), CapacityWeeks: 2,
		}},
		CreatedAt: created,
	}))
	storedData, err := snapshotRepo.GetData(ctx, run.RunID)
	require.NoError(t, err)
	assert.JSONEq(t, string(data), string(storedData))

	require.NoError(t, variantRepo.CreateBatch(ctx, []domain.Variant{{
		VariantID: "var_a1b2", RunID: run.RunID, Label: "Synthetic", GatePass: true,
		Score: 0.8, TeamSize: 1, CostUnits: 100,
		Duration: json.RawMessage(`{"p50_weeks":2}`), MetricValues: json.RawMessage(`{"M1":1}`),
		MetricDetails: json.RawMessage(`{}`), ReasonCodes: []string{"FEASIBLE"},
		Payload: json.RawMessage(`{"variant_id":"var_a1b2","score":0.8}`),
		Members: []domain.VariantMember{{
			EmployeeID: "emp_a1b2c3d4", RoleID: "backend_engineer", Grade: "M",
			AllocatedHours: 80, AssignmentShare: 1, CompetenceFit: 1,
			ReasonCodes: []string{"INCLUDE_REQUIRED_ROLE"},
		}},
		WeeklyAssignments: []domain.VariantWeeklyAssignment{{
			EmployeeID: "emp_a1b2c3d4", RequirementID: "req_go01",
			Week: "2026-W10", Hours: 40, Share: 1,
		}},
		Deficits: []domain.VariantDeficit{{
			CompetencyID: "lang_go", CompetencyNameRU: "Go",
			RequirementIDs: []string{"req_go01"}, PersonDays: 3, Weeks: 1,
			RecommendedActions: []domain.VariantRecommendedAction{{
				Action: "contractor", ReasonCodes: []string{"DEFICIT_EXTERNAL_CONTRACTOR"},
			}},
		}},
		ActiveConstraints: []domain.VariantActiveConstraint{{
			Type: "force_include", Value: json.RawMessage(`["emp_a1b2c3d4"]`),
			Source: "user", ReasonCodes: []string{"CONSTRAINT_ACTIVE_FORCE_INCLUDE"},
		}},
		ExcludedCandidates: []domain.VariantExcludedCandidate{{
			EmployeeID: "emp_b2c3d4e5", ReasonCodes: []string{"G5"}, Detail: json.RawMessage(`{}`),
		}},
	}}))
	variant, err := variantRepo.Get(ctx, run.RunID, "var_a1b2")
	require.NoError(t, err)
	require.Len(t, variant.Members, 1)
	assert.Equal(t, 80.0, variant.Members[0].AllocatedHours)
	require.Len(t, variant.WeeklyAssignments, 1)
	assert.Equal(t, 40.0, variant.WeeklyAssignments[0].Hours)
	require.Len(t, variant.Deficits, 1)
	require.Len(t, variant.Deficits[0].RecommendedActions, 1)
	require.Len(t, variant.ActiveConstraints, 1)
	require.Len(t, variant.ExcludedCandidates, 1)
	variants, err := variantRepo.ListByRun(ctx, run.RunID)
	require.NoError(t, err)
	require.Len(t, variants, 1)
	rosters, err := variantRepo.Roster(ctx, run.RunID)
	require.NoError(t, err)
	assert.Equal(t, []string{"emp_a1b2c3d4"}, rosters["var_a1b2"])

	for i, locale := range []string{"ru", "en"} {
		explanationID := []string{"exp_a1b2c3d4", "exp_b2c3d4e5"}[i]
		require.NoError(t, explanationRepo.Upsert(ctx, domain.Explanation{
			ExplanationID: explanationID, VariantID: "var_a1b2", Locale: locale,
			Headline: "Synthetic explanation", Tradeoff: json.RawMessage(`{"text":"t"}`),
			Included: json.RawMessage(`[]`), Excluded: json.RawMessage(`[]`),
			Deficits: json.RawMessage(`[]`), Counterfactuals: json.RawMessage(`[]`),
			Payload: json.RawMessage(`{"locale":"` + locale + `"}`), CreatedAt: created,
		}))
	}
	explanation, err := explanationRepo.Get(ctx, run.RunID, "var_a1b2", "ru")
	require.NoError(t, err)
	assert.JSONEq(t, `{"locale":"ru"}`, string(explanation.Payload))

	require.NoError(t, overrideRepo.CreateBatch(ctx, run.RunID, []domain.ConstraintOverride{{
		RunID: run.RunID, Type: "force_include", EmployeeID: "emp_a1b2c3d4",
	}}))
	require.NoError(t, decisionRepo.Create(ctx, domain.Decision{
		RunID: run.RunID, VariantID: "var_a1b2", DecidedAt: created.Add(2 * time.Hour),
		DecidedBy: actorSystem, EditsApplied: json.RawMessage(`[{"type":"force_include"}]`),
		AuditEventID: "aud_b2c3d4e5",
	}))
	decision, err := decisionRepo.Get(ctx, run.RunID)
	require.NoError(t, err)
	assert.Equal(t, actorSystem, decision.DecidedBy)
	assert.ErrorIs(t, decisionRepo.Create(ctx, domain.Decision{
		RunID: run.RunID, VariantID: "var_a1b2", DecidedBy: actorSystem,
		AuditEventID: "aud_c3d4e5f6",
	}), repository.ErrDecisionExists)

	for _, entry := range []domain.AuditEntry{{
		EventID: "aud_a1b2c3d4", At: created, Actor: actorSystem, Action: "run_started",
		EntityType: "run", EntityID: run.RunID, RequestID: "req_a1b2c3", Summary: "started",
	}, {
		EventID: "aud_b2c3d4e5", At: created.Add(time.Hour), Actor: actorSystem,
		Action: "run_finished", EntityType: "run", EntityID: run.RunID,
		RequestID: "req_b2c3d4", Summary: "finished",
	}} {
		require.NoError(t, auditRepo.Create(ctx, entry))
	}
	entries, total, err := auditRepo.ListByEntity(ctx, domain.AuditFilter{
		EntityType: "run", EntityID: run.RunID, Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, entries, 2)
	assert.Equal(t, "run_started", entries[0].Action)
	assert.Equal(t, "run_finished", entries[1].Action)
}

const actorSystem = "system"

func strings64Repeat(b byte) string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = b
	}
	return string(out)
}

func TestDecisionRepoMissingRun(t *testing.T) {
	ctx := context.Background()
	pool := setupWorkflowRepos(t)
	repo := repository.NewPgDecisionRepo(pool)
	_, err := repo.Get(ctx, "run_zzzz")
	assert.True(t, errors.Is(err, repository.ErrNotFound))
}
