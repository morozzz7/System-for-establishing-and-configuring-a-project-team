package handler_test

// HTTP tests for the recalculation + read endpoints (ADR-007 §3): GET /runs/{id}/inputs,
// GET /runs/{id}/variants/{vid}, GET /runs/{id}/variants/{vid}/explanation and
// POST /runs/{id}/recalculate, over real chi routing with a fake ML service.

import (
	"net/http"
	"testing"
)

// TestRecalcEndpointsHappyPath exercises inputs/variant/explanation/inputs and a recalc
// that reports RECALC_NO_EFFECT with lineage.
func TestRecalcEndpointsHappyPath(t *testing.T) {
	fake := &fakeML{extract: extractFixture, optimize: optimizeFixture}
	server := newWorkflowServer(t, fake)
	briefID := createBrief(t, server)

	status, run := callJSON(t, server, http.MethodPost, "/api/v1/runs", map[string]any{"brief_id": briefID})
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs = %d, want 202 (%v)", status, run)
	}
	runID, _ := run["run_id"].(string)

	// GET inputs.
	status, inputs := callJSON(t, server, http.MethodGet, "/api/v1/runs/"+runID+"/inputs", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /inputs = %d, want 200", status)
	}
	if inputs["candidate_count"] != float64(1) {
		t.Fatalf("inputs.candidate_count = %v, want 1", inputs["candidate_count"])
	}
	if _, ok := inputs["constraints"].(map[string]any); !ok {
		t.Fatalf("inputs.constraints missing: %v", inputs)
	}
	if _, ok := inputs["snapshot_sha256"].(string); !ok {
		t.Fatalf("inputs.snapshot_sha256 missing")
	}

	// GET variant.
	status, variant := callJSON(t, server, http.MethodGet, "/api/v1/runs/"+runID+"/variants/var_alt1", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /variants/var_alt1 = %d, want 200", status)
	}
	if members, _ := variant["members"].([]any); len(members) != 1 {
		t.Fatalf("variant.members = %v, want 1", variant["members"])
	}
	if _, ok := variant["weekly_assignments"].([]any); !ok {
		t.Fatalf("variant.weekly_assignments missing")
	}
	if _, ok := variant["active_constraints"].([]any); !ok {
		t.Fatalf("variant.active_constraints missing")
	}

	// GET explanation.
	status, explanation := callJSON(t, server, http.MethodGet, "/api/v1/runs/"+runID+"/variants/var_alt1/explanation?locale=ru", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /explanation = %d, want 200", status)
	}
	if included, _ := explanation["included"].([]any); len(included) != 1 {
		t.Fatalf("explanation.included = %v, want 1", explanation["included"])
	}
	if cf, _ := explanation["counterfactuals"].([]any); len(cf) == 0 {
		t.Fatalf("explanation.counterfactuals must be non-empty")
	}

	// POST recalculate with force_include of the already-included member -> no effect but
	// a new run with lineage.
	status, resp := callJSON(t, server, http.MethodPost, "/api/v1/runs/"+runID+"/recalculate", map[string]any{
		"base_variant_id": "var_alt1",
		"overrides":       []any{map[string]any{"type": "force_include", "employee_id": "emp_demo0001"}},
	})
	if status != http.StatusOK {
		t.Fatalf("POST /recalculate = %d, want 200 (%v)", status, resp)
	}
	if resp["parent_run_id"] != runID {
		t.Fatalf("parent_run_id = %v, want %s", resp["parent_run_id"], runID)
	}
	if resp["applied_overrides"] != float64(1) {
		t.Fatalf("applied_overrides = %v, want 1", resp["applied_overrides"])
	}
	codes, _ := resp["reason_codes"].([]any)
	hasNoEffect := false
	for _, c := range codes {
		if c == "RECALC_NO_EFFECT" {
			hasNoEffect = true
		}
	}
	if !hasNoEffect {
		t.Fatalf("reason_codes = %v, want RECALC_NO_EFFECT", codes)
	}
	newRunID, _ := resp["run_id"].(string)
	if newRunID == "" || newRunID == runID {
		t.Fatalf("new run_id must differ from parent: %v", resp["run_id"])
	}
	// The child is a real run and its payload records the lineage.
	status, child := callJSON(t, server, http.MethodGet, "/api/v1/runs/"+newRunID, nil)
	if status != http.StatusOK || child["parent_run_id"] != runID {
		t.Fatalf("child run lineage missing: status=%d child=%v", status, child)
	}
}

// TestRecalcEndpointsRejectedOverride asserts an employee absent from the snapshot is
// rejected individually while the run still succeeds.
func TestRecalcEndpointsRejectedOverride(t *testing.T) {
	fake := &fakeML{extract: extractFixture, optimize: optimizeFixture}
	server := newWorkflowServer(t, fake)
	briefID := createBrief(t, server)

	status, run := callJSON(t, server, http.MethodPost, "/api/v1/runs", map[string]any{"brief_id": briefID})
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs = %d, want 202", status)
	}
	runID, _ := run["run_id"].(string)

	status, resp := callJSON(t, server, http.MethodPost, "/api/v1/runs/"+runID+"/recalculate", map[string]any{
		"overrides": []any{map[string]any{"type": "force_include", "employee_id": "emp_ghost01"}},
	})
	if status != http.StatusOK {
		t.Fatalf("POST /recalculate = %d, want 200 (%v)", status, resp)
	}
	rejected, _ := resp["rejected_overrides"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected_overrides = %v, want 1", rejected)
	}
	r0 := rejected[0].(map[string]any)
	if codes, _ := r0["reason_codes"].([]any); len(codes) == 0 || codes[0] != "OVERRIDE_REJECTED" {
		t.Fatalf("rejected reason_codes = %v, want OVERRIDE_REJECTED", r0["reason_codes"])
	}
	if resp["applied_overrides"] != float64(0) {
		t.Fatalf("applied_overrides = %v, want 0", resp["applied_overrides"])
	}
}

// TestRecalcEndpointsErrors asserts 404s and the 400 for empty overrides.
func TestRecalcEndpointsErrors(t *testing.T) {
	fake := &fakeML{extract: extractFixture, optimize: optimizeFixture}
	server := newWorkflowServer(t, fake)
	briefID := createBrief(t, server)

	status, run := callJSON(t, server, http.MethodPost, "/api/v1/runs", map[string]any{"brief_id": briefID})
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs = %d, want 202", status)
	}
	runID, _ := run["run_id"].(string)

	if status, _ := callJSON(t, server, http.MethodGet, "/api/v1/runs/run_nope/inputs", nil); status != http.StatusNotFound {
		t.Fatalf("GET /inputs(nonexistent) = %d, want 404", status)
	}
	if status, _ := callJSON(t, server, http.MethodGet, "/api/v1/runs/"+runID+"/variants/var_ghost", nil); status != http.StatusNotFound {
		t.Fatalf("GET /variants/var_ghost = %d, want 404", status)
	}
	if status, _ := callJSON(t, server, http.MethodPost, "/api/v1/runs/run_nope/recalculate", map[string]any{
		"overrides": []any{map[string]any{"type": "force_exclude", "employee_id": "emp_demo0001"}},
	}); status != http.StatusNotFound {
		t.Fatalf("POST /recalculate(nonexistent) = %d, want 404", status)
	}
	if status, _ := callJSON(t, server, http.MethodPost, "/api/v1/runs/"+runID+"/recalculate", map[string]any{
		"overrides": []any{},
	}); status != http.StatusBadRequest {
		t.Fatalf("POST /recalculate(empty) = %d, want 400", status)
	}
}
