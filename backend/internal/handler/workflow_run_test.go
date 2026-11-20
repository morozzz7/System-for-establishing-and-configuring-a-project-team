package handler_test

// This test exercises the run pipeline over real HTTP without Docker: chi router +
// WorkflowHandler + a fake ML service. It proves that the served run payload carries the
// authoritative Go metrics and not the solver's advisory estimates.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/handler"
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/mlclient"
	"github.com/teamopt/backend/internal/service"
)

// fakeML is an ML service stub: it answers /v1/extract and /v1/optimize and records the
// optimize request the workflow sent.
type fakeML struct {
	extract  string
	optimize string
	last     map[string]any
}

func (f *fakeML) RoundTrip(r *http.Request) (*http.Response, error) {
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
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte(response))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

const extractFixture = `{
  "requirements": [{
    "requirement_id": "req_go01",
    "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-инженер", "match": "exact", "confidence": 0.9},
    "competency": {"competency_id": "lang_go", "name_ru": "Go", "match": "exact", "confidence": 0.9},
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "effort": {"min_person_days": 5, "likely_person_days": 8, "max_person_days": 12, "unit": "person_days"},
    "assumptions": ["Timeboxed to one increment"],
    "confidence": 0.8,
    "needs_review": false,
    "evidence": [{"quote": "Разработать сервис на Go", "start": 0, "end": 24}]
  }],
  "requirements_for_review": [],
  "diagnostics": {"engine": "rules"},
  "warnings": []
}`

// optimizeFixture proposes one feasible variant. Its `metrics` block is deliberately
// wrong (advisory nonsense): Go must not copy it into the response.
const optimizeFixture = `{
  "request_id": "req_run0001",
  "run_id": "run_0001",
  "solver": {"name": "ortools-cp-sat", "version": "9.10", "status": "OPTIMAL", "wall_time_ms": 17,
             "num_workers": 1, "seed": 42, "deterministic": true},
  "variants": [{
    "variant_id": "var_alt1",
    "label": "Вариант A",
    "members": [{"employee_id": "emp_demo0001", "role_id": "backend_engineer",
                 "allocated_hours": 80, "assignment_share": 0.8,
                 "reason_codes": ["COVERS_MUST_COMPETENCY"]}],
    "assignments": [
      {"employee_id": "emp_demo0001", "requirement_id": "req_go01", "week": "2026-W10", "hours": 40},
      {"employee_id": "emp_demo0001", "requirement_id": "req_go01", "week": "2026-W11", "hours": 40}
    ],
    "metrics": {"M1": 0.01, "M2": 0.02, "M3": 0.03, "M5": 0.04, "M6": 0.05, "M7": 0.06,
                "M8": 0.07, "M9": 0.08, "M10": 0.09, "M11": 0.1, "M12": 0.11, "score": 0.123},
    "deficits": [],
    "unscheduled_requirements": [],
    "active_constraints": [],
    "reason_codes": ["FEASIBLE"]
  }],
  "deterministic_fingerprint": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
  "diagnostics": {"model_version": "test", "weeks": 3, "requirement_count": 1,
                  "candidate_count": 1, "variables": 3, "constraints": 4, "time_limit_ms": 10000},
  "reason_codes": []
}`

// optimizeUnknownMemberFixture proposes a member that is not in the snapshot: the
// independent validator must reject it (G5) and it must never reach the user.
const optimizeUnknownMemberFixture = `{
  "request_id": "req_run0001",
  "run_id": "run_0001",
  "solver": {"name": "ortools-cp-sat", "version": "9.10", "status": "FEASIBLE", "wall_time_ms": 9,
             "num_workers": 1, "seed": 42, "deterministic": true},
  "variants": [{
    "variant_id": "var_ghost",
    "label": "Призрак",
    "members": [{"employee_id": "emp_ghost01", "role_id": "backend_engineer",
                 "allocated_hours": 80, "assignment_share": 1, "reason_codes": ["FEASIBLE"]}],
    "assignments": [
      {"employee_id": "emp_ghost01", "requirement_id": "req_go01", "week": "2026-W10", "hours": 40},
      {"employee_id": "emp_ghost01", "requirement_id": "req_go01", "week": "2026-W11", "hours": 40}
    ],
    "metrics": {"M1": 1, "score": 1},
    "deficits": [],
    "unscheduled_requirements": [],
    "active_constraints": [],
    "reason_codes": ["FEASIBLE"]
  }],
  "deterministic_fingerprint": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
  "diagnostics": {"model_version": "test", "weeks": 3, "requirement_count": 1,
                  "candidate_count": 1, "variables": 3, "constraints": 4, "time_limit_ms": 10000},
  "reason_codes": []
}`

