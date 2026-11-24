package service

// Durable workflow state. Every write goes through the stores when they are configured;
// the in-memory maps remain the fallback for unit tests without a database.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/explain"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/platform/actor"
	"github.com/teamopt/backend/internal/repository"
	"github.com/teamopt/backend/internal/runinput"
)

// WorkflowStores is the set of repositories the workflow needs. A nil field disables
// only that operation; the in-memory maps answer the rest.
type WorkflowStores struct {
	Briefs       repository.BriefRepo
	Runs         repository.RunRepo
	Snapshots    repository.SnapshotRepo
	Variants     repository.VariantRepo
	Decisions    repository.DecisionRepo
	Explanations repository.ExplanationRepo
	Overrides    repository.ConstraintOverrideRepo
	Audit        repository.WorkflowAuditRepo
}

// SetStores wires durable persistence into the workflow. It is called once during
// startup; tests that never call it keep the original in-memory behavior.
func (s *WorkflowService) SetStores(stores WorkflowStores) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stores = &stores
}

func (s *WorkflowService) dbBacked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stores != nil && s.stores.Runs != nil
}

// ---------- identifiers ----------

func (s *WorkflowService) newBriefID(seq int) string {
	if s.dbBacked() {
		return domain.GenerateID("brf_", 8)
	}
	return fmt.Sprintf("brf_%04d", seq)
}

func (s *WorkflowService) newRunID(seq int) string {
	if s.dbBacked() {
		return domain.GenerateID("run_", 8)
	}
	return fmt.Sprintf("run_%04d", seq)
}

