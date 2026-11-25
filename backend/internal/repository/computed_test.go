package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/repository"
)

// TestCreateComputedRollsBackOnFailure injects a variant-id conflict at the third write of
// the aggregate. The run and its snapshot must not survive the failure: an orphan run is
// exactly the state that made a restart unsafe.
func TestCreateComputedRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	pool := setupWorkflowRepos(t)

	briefID, blockerRunID, runID := "brf_atomic01", "run_blocker1", "run_atomic01"
	require.NoError(t, repository.NewPgBriefRepo(pool).Create(ctx, domain.Brief{
		BriefID: briefID, Title: "Atomic", SourceType: "text", Language: "ru",
		Status: "confirmed", Text: "Go engineer", TextSHA256: strings.Repeat("a", 64),
	}))

	newRun := func(id string) domain.Run {
		return domain.Run{
			RunID: id, BriefID: briefID, Status: "partial", Preset: "balanced",
			K: 1, Seed: 42, TimeLimitMS: 10000,
			StartWeek: "2026-W10", EndWeek: "2026-W11",
		}
	}
	runRepo := repository.NewPgRunRepo(pool)
	require.NoError(t, runRepo.Create(ctx, newRun(blockerRunID)))

	conflictingVariantID := "var_conflict1"
	require.NoError(t, repository.NewPgVariantRepo(pool).CreateBatch(ctx, []domain.Variant{{
		VariantID: conflictingVariantID, RunID: blockerRunID, Label: "blocker", GatePass: true,
	}}))

	snapshot := domain.Snapshot{
		SnapshotID: "snp_atomic1", RunID: runID, SnapshotSHA256: strings.Repeat("b", 64),
		HorizonStart: "2026-W10", HorizonEnd: "2026-W11",
	}
	variants := []domain.Variant{{
		VariantID: conflictingVariantID, RunID: runID, Label: "conflicting", GatePass: true,
	}}

	err := repository.NewPgComputedRunRepo(pool).CreateComputed(ctx, repository.ComputedRun{
		Run: newRun(runID), Snapshot: &snapshot, Variants: variants,
	})
	require.Error(t, err, "the variant conflict must fail the aggregate write")

	var runCount, snapshotCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM team_run WHERE run_id = $1", runID).Scan(&runCount))
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM run_snapshot WHERE run_id = $1", runID).Scan(&snapshotCount))
	require.Zero(t, runCount, "a failed aggregate must not leave an orphan run")
	require.Zero(t, snapshotCount, "a failed aggregate must not leave a snapshot")

	require.NoError(t, repository.NewPgComputedRunRepo(pool).CreateComputed(ctx, repository.ComputedRun{
		Run: newRun(runID), Snapshot: &snapshot,
		Variants: []domain.Variant{{VariantID: "var_atomic1", RunID: runID, Label: "ok", GatePass: true}},
	}))
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM team_run WHERE run_id = $1", runID).Scan(&runCount))
	require.Equal(t, 1, runCount, "a clean aggregate must still commit")
}
