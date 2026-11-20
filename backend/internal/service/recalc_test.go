package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/teamopt/backend/internal/mlclient"
)

// recalcFakeMT is a fake ML service that serves /v1/extract and /v1/optimize for the
// recalculation tests. It returns the same single-variant optimize response every time
// (mirroring frontend/src/mocks/runs.ts "solver proposes, Go verifies").
type recalcFakeML struct {
	extract  string
	optimize string
	last     map[string]any
}

func (f *recalcFakeML) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	var response string
	switch r.URL.Path {
	case "/v1/extract":
		response = f.extract
	case "/v1/optimize":
		f.last = payload
		response = f.optimize
	default:
		return &http.Response{StatusCode: 404, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(response))), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
}

const recalcExtract = `{
  "requirements":[{
    "requirement_id": "req_go01",
    "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-инженер", "match": "exact", "confidence": 0.9},
    "competency": {"competency_id": "lang_go", "name_ru": "Go", "match": "exact", "confidence": 0.9},
    "level": 3, "criticality": "must", "headcount": 1,
    "effort": {"min_person_days": 5, "likely_person_days": 8, "max_person_days": 12, "unit": "person_days"},
    "confidence": 0.8, "needs_review": false,
    "evidence": [{"quote": "Разработать сервис на Go", "start": 0, "end": 24}]
  }],
  "requirements_for_review": [], "diagnostics": {"engine": "rules"}, "warnings": []
}`

const recalcOptimize = `{
  "request_id": "req_run0001",
  "run_id": "run_0001",
  "solver": {"name": "ortools-cp-sat", "version": "9.10", "status": "OPTIMAL", "wall_time_ms": 12,
             "num_workers": 1, "seed": 42, "deterministic": true},
  "variants": [{
    "variant_id": "var_alt1",
    "label": "Вариант A",
    "members": [{"employee_id": "emp_demo0001", "role_id": "backend_engineer",
                 "allocated_hours": 80, "assignment_share": 0.8,
                 "reason_codes": ["INCLUDE_REQUIRED_ROLE"]}],
    "assignments": [
      {"employee_id": "emp_demo0001", "requirement_id": "req_go01", "week": "2026-W10", "hours": 40},
      {"employee_id": "emp_demo0001", "requirement_id": "req_go01", "week": "2026-W11", "hours": 40}
    ],
    "metrics": {},
    "deficits": [],
    "unscheduled_requirements": [],
    "reason_codes": ["FEASIBLE"]
  }],
  "deterministic_fingerprint": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
  "diagnostics": {}, "reason_codes": []
}`

