package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teamopt/backend/internal/domain"
)

// ErrDecisionExists is returned when a decision has already been recorded for a run.
var ErrDecisionExists = errors.New("decision already recorded")

// Workflow stores. PgAuditRepo implements WorkflowAuditRepo: Create lives in
// postgres.go and ListByEntity is added here without changing that file.
type (
	BriefRepo interface {
		Create(ctx context.Context, b domain.Brief) error
		Get(ctx context.Context, id string) (domain.Brief, error)
		SetStatus(ctx context.Context, id, status string) error
	}

	RunRepo interface {
		Create(ctx context.Context, r domain.Run) error
		Get(ctx context.Context, id string) (domain.Run, error)
		Finish(ctx context.Context, id, status string, finishedAt time.Time, payload json.RawMessage) error
		ListByBrief(ctx context.Context, briefID string) ([]domain.Run, error)
	}

	SnapshotRepo interface {
		Create(ctx context.Context, s domain.Snapshot) error
		GetData(ctx context.Context, runID string) (json.RawMessage, error)
	}

	VariantRepo interface {
		CreateBatch(ctx context.Context, variants []domain.Variant) error
		Get(ctx context.Context, runID, variantID string) (domain.Variant, error)
		ListByRun(ctx context.Context, runID string) ([]domain.Variant, error)
		Roster(ctx context.Context, runID string) (map[string][]string, error)
	}

	DecisionRepo interface {
		Create(ctx context.Context, d domain.Decision) error
		CreateWithAudit(ctx context.Context, d domain.Decision, entry domain.AuditEntry) error
		Get(ctx context.Context, runID string) (domain.Decision, error)
	}

	ExplanationRepo interface {
		Upsert(ctx context.Context, e domain.Explanation) error
		Get(ctx context.Context, runID, variantID, locale string) (domain.Explanation, error)
	}

	ConstraintOverrideRepo interface {
		CreateBatch(ctx context.Context, runID string, overrides []domain.ConstraintOverride) error
	}

	WorkflowAuditRepo interface {
		Create(ctx context.Context, e domain.AuditEntry) error
		ListByEntity(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEntry, int, error)
	}
)

func jsonbOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func stringsOrEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func jsonBytes(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(append([]byte(nil), raw...))
}

// ---------- PgBriefRepo ----------

type PgBriefRepo struct{ Pool *pgxpool.Pool }

func NewPgBriefRepo(pool *pgxpool.Pool) *PgBriefRepo { return &PgBriefRepo{Pool: pool} }

func (r *PgBriefRepo) Create(ctx context.Context, b domain.Brief) error {
	created := b.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO brief (brief_id, title, source_type, language, status, text, text_sha256,
		   requested_start_week, deadline_week, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		b.BriefID, b.Title, b.SourceType, b.Language, b.Status, b.Text, b.TextSHA256,
		nullString(b.RequestedStartWeek), nullString(b.DeadlineWeek), created, created,
	)
	if err != nil {
		if IsDuplicate(err) {
			return fmt.Errorf("%w: brief %s", ErrConflict, b.BriefID)
		}
		return fmt.Errorf("create brief: %w", err)
	}
	return nil
}

func (r *PgBriefRepo) Get(ctx context.Context, id string) (domain.Brief, error) {
	var b domain.Brief
	var start, deadline *string
	err := r.Pool.QueryRow(ctx,
		`SELECT brief_id, title, source_type, language, status, text, text_sha256,
		        COALESCE(requested_start_week,''), COALESCE(deadline_week,''), created_at, updated_at
		 FROM brief WHERE brief_id = $1`, id,
	).Scan(&b.BriefID, &b.Title, &b.SourceType, &b.Language, &b.Status, &b.Text, &b.TextSHA256,
		&start, &deadline, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Brief{}, fmt.Errorf("%w: brief %s", ErrNotFound, id)
		}
		return domain.Brief{}, fmt.Errorf("get brief: %w", err)
	}
	if start != nil {
		b.RequestedStartWeek = *start
	}
	if deadline != nil {
		b.DeadlineWeek = *deadline
	}
	return b, nil
}

func (r *PgBriefRepo) SetStatus(ctx context.Context, id, status string) error {
	tag, err := r.Pool.Exec(ctx,
		"UPDATE brief SET status = $2, updated_at = now() WHERE brief_id = $1", id, status)
	if err != nil {
		return fmt.Errorf("set brief status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: brief %s", ErrNotFound, id)
	}
	return nil
}

