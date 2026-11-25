package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/domain"
)

// sqlExecutor is implemented by both *pgxpool.Pool and pgx.Tx, so one set of INSERT
// statements can run standalone or inside the computed-run transaction.
type sqlExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ComputedRun is the complete run payload written by one transaction. A nil Snapshot
// means the (test-only) store is not wired for snapshots.
type ComputedRun struct {
	Run          domain.Run
	Snapshot     *domain.Snapshot
	Variants     []domain.Variant
	Explanations []domain.Explanation
}

// ComputedRunRepo writes a verified run and all of its children atomically. A failure at
// any stage rolls back the run row, so a restart can never expose an orphan run.
type ComputedRunRepo interface {
	CreateComputed(ctx context.Context, input ComputedRun) error
}

type PgComputedRunRepo struct{ Pool *pgxpool.Pool }

func NewPgComputedRunRepo(pool *pgxpool.Pool) *PgComputedRunRepo {
	return &PgComputedRunRepo{Pool: pool}
}

func (r *PgComputedRunRepo) CreateComputed(ctx context.Context, input ComputedRun) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin computed run tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertRun(ctx, tx, input.Run); err != nil {
		return err
	}
	if input.Snapshot != nil {
		if err := insertSnapshot(ctx, tx, *input.Snapshot); err != nil {
			return err
		}
	}
	if err := insertVariants(ctx, tx, input.Variants); err != nil {
		return err
	}
	for _, explanation := range input.Explanations {
		if err := upsertExplanation(ctx, tx, explanation); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertRun(ctx context.Context, exec sqlExecutor, run domain.Run) error {
	created := run.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if run.MetricsVersion == "" {
		run.MetricsVersion = "m1-m12@2026-10"
	}
	if run.ContractVersion == "" {
		run.ContractVersion = "v1"
	}
	_, err := exec.Exec(ctx,
		`INSERT INTO team_run (run_id, brief_id, parent_run_id, status, preset, k, seed,
		   time_limit_ms, start_week, end_week, weights, constraints, candidate_filter,
		   solver_status, solve_time_ms, ml_status, ml_attempts, validator_rejected,
		   fallback_used, fallback_reason, metrics_version, contract_version, reason_codes,
		   created_at, finished_at, payload_jsonb)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
		         $21,$22,$23,$24,$25,$26)`,
		run.RunID, run.BriefID, nullString(run.ParentRunID), run.Status, run.Preset, run.K,
		run.Seed, run.TimeLimitMS, run.StartWeek, run.EndWeek, jsonbOrEmpty(run.Weights),
		jsonbOrEmpty(run.Constraints), jsonbOrEmpty(run.CandidateFilter), nullString(run.SolverStatus),
		nullInt(run.SolveTimeMS), nullString(run.MLStatus), nullInt(run.MLAttempts),
		run.ValidatorRejected, run.FallbackUsed, nullString(run.FallbackReason),
		run.MetricsVersion, run.ContractVersion, stringsOrEmpty(run.ReasonCodes), created,
		nullTime(run.FinishedAt), jsonbOrEmpty(run.Payload),
	)
	if err != nil {
		if IsDuplicate(err) {
			return fmt.Errorf("%w: run %s", ErrConflict, run.RunID)
		}
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func insertSnapshot(ctx context.Context, exec sqlExecutor, s domain.Snapshot) error {
	created := s.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := exec.Exec(ctx,
		`INSERT INTO run_snapshot (snapshot_id, run_id, snapshot_sha256, horizon_start, horizon_end,
		   weights, constraints, requirement_count, candidate_count, capacity_rows, data_jsonb, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.SnapshotID, s.RunID, s.SnapshotSHA256, s.HorizonStart, s.HorizonEnd,
		jsonbOrEmpty(s.Weights), jsonbOrEmpty(s.Constraints), s.RequirementCount,
		s.CandidateCount, s.CapacityRows, jsonbOrEmpty(s.Data), created,
	)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	for _, c := range s.Candidates {
		_, err := exec.Exec(ctx,
			`INSERT INTO run_snapshot_candidate (snapshot_id, employee_id, grade, cost_coefficient,
			   competency_ids, capacity_digest, capacity_weeks)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			s.SnapshotID, c.EmployeeID, c.Grade, c.CostCoefficient, stringsOrEmpty(c.CompetencyIDs),
			c.CapacityDigest, c.CapacityWeeks,
		)
		if err != nil {
			return fmt.Errorf("insert snapshot candidate: %w", err)
		}
	}
	return nil
}

func insertVariants(ctx context.Context, exec sqlExecutor, variants []domain.Variant) error {
	for _, v := range variants {
		label := v.Label
		if len(label) > 120 {
			label = label[:120]
		}
		if label == "" {
			label = v.VariantID
		}
		_, err := exec.Exec(ctx,
			`INSERT INTO team_variant (variant_id, run_id, label, gate_pass, gate_g1, gate_g2,
			   gate_g3, gate_g4, gate_g5, max_overload_pct, score, team_size, cost_units,
			   duration, metric_values, metric_details, reason_codes, payload_jsonb)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			v.VariantID, v.RunID, label, v.GatePass, v.GateG1, v.GateG2, v.GateG3, v.GateG4,
			v.GateG5, v.MaxOverloadPct, v.Score, v.TeamSize, v.CostUnits,
			jsonbOrEmpty(v.Duration), jsonbOrEmpty(v.MetricValues), jsonbOrEmpty(v.MetricDetails),
			stringsOrEmpty(v.ReasonCodes), jsonbOrEmpty(v.Payload),
		)
		if err != nil {
			return fmt.Errorf("insert variant %s: %w", v.VariantID, err)
		}
		for _, m := range v.Members {
			minutes := int(m.AllocatedHours * 60)
			if minutes < 0 {
				minutes = 0
			}
			_, err := exec.Exec(ctx,
				`INSERT INTO variant_member (variant_id, employee_id, role_id, grade,
				   allocated_hours_minutes, assignment_share, competence_fit, reason_codes)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				v.VariantID, m.EmployeeID, m.RoleID, m.Grade, minutes,
				m.AssignmentShare, m.CompetenceFit, stringsOrEmpty(m.ReasonCodes),
			)
			if err != nil {
				return fmt.Errorf("insert variant member: %w", err)
			}
		}
		for _, a := range v.WeeklyAssignments {
			minutes := int(a.Hours * 60)
			if minutes < 0 {
				minutes = 0
			}
			_, err := exec.Exec(ctx,
				`INSERT INTO variant_weekly_assignment (variant_id, employee_id, requirement_id,
				   week, hours_minutes, share)
				 VALUES ($1,$2,$3,$4,$5,$6)`,
				v.VariantID, a.EmployeeID, a.RequirementID, a.Week, minutes, a.Share,
			)
			if err != nil {
				return fmt.Errorf("insert weekly assignment: %w", err)
			}
		}
		for _, d := range v.Deficits {
			var deficitID int64
			weeks := any(nil)
			if d.Weeks > 0 {
				weeks = d.Weeks
			}
			err := exec.QueryRow(ctx,
				`INSERT INTO variant_deficit (variant_id, competency_id, competency_name_ru,
				   requirement_ids, person_days, weeks, external_cooperation_flag)
				 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
				v.VariantID, d.CompetencyID, d.CompetencyNameRU, stringsOrEmpty(d.RequirementIDs),
				d.PersonDays, weeks, d.ExternalCooperationFlag,
			).Scan(&deficitID)
			if err != nil {
				return fmt.Errorf("insert variant deficit: %w", err)
			}
			for _, a := range d.RecommendedActions {
				_, err := exec.Exec(ctx,
					`INSERT INTO variant_recommended_action (deficit_id, action, estimated_cost_units,
					   time_to_start_weeks, from_project_id, reason_codes)
					 VALUES ($1,$2,$3,$4,$5,$6)`,
					deficitID, a.Action, a.EstimatedCostUnits, a.TimeToStartWeeks,
					nullString(a.FromProjectID), stringsOrEmpty(a.ReasonCodes),
				)
				if err != nil {
					return fmt.Errorf("insert recommended action: %w", err)
				}
			}
		}
		for _, c := range v.ActiveConstraints {
			_, err := exec.Exec(ctx,
				`INSERT INTO variant_active_constraint (variant_id, type, value, source, reason_codes)
				 VALUES ($1,$2,$3,$4,$5)`,
				v.VariantID, c.Type, jsonbOrEmpty(c.Value), c.Source, stringsOrEmpty(c.ReasonCodes),
			)
			if err != nil {
				return fmt.Errorf("insert active constraint: %w", err)
			}
		}
		for _, e := range v.ExcludedCandidates {
			_, err := exec.Exec(ctx,
				`INSERT INTO variant_excluded_candidate (variant_id, employee_id, reason_codes, detail)
				 VALUES ($1,$2,$3,$4)`,
				v.VariantID, e.EmployeeID, stringsOrEmpty(e.ReasonCodes), jsonbOrEmpty(e.Detail),
			)
			if err != nil {
				return fmt.Errorf("insert excluded candidate: %w", err)
			}
		}
	}
	return nil
}

func upsertExplanation(ctx context.Context, exec sqlExecutor, e domain.Explanation) error {
	created := e.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := exec.Exec(ctx,
		`INSERT INTO explanation (explanation_id, variant_id, locale, headline, tradeoff,
		   included, excluded, deficits, counterfactuals, payload_jsonb, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 ON CONFLICT (variant_id, locale) DO UPDATE SET
		   headline = EXCLUDED.headline, tradeoff = EXCLUDED.tradeoff,
		   included = EXCLUDED.included, excluded = EXCLUDED.excluded,
		   deficits = EXCLUDED.deficits, counterfactuals = EXCLUDED.counterfactuals,
		   payload_jsonb = EXCLUDED.payload_jsonb, created_at = EXCLUDED.created_at`,
		e.ExplanationID, e.VariantID, e.Locale, e.Headline,
		jsonbOrEmpty(e.Tradeoff), jsonbOrEmpty(e.Included), jsonbOrEmpty(e.Excluded),
		jsonbOrEmpty(e.Deficits), jsonbOrEmpty(e.Counterfactuals),
		jsonbOrEmpty(e.Payload), created,
	)
	if err != nil {
		return fmt.Errorf("upsert explanation: %w", err)
	}
	return nil
}