func (s *WorkflowService) newAuditID() string {
	if s.dbBacked() {
		return domain.GenerateID("aud_", 8)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return fmt.Sprintf("aud_%04d", s.next)
}

// ---------- JSON shape helpers ----------

func wfMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func wfSlice(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
}

func wfString(v any) string {
	s, _ := v.(string)
	return s
}

func wfFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func wfInt(v any) int { return int(wfFloat(v)) }

func wfStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return append([]string{}, x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, entry := range x {
			if s, ok := entry.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
}

func wfTime(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case string:
		t, _ := time.Parse(time.RFC3339, x)
		return t
	}
	return time.Time{}
}

func wfJSON(v any) json.RawMessage {
	encoded, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

// storedVariantID namespaces a solver variant id by its run: team_variant.variant_id is
// a global primary key, while the solver may reuse ids across runs. The contract id stays
// in payload_jsonb, so read paths still return the original id.
func storedVariantID(runID, variantID string) string {
	sum := sha256.Sum256([]byte(runID + "|" + variantID))
	return "var_" + hex.EncodeToString(sum[:])[:16]
}

func wfJSONMap(v any) map[string]any {
	out := map[string]any{}
	encoded, err := json.Marshal(v)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(encoded, &out)
	return out
}

// ---------- writes ----------

// persistBrief stores the brief row the run references. The ML-only fields live in the
// in-memory record; the database keeps the authoritative identity, status and text hash.
func (s *WorkflowService) persistBrief(ctx context.Context, b *BriefRecord) error {
	s.mu.RLock()
	repo := s.stores
	s.mu.RUnlock()
	if repo == nil || repo.Briefs == nil {
		return nil
	}
	if _, err := repo.Briefs.Get(ctx, b.BriefID); err == nil {
		return nil
	}
	source := b.SourceType
	switch source {
	case "text", "docx", "pdf", "txt":
	default:
		source = "txt"
	}
	language := b.Language
	if language != "en" {
		language = "ru"
	}
	title := b.Title
	if title == "" {
		title = "Без названия"
	}
	if len(title) > 200 {
		title = truncateRunes(title, 200)
	}
	text := b.Text
	if len(text) > 200000 {
		text = truncateRunes(text, 200000)
	}
	sum := sha256.Sum256([]byte(text))
	return repo.Briefs.Create(ctx, domain.Brief{
		BriefID: b.BriefID, Title: title, SourceType: source, Language: language,
		Status: b.Status, Text: text, TextSHA256: hex.EncodeToString(sum[:]),
		RequestedStartWeek: b.RequestedStartWeek, DeadlineWeek: b.DeadlineWeek,
		CreatedAt: b.CreatedAt,
	})
}

func (s *WorkflowService) confirmBriefStored(id string) {
	s.mu.RLock()
	repo := s.stores
	s.mu.RUnlock()
	if repo == nil || repo.Briefs == nil {
		return
	}
	if err := repo.Briefs.SetStatus(context.Background(), id, "confirmed"); err != nil {
		slog.WarnContext(context.Background(), "confirm brief in store", "error", err)
	}
}

// auditEvent records a workflow fact in the database (when wired) and in the in-memory
// fallback. The actor is always server-derived; client identity assertions are ignored.
func (s *WorkflowService) auditEvent(ctx context.Context, action, entityType, entityID, summary string, details any) (string, error) {
	id := s.newAuditID()
	return s.auditEventWithID(ctx, id, action, entityType, entityID, summary, details)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func (s *WorkflowService) auditEventWithID(ctx context.Context, id, action, entityType, entityID, summary string, details any) (string, error) {
	summary = truncateRunes(summary, 500)
	if summary == "" {
		summary = action
	}
	requestID := domain.GenerateID("req_", 8)
	entry := domain.AuditEntry{
		EventID: id, At: time.Now().UTC(), Actor: actor.FromContext(ctx),
		Action: action, EntityType: entityType, EntityID: entityID,
		RequestID: requestID, Summary: summary, Details: wfJSON(details),
	}
	s.mu.RLock()
	repo := s.stores
	s.mu.RUnlock()
	if repo != nil && repo.Audit != nil {
		if err := repo.Audit.Create(ctx, entry); err != nil {
			return "", fmt.Errorf("write audit %s: %w", action, err)
		}
	}
	s.mu.Lock()
	s.audits[entityID] = append(s.audits[entityID], entry)
	s.mu.Unlock()
	return id, nil
}

// persistComputed writes the complete, Go-verified run: the run row, its snapshot with
// the immutable input data, every variant with all children and both explanation locales.
func (s *WorkflowService) persistComputed(
	ctx context.Context,
	briefID, runID, parentRunID string,
	payload map[string]any,
	data runinput.Data,
	snapshot *runinput.Snapshot,
	payloads map[string]map[string]any,
	details map[string]explain.VariantDetail,
	varResults map[string]metrics.VariantResult,
	createdAt time.Time,
) error {
	s.mu.RLock()
	repo := s.stores
	s.mu.RUnlock()
	if repo == nil || repo.Runs == nil {
		return nil
	}

	if err := repo.Runs.Create(ctx, domainRunFromPayload(runID, briefID, parentRunID, payload, data, createdAt)); err != nil {
		return fmt.Errorf("persist run: %w", err)
	}
	if repo.Snapshots != nil {
		if err := repo.Snapshots.Create(ctx, domainSnapshotFrom(snapshot, data)); err != nil {
			return fmt.Errorf("persist snapshot: %w", err)
		}
	}
	if repo.Variants != nil {
		if err := repo.Variants.CreateBatch(ctx, domainVariantsFrom(runID, payloads)); err != nil {
			return fmt.Errorf("persist variants: %w", err)
		}
	}
	if repo.Explanations != nil {
		for variantID, result := range varResults {
			detail, ok := details[variantID]
			if !ok {
				continue
			}
			for _, locale := range []string{"ru", "en"} {
				exp, err := generateExplanation(runID, variantID, locale, snapshot, result, detail)
				if err != nil {
					continue
				}
				explanation := domainExplanationFrom(runID, storedVariantID(runID, variantID), locale, exp)
				if err := repo.Explanations.Upsert(ctx, explanation); err != nil {
					return fmt.Errorf("persist explanation %s/%s: %w", variantID, locale, err)
				}
			}
		}
	}
	return nil
}

func domainRunFromPayload(runID, briefID, parentRunID string, payload map[string]any, data runinput.Data, createdAt time.Time) domain.Run {
	horizon := wfMap(payload["horizon"])
	diagnostics := wfMap(payload["diagnostics"])
	finishedAt := wfTime(payload["finished_at"])
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	return domain.Run{
		RunID: runID, BriefID: briefID, ParentRunID: parentRunID,
		Status: wfString(payload["status"]), Preset: data.Preset, K: data.K,
		Seed: data.Seed, TimeLimitMS: data.TimeLimitMS,
		StartWeek: wfString(horizon["start_week"]), EndWeek: wfString(horizon["end_week"]),
		Weights: wfJSON(data.Weights), Constraints: wfJSON(data.Constraints),
		SolverStatus:      wfString(diagnostics["solver_status"]),
		SolveTimeMS:       wfInt(diagnostics["solve_time_ms"]),
		MLStatus:          wfString(diagnostics["ml_status"]),
		MLAttempts:        wfInt(diagnostics["ml_attempts"]),
		ValidatorRejected: wfInt(diagnostics["validator_rejected"]),
		FallbackUsed:      diagnostics["fallback_used"] == true,
		MetricsVersion:    wfString(diagnostics["metrics_version"]),
		ContractVersion:   wfString(diagnostics["contract_version"]),
		ReasonCodes:       wfStrings(payload["reason_codes"]),
		CreatedAt:         createdAt, FinishedAt: finishedAt,
		Payload: wfJSON(payload),
	}
}

func domainSnapshotFrom(snapshot *runinput.Snapshot, data runinput.Data) domain.Snapshot {
	horizon := snapshot.Horizon()
	start, end := "", ""
	if len(horizon) > 0 {
		start, end = horizon[0], horizon[len(horizon)-1]
	}
	capacityRows := 0
	candidates := make([]domain.SnapshotCandidate, 0, len(snapshot.Candidates()))
	for _, c := range snapshot.Candidates() {
		capacityRows += len(c.Capacity)
		candidates = append(candidates, domain.SnapshotCandidate{
			EmployeeID: c.EmployeeID, Grade: c.Grade, CostCoefficient: c.CostCoefficient,
			CompetencyIDs: competencyIDs(c), CapacityDigest: capacityDigest(c),
			CapacityWeeks: len(c.Capacity),
		})
	}
	return domain.Snapshot{
		SnapshotID: "snp_" + snapshot.SnapshotSHA256()[:8],
		RunID:      snapshot.RunID(), SnapshotSHA256: snapshot.SnapshotSHA256(),
		HorizonStart: start, HorizonEnd: end,
		Weights: wfJSON(snapshot.Weights()), Constraints: wfJSON(snapshot.Constraints()),
		RequirementCount: len(snapshot.Requirements()), CandidateCount: len(snapshot.Candidates()),
		CapacityRows: capacityRows, Data: wfJSON(data), Candidates: candidates,
		CreatedAt: snapshot.CreatedAt(),
	}
}

func domainVariantsFrom(runID string, payloads map[string]map[string]any) []domain.Variant {
	out := make([]domain.Variant, 0, len(payloads))
	for variantID, raw := range payloads {
		v := wfJSONMap(raw)
		gate := wfMap(v["gate_violations"])
		metricDetails := wfMap(v["metric_details"])
		maxOverload, _ := metricDetails["max_overload_pct"].(float64)
		variant := domain.Variant{
			VariantID: storedVariantID(runID, variantID), RunID: wfString(v["run_id"]), Label: wfString(v["label"]),
			GatePass: v["gate_pass"] == true,
			GateG1:   wfInt(gate["G1"]), GateG2: wfInt(gate["G2"]), GateG3: wfInt(gate["G3"]),
			GateG4: wfInt(gate["G4"]), GateG5: wfInt(gate["G5"]),
			MaxOverloadPct: maxOverload,
			Score:          wfFloat(v["score"]), TeamSize: wfInt(v["team_size"]),
			CostUnits: wfFloat(v["cost_units"]),
			Duration:  wfJSON(v["duration"]), MetricValues: wfJSON(v["metric_values"]),
			MetricDetails: wfJSON(v["metric_details"]),
			ReasonCodes:   wfStrings(v["reason_codes"]), Payload: wfJSON(v),
		}
		for _, rawMember := range wfSlice(v["members"]) {
			m := wfMap(rawMember)
			variant.Members = append(variant.Members, domain.VariantMember{
				EmployeeID: wfString(m["employee_id"]), RoleID: wfString(m["role_id"]),
				Grade: wfString(m["grade"]), AllocatedHours: wfFloat(m["allocated_hours"]),
				AssignmentShare: wfFloat(m["assignment_share"]),
				CompetenceFit:   wfFloat(m["competence_fit"]),
				ReasonCodes:     wfStrings(m["reason_codes"]),
			})
		}
		for _, rawAssignment := range wfSlice(v["weekly_assignments"]) {
			a := wfMap(rawAssignment)
			variant.WeeklyAssignments = append(variant.WeeklyAssignments, domain.VariantWeeklyAssignment{
				EmployeeID: wfString(a["employee_id"]), RequirementID: wfString(a["requirement_id"]),
				Week: wfString(a["week"]), Hours: wfFloat(a["hours"]), Share: wfFloat(a["share"]),
			})
		}
		for _, rawDeficit := range wfSlice(v["deficits"]) {
			d := wfMap(rawDeficit)
			deficit := domain.VariantDeficit{
				CompetencyID:     wfString(d["competency_id"]),
				CompetencyNameRU: wfString(d["competency_name_ru"]),
				RequirementIDs:   wfStrings(d["requirement_ids"]),
				PersonDays:       wfFloat(d["person_days"]), Weeks: wfFloat(d["weeks"]),
				ExternalCooperationFlag: d["external_cooperation_flag"] == true,
			}
			for _, rawAction := range wfSlice(d["recommended_actions"]) {
				action := wfMap(rawAction)
				recommendation := domain.VariantRecommendedAction{
					Action: wfString(action["action"]), ReasonCodes: wfStrings(action["reason_codes"]),
					FromProjectID: wfString(action["from_project_id"]),
				}
				if cost, ok := action["estimated_cost_units"].(float64); ok {
					recommendation.EstimatedCostUnits = &cost
				}
				if weeks, ok := action["time_to_start_weeks"].(float64); ok {
					recommendation.TimeToStartWeeks = &weeks
				}
				deficit.RecommendedActions = append(deficit.RecommendedActions, recommendation)
			}
			variant.Deficits = append(variant.Deficits, deficit)
		}
		for _, rawConstraint := range wfSlice(v["active_constraints"]) {
			c := wfMap(rawConstraint)
			variant.ActiveConstraints = append(variant.ActiveConstraints, domain.VariantActiveConstraint{
				Type: wfString(c["type"]), Value: wfJSON(c["value"]),
				Source: wfString(c["source"]), ReasonCodes: wfStrings(c["reason_codes"]),
			})
		}
		for _, rawExcluded := range wfSlice(v["excluded_candidates"]) {
			e := wfMap(rawExcluded)
			variant.ExcludedCandidates = append(variant.ExcludedCandidates, domain.VariantExcludedCandidate{
				EmployeeID: wfString(e["employee_id"]), ReasonCodes: wfStrings(e["reason_codes"]),
				Detail: wfJSON(e["detail"]),
			})
		}
		out = append(out, variant)
	}
	return out
}

func domainExplanationFrom(runID, variantID, locale string, exp map[string]any) domain.Explanation {
	headline := wfString(exp["headline"])
	if headline == "" {
		headline = "Explanation for " + variantID
	}
	if len(headline) > 400 {
		headline = truncateRunes(headline, 400)
	}
	payload := wfJSON(exp)
	return domain.Explanation{
		ExplanationID: domain.GenerateID("exp_", 8), RunID: runID,
		VariantID: variantID, Locale: locale, Headline: headline,
		Tradeoff: wfJSON(exp["tradeoff"]), Included: wfJSON(exp["included"]),
		Excluded: wfJSON(exp["excluded"]), Deficits: wfJSON(exp["deficits"]),
		Counterfactuals: wfJSON(exp["counterfactuals"]), Payload: payload,
		CreatedAt: time.Now().UTC(),
	}
}

func domainOverridesFrom(runID string, overrides []ConstraintOverride) []domain.ConstraintOverride {
	out := make([]domain.ConstraintOverride, 0, len(overrides))
	for _, o := range overrides {
		out = append(out, domain.ConstraintOverride{
			RunID: runID, Type: o.Type, EmployeeID: o.EmployeeID,
			RequirementID: o.RequirementID, Value: o.Value, Week: o.Week, Reason: o.Reason,
		})
	}
	return out
}

// ---------- reads ----------

func decodeJSONMap(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return nil, false
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

func storedRun(run domain.Run) map[string]any {
	payload, ok := decodeJSONMap(run.Payload)
	if !ok {
		payload = map[string]any{}
	}
	if _, exists := payload["run_id"]; !exists {
		payload["run_id"] = run.RunID
		payload["brief_id"] = run.BriefID
		payload["status"] = run.Status
		payload["preset"] = run.Preset
		payload["k"] = run.K
		payload["seed"] = run.Seed
		payload["time_limit_ms"] = run.TimeLimitMS
		payload["created_at"] = run.CreatedAt
		if !run.FinishedAt.IsZero() {
			payload["finished_at"] = run.FinishedAt
		}
		payload["horizon"] = map[string]any{
			"start_week": run.StartWeek, "end_week": run.EndWeek,
		}
		payload["reason_codes"] = run.ReasonCodes
		payload["variants"] = []any{}
	}
	if run.ParentRunID != "" {
		payload["parent_run_id"] = run.ParentRunID
	}
	return payload
}

func (s *WorkflowService) storedDecision(ctx context.Context, runID string) (map[string]any, bool) {
	s.mu.RLock()
	repo := s.stores
	s.mu.RUnlock()
	if repo == nil || repo.Decisions == nil {
		return nil, false
	}
	d, err := repo.Decisions.Get(ctx, runID)
	if err != nil {
		return nil, false
	}
	edits := 0
	var list []any
	if json.Unmarshal(d.EditsApplied, &list) == nil {
		edits = len(list)
	}
	return map[string]any{
		"run_id": d.RunID, "variant_id": d.VariantID, "decided_at": d.DecidedAt,
		"decided_by": d.DecidedBy, "edits_count": edits, "audit_event_id": d.AuditEventID,
		"comment": d.Comment,
	}, true
}

// ListVariants serves GET /runs/{runId}/variants as contracts VariantPage.
func (s *WorkflowService) ListVariants(_ context.Context, runID string, limit, offset int) (map[string]any, bool) {
	run, ok := s.GetRun(runID)
	if !ok {
		return nil, false
	}
	rawItems := wfSlice(run["variants"])
	sort.SliceStable(rawItems, func(i, j int) bool {
		return wfFloat(wfMap(rawItems[i])["score"]) > wfFloat(wfMap(rawItems[j])["score"])
	})
	items := make([]any, 0, len(rawItems))
	for _, raw := range rawItems {
		v := wfMap(raw)
		items = append(items, map[string]any{
			"variant_id": v["variant_id"], "label": v["label"], "gate_pass": v["gate_pass"],
			"score": v["score"], "team_size": v["team_size"], "duration": v["duration"],
			"cost_units": v["cost_units"], "metric_values": v["metric_values"],
			"deficit_count": v["deficit_count"], "reason_codes": v["reason_codes"],
		})
	}
	total := len(items)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	start, end := offset, offset+limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	page := map[string]any{
		"items": items[start:end],
		"meta":  map[string]any{"limit": limit, "offset": offset, "total": total},
	}
	return jsonMap(page), true
}

// ListAudit serves GET /runs/{runId}/audit as contracts AuditLogPage, oldest first.
func (s *WorkflowService) ListAudit(ctx context.Context, runID string, limit, offset int) (map[string]any, bool) {
	if _, ok := s.GetRun(runID); !ok {
		return nil, false
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	s.mu.RLock()
	repo := s.stores
	entries := append([]domain.AuditEntry(nil), s.audits[runID]...)
	s.mu.RUnlock()
	if repo != nil && repo.Audit != nil {
		stored, total, err := repo.Audit.ListByEntity(ctx, domain.AuditFilter{
			EntityType: "run", EntityID: runID, Limit: limit, Offset: offset,
		})
		if err != nil {
			return nil, false
		}
		items := make([]any, 0, len(stored))
		for _, e := range stored {
			items = append(items, auditJSON(e))
		}
		return jsonMap(map[string]any{
			"items": items,
			"meta":  map[string]any{"limit": limit, "offset": offset, "total": total},
		}), true
	}
	total := len(entries)
	start, end := offset, offset+limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	items := make([]any, 0, end-start)
	for _, e := range entries[start:end] {
		items = append(items, auditJSON(e))
	}
	return jsonMap(map[string]any{
		"items": items,
		"meta":  map[string]any{"limit": limit, "offset": offset, "total": total},
	}), true
}

func auditJSON(e domain.AuditEntry) map[string]any {
	return map[string]any{
		"event_id": e.EventID, "at": e.At, "actor": e.Actor, "action": e.Action,
		"entity_type": e.EntityType, "entity_id": e.EntityID,
		"request_id": e.RequestID, "summary": e.Summary,
	}
}

// generateExplanation is the single source of Explanation payloads, shared by the
// read path and the persistence write path so a stored explanation never drifts.
func generateExplanation(
	runID, variantID, locale string,
	snapshot *runinput.Snapshot,
	result metrics.VariantResult,
	detail explain.VariantDetail,
) (map[string]any, error) {
	exp, err := explain.Generate(explain.Input{
		RunID: runID, Locale: locale, Snapshot: snapshot, Variant: result, Detail: detail,
		Schedule: metrics.ScheduleConfig{
			MonteCarloRuns:          metrics.DefaultMonteCarloRuns,
			Seed:                    snapshot.Seed(),
			PortfolioTauWorkingDays: metrics.DefaultPortfolioTauWorkingDays,
		},
	})
	if err != nil {
		return nil, err
	}
	return wfJSONMap(exp), nil
}