// ---------- PgRunRepo ----------

type PgRunRepo struct{ Pool *pgxpool.Pool }

func NewPgRunRepo(pool *pgxpool.Pool) *PgRunRepo { return &PgRunRepo{Pool: pool} }

const runColumns = `run_id, brief_id, COALESCE(parent_run_id,''), status, preset, k, seed,
	time_limit_ms, start_week, end_week, weights, COALESCE(constraints,'{}'::jsonb),
	COALESCE(candidate_filter,'{}'::jsonb), COALESCE(solver_status,''),
	COALESCE(solve_time_ms,0), COALESCE(ml_status,''), COALESCE(ml_attempts,0),
	COALESCE(validator_rejected,0), COALESCE(fallback_used,false), COALESCE(fallback_reason,''),
	metrics_version, contract_version, reason_codes, created_at, COALESCE(finished_at,'0001-01-01T00:00:00Z'::timestamptz),
	payload_jsonb`

func scanRun(rows interface{ Scan(...any) error }) (domain.Run, error) {
	var r domain.Run
	var weights, constraints, filter, payload []byte
	var finished time.Time
	err := rows.Scan(&r.RunID, &r.BriefID, &r.ParentRunID, &r.Status, &r.Preset, &r.K, &r.Seed,
		&r.TimeLimitMS, &r.StartWeek, &r.EndWeek, &weights, &constraints, &filter,
		&r.SolverStatus, &r.SolveTimeMS, &r.MLStatus, &r.MLAttempts, &r.ValidatorRejected,
		&r.FallbackUsed, &r.FallbackReason, &r.MetricsVersion, &r.ContractVersion,
		&r.ReasonCodes, &r.CreatedAt, &finished, &payload)
	if err != nil {
		return domain.Run{}, err
	}
	r.Weights = jsonBytes(weights)
	r.Constraints = jsonBytes(constraints)
	r.CandidateFilter = jsonBytes(filter)
	r.Payload = jsonBytes(payload)
	if !finished.IsZero() {
		r.FinishedAt = finished
	}
	return r, nil
}