func newWorkflowServer(t *testing.T, fake *fakeML) *httptest.Server {
	t.Helper()
	client := mlclient.NewWithHTTPClient("http://fixture", &http.Client{Transport: fake})
	workflow := service.NewWorkflowService(client)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		(&handler.WorkflowHandler{Svc: workflow}).Register(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

func callJSON(t *testing.T, server *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, string(raw), err)
		}
	}
	return resp.StatusCode, decoded
}

func createBrief(t *testing.T, server *httptest.Server) string {
	t.Helper()
	status, brief := callJSON(t, server, http.MethodPost, "/api/v1/briefs", map[string]any{
		"title": "Синтетический бриф", "source_type": "text", "language": "ru",
		"text": "Нужен Go-инженер на сервис", "requested_start_week": "2026-W10",
	})
	if status != http.StatusAccepted {
		t.Fatalf("POST /briefs = %d, want 202 (%v)", status, brief)
	}
	briefID, _ := brief["brief_id"].(string)
	if briefID == "" {
		t.Fatalf("brief_id is missing in %v", brief)
	}
	return briefID
}

func TestRunEndpointServesGoMetrics(t *testing.T) {
	fake := &fakeML{extract: extractFixture, optimize: optimizeFixture}
	server := newWorkflowServer(t, fake)
	briefID := createBrief(t, server)

	status, run := callJSON(t, server, http.MethodPost, "/api/v1/runs", map[string]any{"brief_id": briefID})
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs = %d, want 202 (%v)", status, run)
	}
	runID, _ := run["run_id"].(string)
	if runID == "" {
		t.Fatalf("run_id is missing in %v", run)
	}

	// The optimize request is derived from the snapshot, not from the HTTP body.
	if fake.last == nil {
		t.Fatalf("the workflow never called the solver")
	}
	if got := len(fake.last["candidates"].([]any)); got != 1 {
		t.Fatalf("solver candidates = %d, want 1 (one demo candidate per requirement)", got)
	}
	var weightSum float64
	for _, value := range fake.last["weights"].(map[string]any) {
		weightSum += value.(float64)
	}
	if diff := weightSum - 1; diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("solver weights sum to %v, want 1", weightSum)
	}
	requirement := fake.last["requirements"].([]any)[0].(map[string]any)
	effort := requirement["effort"].(map[string]any)
	if effort["unit"] != "person_days" {
		t.Fatalf("effort unit = %v, want person_days", effort["unit"])
	}

	variants, _ := run["variants"].([]any)
	if len(variants) != 1 {
		t.Fatalf("variants = %v, want exactly 1 served variant", run["variants"])
	}
	variant := variants[0].(map[string]any)
	if _, leaked := variant["metrics"]; leaked {
		t.Fatalf("the solver's advisory metrics leaked into the run payload: %v", variant["metrics"])
	}
	values, ok := variant["metric_values"].(map[string]any)
	if !ok {
		t.Fatalf("metric_values is missing: %v", variant)
	}
	for _, key := range metrics.Keys {
		value, present := values[key]
		if key == "M4" && !present {
			continue // no project history in the demo snapshot: M4 is dropped
		}
		if !present {
			t.Fatalf("metric_values[%s] is missing: %v", key, values)
		}
		number, ok := value.(float64)
		if !ok || number < 0 || number > 1 {
			t.Fatalf("metric_values[%s] = %v is not a probability", key, value)
		}
	}
	if values["score"] == 0.123 {
		t.Fatalf("score still carries the solver's advisory value: %v", values["score"])
	}
	if _, present := variant["metric_details"]; !present {
		t.Fatalf("metric_details is missing: %v", variant)
	}
	if variant["gate_pass"] != true {
		t.Fatalf("the feasible variant must pass the gate: %v", variant["gate"])
	}
	duration, _ := variant["duration"].(map[string]any)
	mc, _ := duration["monte_carlo"].(map[string]any)
	if mc["runs"] != float64(metrics.DefaultMonteCarloRuns) {
		t.Fatalf("monte_carlo.runs = %v, want %d", mc["runs"], metrics.DefaultMonteCarloRuns)
	}
	if _, present := duration["p50_weeks"]; !present {
		t.Fatalf("duration is missing p50_weeks: %v", duration)
	}
	if _, present := variant["cost_units"]; !present {
		t.Fatalf("cost_units is missing: %v", variant)
	}

	diagnostics, _ := run["diagnostics"].(map[string]any)
	if diagnostics["metrics_version"] != metrics.Version {
		t.Fatalf("metrics_version = %v, want %s", diagnostics["metrics_version"], metrics.Version)
	}
	if diagnostics["validator_rejected"] != float64(0) {
		t.Fatalf("validator_rejected = %v, want 0", diagnostics["validator_rejected"])
	}
	if diagnostics["solver_status"] != "OPTIMAL" {
		t.Fatalf("solver_status = %v, want OPTIMAL", diagnostics["solver_status"])
	}
	if _, present := run["variant_set"]; !present {
		t.Fatalf("variant_set is missing: %v", run)
	}

	// GET returns the stored payload unchanged (no recomputation drift).
	status, fetched := callJSON(t, server, http.MethodGet, "/api/v1/runs/"+runID, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /runs/%s = %d, want 200", runID, status)
	}
	if fetched["run_id"] != runID {
		t.Fatalf("GET run_id = %v, want %s", fetched["run_id"], runID)
	}
	fetchedVariants, _ := fetched["variants"].([]any)
	if len(fetchedVariants) != 1 {
		t.Fatalf("GET variants = %v, want 1", fetched["variants"])
	}
	fetchedValues := fetchedVariants[0].(map[string]any)["metric_values"].(map[string]any)
	if fetchedValues["score"] != values["score"] || fetchedValues["M1"] != values["M1"] {
		t.Fatalf("GET metric_values = %v, want %v", fetchedValues, values)
	}
}