func newRecalcService(t *testing.T) (*WorkflowService, RecalculateRequest, string, map[string]any) {
	t.Helper()
	fake := &recalcFakeML{extract: recalcExtract, optimize: recalcOptimize}
	svc := NewWorkflowService(mlclient.NewWithHTTPClient("http://fixture", &http.Client{Transport: fake}))
	ctx := context.Background()

	// Confirm a brief, then create a parent run.
	brief, err := svc.CreateBrief(ctx, BriefInput{Title: "Synthetic", SourceType: "text", Language: "ru", Text: "Go инженер"})
	if err != nil {
		t.Fatalf("create brief: %v", err)
	}
	if _, ok := svc.ConfirmBrief(brief.BriefID); !ok {
		t.Fatalf("confirm brief")
	}
	parent, err := svc.CreateRun(ctx, brief.BriefID, map[string]any{
		"horizon": map[string]any{"weeks": []any{"2026-W10", "2026-W11"}},
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	parentID, _ := parent["run_id"].(string)

	request := RecalculateRequest{
		BaseVariantID: "var_alt1",
		Overrides: []ConstraintOverride{
			{Type: "force_include", EmployeeID: "emp_demo0001"},
		},
	}
	return svc, request, parentID, parent
}

// TestRecalculateCreatesChildWithLineage asserts a new run with parent_run_id and the
// parent (snapshot + run) left immutable.
func TestRecalculateCreatesChildWithLineage(t *testing.T) {
	svc, request, parentID, parentBefore := newRecalcService(t)
	beforeDigest := svc.snapshots[parentID].SnapshotSHA256()

	response, err := svc.Recalculate(context.Background(), parentID, request)
	if err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	if response["parent_run_id"] != parentID {
		t.Fatalf("parent_run_id = %v, want %s", response["parent_run_id"], parentID)
	}
	newRunID, _ := response["run_id"].(string)
	if newRunID == "" {
		t.Fatalf("new run_id is empty: %v", response)
	}
	childPayload, ok := svc.GetRun(newRunID)
	if !ok {
		t.Fatalf("child run not stored")
	}
	if childPayload["parent_run_id"] != parentID {
		t.Fatalf("stored child parent_run_id = %v, want %s", childPayload["parent_run_id"], parentID)
	}
	// Parent immutability.
	if got := svc.snapshots[parentID].SnapshotSHA256(); got != beforeDigest {
		t.Fatalf("parent snapshot digest changed after recalc: %s != %s", got, beforeDigest)
	}
	if _, ok := svc.GetRun(parentID); !ok {
		t.Fatalf("parent run must still be served")
	}
	_ = parentBefore
	if response["applied_overrides"] != float64(1) {
		t.Fatalf("applied_overrides = %v, want 1", response["applied_overrides"])
	}
	if rej := response["rejected_overrides"].([]any); len(rej) != 0 {
		t.Fatalf("rejected_overrides = %v, want none", rej)
	}
}

// TestRecalculateNoEffect asserts a force_include of the already-included member keeps the
// roster identical and reports RECALC_NO_EFFECT with measured facts.
func TestRecalculateNoEffect(t *testing.T) {
	svc, request, parentID, _ := newRecalcService(t)
	response, err := svc.Recalculate(context.Background(), parentID, request)
	if err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	codes := response["reason_codes"].([]any)
	found := false
	for _, c := range codes {
		if c == "RECALC_NO_EFFECT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected RECALC_NO_EFFECT, got %v", codes)
	}
	facts, _ := response["facts"].(map[string]any)
	if facts["changed_members"] != float64(0) {
		t.Fatalf("changed_members = %v, want 0", facts["changed_members"])
	}
	if facts["roster_jaccard"] != float64(0) {
		t.Fatalf("roster_jaccard = %v, want 0 for identical roster", facts["roster_jaccard"])
	}
	if facts["base_variant_id"] != "var_alt1" || facts["new_variant_id"] != "var_alt1" {
		t.Fatalf("variant ids = %v/%v", facts["base_variant_id"], facts["new_variant_id"])
	}
}

// TestRecalculateRejectsContradictoryOverride asserts individual rejection: after force_include
// is applied, the same employee cannot be force_excluded.
func TestRecalculateRejectsContradictoryOverride(t *testing.T) {
	svc, _, parentID, _ := newRecalcService(t)
	request := RecalculateRequest{
		Overrides: []ConstraintOverride{
			{Type: "force_include", EmployeeID: "emp_demo0001"},
			{Type: "force_exclude", EmployeeID: "emp_demo0001"},
		},
	}
	response, err := svc.Recalculate(context.Background(), parentID, request)
	if err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	rejected := response["rejected_overrides"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected_overrides = %v, want exactly 1", rejected)
	}
	r0 := rejected[0].(map[string]any)
	if r0["index"] != float64(1) {
		t.Fatalf("rejected index = %v, want 1", r0["index"])
	}
	codes := r0["reason_codes"].([]any)
	if len(codes) == 0 || codes[0] != "OVERRIDE_VIOLATES_G4" {
		t.Fatalf("rejected reason_codes = %v, want OVERRIDE_VIOLATES_G4", codes)
	}
	if response["applied_overrides"] != float64(1) {
		t.Fatalf("applied_overrides = %v, want 1", response["applied_overrides"])
	}
}

// TestRecalculateRejectsUnknownEmployee asserts an employee absent from the snapshot is
// rejected (OVERRIDE_REJECTED) and reported in rejected_overrides.
func TestRecalculateRejectsUnknownEmployee(t *testing.T) {
	svc, _, parentID, _ := newRecalcService(t)
	request := RecalculateRequest{
		Overrides: []ConstraintOverride{
			{Type: "force_include", EmployeeID: "emp_ghost01"},
		},
	}
	response, err := svc.Recalculate(context.Background(), parentID, request)
	if err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	rejected := response["rejected_overrides"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected_overrides = %v, want 1", rejected)
	}
	r0 := rejected[0].(map[string]any)
	if codes := r0["reason_codes"].([]any); len(codes) == 0 || codes[0] != "OVERRIDE_REJECTED" {
		t.Fatalf("reason_codes = %v, want OVERRIDE_REJECTED", r0["reason_codes"])
	}
	if response["applied_overrides"] != float64(0) {
		t.Fatalf("applied_overrides = %v, want 0 (no override applied)", response["applied_overrides"])
	}
}

// TestRecalculateDeterminism asserts the same override set yields the same measured effect
// (ignoring the new run_id and timestamps, which are lineage, not math).
func TestRecalculateDeterminism(t *testing.T) {
	svc, request, parentID, _ := newRecalcService(t)
	a, err := svc.Recalculate(context.Background(), parentID, request)
	if err != nil {
		t.Fatalf("recalc a: %v", err)
	}
	svc2, request2, parentID2, _ := newRecalcService(t)
	b, err := svc2.Recalculate(context.Background(), parentID2, request2)
	if err != nil {
		t.Fatalf("recalc b: %v", err)
	}
	for _, key := range []string{"changed_members", "roster_jaccard", "p50_weeks_before", "p50_weeks_after", "seed"} {
		fa, _ := a["facts"].(map[string]any)
		fb, _ := b["facts"].(map[string]any)
		if fa[key] != fb[key] {
			t.Fatalf("facts.%s differs across identical recalc: %v vs %v", key, fa[key], fb[key])
		}
	}
}

// TestRecalculateInvalidRequest asserts empty/missing overrides and a missing parent run.
func TestRecalculateInvalidRequest(t *testing.T) {
	svc, _, parentID, _ := newRecalcService(t)
	if _, err := svc.Recalculate(context.Background(), parentID, RecalculateRequest{}); err == nil {
		t.Fatalf("empty overrides must fail")
	}
	if _, err := svc.Recalculate(context.Background(), "run_nope", RecalculateRequest{Overrides: []ConstraintOverride{{Type: "force_exclude", EmployeeID: "emp_demo0001"}}}); err == nil {
		t.Fatalf("missing parent must fail")
	}
}

// TestGetRunInputsAndVariantAndExplanation exercises the read endpoints.
func TestGetRunInputsAndVariantAndExplanation(t *testing.T) {
	svc, _, parentID, _ := newRecalcService(t)
	inputs, ok := svc.GetRunInputs(parentID)
	if !ok {
		t.Fatalf("GetRunInputs not found for %s", parentID)
	}
	if inputs["candidate_count"] != float64(1) {
		t.Fatalf("candidate_count = %v, want 1", inputs["candidate_count"])
	}
	if _, ok := inputs["snapshot_sha256"]; !ok {
		t.Fatalf("snapshot_sha256 missing")
	}
	variant, ok := svc.GetVariant(parentID, "var_alt1")
	if !ok {
		t.Fatalf("GetVariant not found")
	}
	if members, _ := variant["members"].([]any); len(members) != 1 {
		t.Fatalf("variant members = %v, want 1", variant["members"])
	}
	exp, ok := svc.GetExplanation(parentID, "var_alt1", "ru")
	if !ok {
		t.Fatalf("GetExplanation not found")
	}
	if included, _ := exp["included"].([]any); len(included) != 1 {
		t.Fatalf("explanation included = %v, want 1 member", exp["included"])
	}
	if counter := exp["counterfactuals"].([]any); len(counter) == 0 {
		t.Fatalf("explanation must emit at least one counterfactual")
	}
	if _, ok := svc.GetVariant(parentID, "var_ghost"); ok {
		t.Fatalf("GetVariant must be false for unknown variant")
	}
	if _, ok := svc.GetExplanation(parentID, "var_ghost", "ru"); ok {
		t.Fatalf("GetExplanation must be false for unknown variant")
	}
}