func (r *PgRunRepo) Create(ctx context.Context, run domain.Run) error {
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
	_, err := r.Pool.Exec(ctx,
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

func (r *PgRunRepo) Get(ctx context.Context, id string) (domain.Run, error) {
	run, err := scanRun(r.Pool.QueryRow(ctx,
		"SELECT "+runColumns+" FROM team_run WHERE run_id = $1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Run{}, fmt.Errorf("%w: run %s", ErrNotFound, id)
		}
		return domain.Run{}, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

func (r *PgRunRepo) Finish(ctx context.Context, id, status string, finishedAt time.Time, payload json.RawMessage) error {
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	tag, err := r.Pool.Exec(ctx,
		"UPDATE team_run SET status = $2, finished_at = $3, payload_jsonb = $4 WHERE run_id = $1",
		id, status, finishedAt, jsonbOrEmpty(payload))
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: run %s", ErrNotFound, id)
	}
	return nil
}

func (r *PgRunRepo) ListByBrief(ctx context.Context, briefID string) ([]domain.Run, error) {
	rows, err := r.Pool.Query(ctx,
		"SELECT "+runColumns+" FROM team_run WHERE brief_id = $1 ORDER BY created_at DESC, run_id", briefID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	out := []domain.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return out, nil
}

// ---------- PgSnapshotRepo ----------

type PgSnapshotRepo struct{ Pool *pgxpool.Pool }

func NewPgSnapshotRepo(pool *pgxpool.Pool) *PgSnapshotRepo { return &PgSnapshotRepo{Pool: pool} }

func (r *PgSnapshotRepo) Create(ctx context.Context, s domain.Snapshot) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin snapshot tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created := s.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err = tx.Exec(ctx,
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
		_, err := tx.Exec(ctx,
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
	return tx.Commit(ctx)
}

func (r *PgSnapshotRepo) GetData(ctx context.Context, runID string) (json.RawMessage, error) {
	var raw []byte
	err := r.Pool.QueryRow(ctx,
		"SELECT data_jsonb FROM run_snapshot WHERE run_id = $1", runID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: snapshot for run %s", ErrNotFound, runID)
		}
		return nil, fmt.Errorf("get snapshot data: %w", err)
	}
	return jsonBytes(raw), nil
}

// ---------- PgVariantRepo ----------

type PgVariantRepo struct{ Pool *pgxpool.Pool }

func NewPgVariantRepo(pool *pgxpool.Pool) *PgVariantRepo { return &PgVariantRepo{Pool: pool} }

func (r *PgVariantRepo) CreateBatch(ctx context.Context, variants []domain.Variant) error {
	if len(variants) == 0 {
		return nil
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin variant tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, v := range variants {
		label := v.Label
		if len(label) > 120 {
			label = label[:120]
		}
		if label == "" {
			label = v.VariantID
		}
		_, err := tx.Exec(ctx,
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
			_, err := tx.Exec(ctx,
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
			_, err := tx.Exec(ctx,
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
			err := tx.QueryRow(ctx,
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
				_, err := tx.Exec(ctx,
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
			_, err := tx.Exec(ctx,
				`INSERT INTO variant_active_constraint (variant_id, type, value, source, reason_codes)
				 VALUES ($1,$2,$3,$4,$5)`,
				v.VariantID, c.Type, jsonbOrEmpty(c.Value), c.Source, stringsOrEmpty(c.ReasonCodes),
			)
			if err != nil {
				return fmt.Errorf("insert active constraint: %w", err)
			}
		}
		for _, e := range v.ExcludedCandidates {
			_, err := tx.Exec(ctx,
				`INSERT INTO variant_excluded_candidate (variant_id, employee_id, reason_codes, detail)
				 VALUES ($1,$2,$3,$4)`,
				v.VariantID, e.EmployeeID, stringsOrEmpty(e.ReasonCodes), jsonbOrEmpty(e.Detail),
			)
			if err != nil {
				return fmt.Errorf("insert excluded candidate: %w", err)
			}
		}
	}
	return tx.Commit(ctx)
}

const variantColumns = `variant_id, run_id, label, gate_pass, gate_g1, gate_g2, gate_g3,
	gate_g4, gate_g5, COALESCE(max_overload_pct,0), score, team_size, cost_units,
	duration, metric_values, COALESCE(metric_details,'{}'::jsonb), reason_codes, payload_jsonb`

func scanVariant(rows interface{ Scan(...any) error }) (domain.Variant, error) {
	var v domain.Variant
	var duration, values, details, payload []byte
	err := rows.Scan(&v.VariantID, &v.RunID, &v.Label, &v.GatePass, &v.GateG1, &v.GateG2,
		&v.GateG3, &v.GateG4, &v.GateG5, &v.MaxOverloadPct, &v.Score, &v.TeamSize,
		&v.CostUnits, &duration, &values, &details, &v.ReasonCodes, &payload)
	if err != nil {
		return domain.Variant{}, err
	}
	v.Duration = jsonBytes(duration)
	v.MetricValues = jsonBytes(values)
	v.MetricDetails = jsonBytes(details)
	v.Payload = jsonBytes(payload)
	return v, nil
}

func (r *PgVariantRepo) Get(ctx context.Context, runID, variantID string) (domain.Variant, error) {
	v, err := scanVariant(r.Pool.QueryRow(ctx,
		"SELECT "+variantColumns+" FROM team_variant WHERE run_id = $1 AND variant_id = $2",
		runID, variantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Variant{}, fmt.Errorf("%w: variant %s in run %s", ErrNotFound, variantID, runID)
		}
		return domain.Variant{}, fmt.Errorf("get variant: %w", err)
	}
	if err := loadVariantChildren(ctx, r.Pool, &v); err != nil {
		return domain.Variant{}, err
	}
	return v, nil
}

func (r *PgVariantRepo) ListByRun(ctx context.Context, runID string) ([]domain.Variant, error) {
	rows, err := r.Pool.Query(ctx,
		"SELECT "+variantColumns+" FROM team_variant WHERE run_id = $1 ORDER BY variant_id", runID)
	if err != nil {
		return nil, fmt.Errorf("list variants: %w", err)
	}
	variants := []domain.Variant{}
	for rows.Next() {
		v, err := scanVariant(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan variant: %w", err)
		}
		variants = append(variants, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate variants: %w", err)
	}
	rows.Close()
	for i := range variants {
		if err := loadVariantChildren(ctx, r.Pool, &variants[i]); err != nil {
			return nil, err
		}
	}
	return variants, nil
}

func (r *PgVariantRepo) Roster(ctx context.Context, runID string) (map[string][]string, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT v.variant_id, m.employee_id FROM variant_member m
		 JOIN team_variant v ON v.variant_id = m.variant_id
		 WHERE v.run_id = $1 ORDER BY v.variant_id, m.employee_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list variant roster: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var variantID, employeeID string
		if err := rows.Scan(&variantID, &employeeID); err != nil {
			return nil, fmt.Errorf("scan variant roster: %w", err)
		}
		out[variantID] = append(out[variantID], employeeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate variant roster: %w", err)
	}
	return out, nil
}

func loadVariantChildren(ctx context.Context, pool *pgxpool.Pool, v *domain.Variant) error {
	rows, err := pool.Query(ctx,
		`SELECT employee_id, role_id, grade, allocated_hours_minutes, assignment_share,
		        competence_fit, reason_codes
		 FROM variant_member WHERE variant_id = $1 ORDER BY employee_id`, v.VariantID)
	if err != nil {
		return fmt.Errorf("list variant members: %w", err)
	}
	for rows.Next() {
		var m domain.VariantMember
		var minutes int
		if err := rows.Scan(&m.EmployeeID, &m.RoleID, &m.Grade, &minutes,
			&m.AssignmentShare, &m.CompetenceFit, &m.ReasonCodes); err != nil {
			rows.Close()
			return fmt.Errorf("scan variant member: %w", err)
		}
		m.AllocatedHours = float64(minutes) / 60.0
		v.Members = append(v.Members, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate variant members: %w", err)
	}
	rows.Close()

	rows, err = pool.Query(ctx,
		`SELECT employee_id, requirement_id, week, hours_minutes, share
		 FROM variant_weekly_assignment WHERE variant_id = $1
		 ORDER BY employee_id, week, requirement_id`, v.VariantID)
	if err != nil {
		return fmt.Errorf("list weekly assignments: %w", err)
	}
	for rows.Next() {
		var a domain.VariantWeeklyAssignment
		var minutes int
		if err := rows.Scan(&a.EmployeeID, &a.RequirementID, &a.Week, &minutes, &a.Share); err != nil {
			rows.Close()
			return fmt.Errorf("scan weekly assignment: %w", err)
		}
		a.Hours = float64(minutes) / 60.0
		v.WeeklyAssignments = append(v.WeeklyAssignments, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate weekly assignments: %w", err)
	}
	rows.Close()

	rows, err = pool.Query(ctx,
		`SELECT id, competency_id, competency_name_ru, requirement_ids, person_days,
		        COALESCE(weeks,0), external_cooperation_flag
		 FROM variant_deficit WHERE variant_id = $1 ORDER BY competency_id`, v.VariantID)
	if err != nil {
		return fmt.Errorf("list variant deficits: %w", err)
	}
	deficits := []struct {
		id domain.VariantDeficit
		db int64
	}{}
	for rows.Next() {
		var d domain.VariantDeficit
		var dbID int64
		if err := rows.Scan(&dbID, &d.CompetencyID, &d.CompetencyNameRU, &d.RequirementIDs,
			&d.PersonDays, &d.Weeks, &d.ExternalCooperationFlag); err != nil {
			rows.Close()
			return fmt.Errorf("scan variant deficit: %w", err)
		}
		deficits = append(deficits, struct {
			id domain.VariantDeficit
			db int64
		}{d, dbID})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate variant deficits: %w", err)
	}
	rows.Close()
	for _, entry := range deficits {
		actionRows, err := pool.Query(ctx,
			`SELECT action, estimated_cost_units, time_to_start_weeks, from_project_id, reason_codes
			 FROM variant_recommended_action WHERE deficit_id = $1 ORDER BY id`, entry.db)
		if err != nil {
			return fmt.Errorf("list recommended actions: %w", err)
		}
		d := entry.id
		for actionRows.Next() {
			var a domain.VariantRecommendedAction
			var from *string
			if err := actionRows.Scan(&a.Action, &a.EstimatedCostUnits, &a.TimeToStartWeeks,
				&from, &a.ReasonCodes); err != nil {
				actionRows.Close()
				return fmt.Errorf("scan recommended action: %w", err)
			}
			if from != nil {
				a.FromProjectID = *from
			}
			d.RecommendedActions = append(d.RecommendedActions, a)
		}
		if err := actionRows.Err(); err != nil {
			actionRows.Close()
			return fmt.Errorf("iterate recommended actions: %w", err)
		}
		actionRows.Close()
		v.Deficits = append(v.Deficits, d)
	}

	rows, err = pool.Query(ctx,
		`SELECT type, value, source, reason_codes FROM variant_active_constraint
		 WHERE variant_id = $1 ORDER BY id`, v.VariantID)
	if err != nil {
		return fmt.Errorf("list active constraints: %w", err)
	}
	for rows.Next() {
		var c domain.VariantActiveConstraint
		var value []byte
		if err := rows.Scan(&c.Type, &value, &c.Source, &c.ReasonCodes); err != nil {
			rows.Close()
			return fmt.Errorf("scan active constraint: %w", err)
		}
		c.Value = jsonBytes(value)
		v.ActiveConstraints = append(v.ActiveConstraints, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate active constraints: %w", err)
	}
	rows.Close()

	rows, err = pool.Query(ctx,
		`SELECT employee_id, reason_codes, detail FROM variant_excluded_candidate
		 WHERE variant_id = $1 ORDER BY employee_id`, v.VariantID)
	if err != nil {
		return fmt.Errorf("list excluded candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e domain.VariantExcludedCandidate
		var detail []byte
		if err := rows.Scan(&e.EmployeeID, &e.ReasonCodes, &detail); err != nil {
			return fmt.Errorf("scan excluded candidate: %w", err)
		}
		e.Detail = jsonBytes(detail)
		v.ExcludedCandidates = append(v.ExcludedCandidates, e)
	}
	return rows.Err()
}

// ---------- PgDecisionRepo ----------

type PgDecisionRepo struct{ Pool *pgxpool.Pool }

func NewPgDecisionRepo(pool *pgxpool.Pool) *PgDecisionRepo { return &PgDecisionRepo{Pool: pool} }

func (r *PgDecisionRepo) Create(ctx context.Context, d domain.Decision) error {
	decidedAt := d.DecidedAt
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	tag, err := r.Pool.Exec(ctx,
		`INSERT INTO decision (run_id, variant_id, decided_at, decided_by, comment,
		   edits_applied, audit_event_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (run_id) DO NOTHING`,
		d.RunID, d.VariantID, decidedAt, d.DecidedBy, nullString(d.Comment),
		jsonbOrEmpty(d.EditsApplied), d.AuditEventID,
	)
	if err != nil {
		return fmt.Errorf("create decision: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDecisionExists
	}
	return nil
}

// CreateWithAudit commits the decision and its audit evidence together. A conflict rolls
// back the audit row, so a decision never points at an audit event that does not exist.
func (r *PgDecisionRepo) CreateWithAudit(ctx context.Context, d domain.Decision, entry domain.AuditEntry) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin decision transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO audit_log (event_id, at, actor, action, entity_type, entity_id, request_id, summary, details)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		entry.EventID, entry.At, entry.Actor, entry.Action, entry.EntityType,
		entry.EntityID, entry.RequestID, entry.Summary, entry.Details,
	)
	if err != nil {
		return fmt.Errorf("create decision audit: %w", err)
	}
	decidedAt := d.DecidedAt
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	tag, err := tx.Exec(ctx,
		`INSERT INTO decision (run_id, variant_id, decided_at, decided_by, comment,
		   edits_applied, audit_event_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (run_id) DO NOTHING`,
		d.RunID, d.VariantID, decidedAt, d.DecidedBy, nullString(d.Comment),
		jsonbOrEmpty(d.EditsApplied), d.AuditEventID,
	)
	if err != nil {
		return fmt.Errorf("create decision: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDecisionExists
	}
	return tx.Commit(ctx)
}

func (r *PgDecisionRepo) Get(ctx context.Context, runID string) (domain.Decision, error) {
	var d domain.Decision
	var comment *string
	var edits []byte
	err := r.Pool.QueryRow(ctx,
		`SELECT run_id, variant_id, decided_at, decided_by, COALESCE(comment,''),
		        COALESCE(edits_applied,'[]'::jsonb), audit_event_id
		 FROM decision WHERE run_id = $1`, runID,
	).Scan(&d.RunID, &d.VariantID, &d.DecidedAt, &d.DecidedBy, &comment, &edits, &d.AuditEventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Decision{}, fmt.Errorf("%w: decision for run %s", ErrNotFound, runID)
		}
		return domain.Decision{}, fmt.Errorf("get decision: %w", err)
	}
	if comment != nil {
		d.Comment = *comment
	}
	d.EditsApplied = jsonBytes(edits)
	if string(d.EditsApplied) == "[]" {
		d.EditsApplied = json.RawMessage("[]")
	}
	return d, nil
}

// ---------- PgExplanationRepo ----------

type PgExplanationRepo struct{ Pool *pgxpool.Pool }

func NewPgExplanationRepo(pool *pgxpool.Pool) *PgExplanationRepo {
	return &PgExplanationRepo{Pool: pool}
}

func (r *PgExplanationRepo) Upsert(ctx context.Context, e domain.Explanation) error {
	created := e.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := r.Pool.Exec(ctx,
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

func (r *PgExplanationRepo) Get(ctx context.Context, runID, variantID, locale string) (domain.Explanation, error) {
	var e domain.Explanation
	var tradeoff, included, excluded, deficits, counterfactuals, payload []byte
	err := r.Pool.QueryRow(ctx,
		`SELECT e.explanation_id, v.run_id, e.variant_id, e.locale, e.headline, e.tradeoff,
		        e.included, e.excluded, e.deficits, e.counterfactuals, e.payload_jsonb, e.created_at
		 FROM explanation e JOIN team_variant v ON v.variant_id = e.variant_id
		 WHERE v.run_id = $1 AND e.variant_id = $2 AND e.locale = $3`,
		runID, variantID, locale,
	).Scan(&e.ExplanationID, &e.RunID, &e.VariantID, &e.Locale, &e.Headline, &tradeoff,
		&included, &excluded, &deficits, &counterfactuals, &payload, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Explanation{}, fmt.Errorf("%w: explanation %s/%s", ErrNotFound, variantID, locale)
		}
		return domain.Explanation{}, fmt.Errorf("get explanation: %w", err)
	}
	e.Tradeoff = jsonBytes(tradeoff)
	e.Included = jsonBytes(included)
	e.Excluded = jsonBytes(excluded)
	e.Deficits = jsonBytes(deficits)
	e.Counterfactuals = jsonBytes(counterfactuals)
	e.Payload = jsonBytes(payload)
	return e, nil
}

// ---------- PgConstraintOverrideRepo ----------

type PgConstraintOverrideRepo struct{ Pool *pgxpool.Pool }

func NewPgConstraintOverrideRepo(pool *pgxpool.Pool) *PgConstraintOverrideRepo {
	return &PgConstraintOverrideRepo{Pool: pool}
}

func (r *PgConstraintOverrideRepo) CreateBatch(ctx context.Context, runID string, overrides []domain.ConstraintOverride) error {
	if len(overrides) == 0 {
		return nil
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin override tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, o := range overrides {
		_, err := tx.Exec(ctx,
			`INSERT INTO constraint_override (run_id, type, employee_id, requirement_id,
			   value, week, reason)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			runID, o.Type, nullString(o.EmployeeID), nullString(o.RequirementID),
			o.Value, nullString(o.Week), nullString(o.Reason),
		)
		if err != nil {
			return fmt.Errorf("insert constraint override: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// ---------- PgAuditRepo.ListByEntity ----------

func (r *PgAuditRepo) ListByEntity(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEntry, int, error) {
	where := "entity_type = $1 AND entity_id = $2"
	args := []any{f.EntityType, f.EntityID}
	var total int
	if err := r.Pool.QueryRow(ctx,
		"SELECT count(*) FROM audit_log WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit entries: %w", err)
	}
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT event_id, at, actor, action, entity_type, entity_id,
		        COALESCE(request_id,''), summary, COALESCE(details,'{}'::jsonb)
		 FROM audit_log WHERE `+where+" ORDER BY at ASC, event_id LIMIT $3 OFFSET $4",
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	out := []domain.AuditEntry{}
	for rows.Next() {
		var e domain.AuditEntry
		var details []byte
		if err := rows.Scan(&e.EventID, &e.At, &e.Actor, &e.Action, &e.EntityType, &e.EntityID,
			&e.RequestID, &e.Summary, &details); err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}
		e.Details = jsonBytes(details)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit entries: %w", err)
	}
	return out, total, nil
}