func TestRunEndpointDropsValidatorRejectedVariants(t *testing.T) {
	fake := &fakeML{extract: extractFixture, optimize: optimizeUnknownMemberFixture}
	server := newWorkflowServer(t, fake)
	briefID := createBrief(t, server)

	status, run := callJSON(t, server, http.MethodPost, "/api/v1/runs", map[string]any{"brief_id": briefID})
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs = %d, want 202 (%v)", status, run)
	}
	if variants, _ := run["variants"].([]any); len(variants) != 0 {
		t.Fatalf("a variant with an unknown employee must not reach the user: %v", variants)
	}
	diagnostics, _ := run["diagnostics"].(map[string]any)
	if diagnostics["validator_rejected"] != float64(1) {
		t.Fatalf("validator_rejected = %v, want 1", diagnostics["validator_rejected"])
	}
	if run["status"] != "failed" {
		t.Fatalf("status = %v, want failed when no variant survives", run["status"])
	}
}

func TestRunEndpointRejectsInvalidConstraints(t *testing.T) {
	fake := &fakeML{extract: extractFixture, optimize: optimizeFixture}
	server := newWorkflowServer(t, fake)
	briefID := createBrief(t, server)

	// Weights that do not cover the solver objective are a client error, not a solver one.
	status, problem := callJSON(t, server, http.MethodPost, "/api/v1/runs", map[string]any{
		"brief_id": briefID,
		"weights":  map[string]any{"M1": 1.0},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("POST /runs = %d, want 400 (%v)", status, problem)
	}
	if problem["code"] != "BAD_REQUEST" {
		t.Fatalf("problem code = %v, want BAD_REQUEST", problem["code"])
	}
	if fake.last != nil {
		t.Fatalf("an invalid run must not reach the solver")
	}
}
