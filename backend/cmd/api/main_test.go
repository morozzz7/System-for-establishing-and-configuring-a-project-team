package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func upProbe(name string) func(context.Context) dependencyState {
	return func(context.Context) dependencyState {
		return dependencyState{Name: name, State: stateUp}
	}
}

func downProbe(name string, reason string) func(context.Context) dependencyState {
	return func(context.Context) dependencyState {
		return dependencyState{Name: name, State: stateDown, ReasonCodes: []string{reason}}
	}
}

func decodeReport(t *testing.T, rec *httptest.ResponseRecorder) readinessReport {
	t.Helper()
	var report readinessReport
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&report); err != nil {
		t.Fatalf("decode readiness report: %v", err)
	}
	return report
}

func TestHandleHealthzContractShape(t *testing.T) {
	original := startTime
	startTime = time.Now().Add(-4212*time.Second - 100*time.Millisecond)
	defer func() { startTime = original }()

	rec := httptest.NewRecorder()
	handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode healthz: %v", err)
	}
	wantKeys := []string{"status", "service", "version", "uptime_s"}
	if len(raw) != len(wantKeys) {
		t.Fatalf("healthz keys = %v, want exactly %v", raw, wantKeys)
	}
	for _, key := range wantKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("healthz is missing %q: %s", key, rec.Body.String())
		}
	}
	var status healthStatus
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&status); err != nil {
		t.Fatalf("decode typed healthz: %v", err)
	}
	if status.Status != "ok" || status.Service != "core-api" || status.Version != "1.0.0" || status.UptimeS != 4212 {
		t.Fatalf("healthz = %+v, want ok/core-api/1.0.0/4212", status)
	}
}

func TestReadyzAllDependenciesUp(t *testing.T) {
	handler := newReadinessHandler(upProbe(dependencyPostgres), upProbe(dependencyML))
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	report := decodeReport(t, rec)
	if !report.Ready {
		t.Fatalf("all dependencies up must be ready")
	}
	if len(report.DegradedCapabilities) != 0 {
		t.Fatalf("degraded_capabilities = %v, want empty", report.DegradedCapabilities)
	}
	names := make([]string, 0, len(report.Dependencies))
	for _, dep := range report.Dependencies {
		names = append(names, dep.Name)
		if dep.State != stateUp {
			t.Fatalf("dependency %s = %s, want up", dep.Name, dep.State)
		}
	}
	if !reflect.DeepEqual(names, []string{dependencyPostgres, dependencyML, dependencyTaxonomy}) {
		t.Fatalf("dependencies = %v", names)
	}
}

func TestReadyzMLDownReturns503(t *testing.T) {
	handler := newReadinessHandler(upProbe(dependencyPostgres), downProbe(dependencyML, "ML_UNAVAILABLE"))
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	report := decodeReport(t, rec)
	if report.Ready {
		t.Fatalf("ML down must not be ready")
	}
	if !reflect.DeepEqual(report.DegradedCapabilities, []string{"brief_extraction", "optimization"}) {
		t.Fatalf("degraded_capabilities = %v, want [brief_extraction optimization]", report.DegradedCapabilities)
	}
	ml := report.Dependencies[1]
	if ml.Name != dependencyML || ml.State != stateDown {
		t.Fatalf("ml dependency = %+v, want down", ml)
	}
	if !reflect.DeepEqual(ml.ReasonCodes, []string{"ML_UNAVAILABLE"}) {
		t.Fatalf("ml reason_codes = %v", ml.ReasonCodes)
	}
	if report.Dependencies[0].State != stateUp || report.Dependencies[2].State != stateUp {
		t.Fatalf("unaffected dependencies must stay up: %+v", report.Dependencies)
	}
}

func TestReadyzPostgresDownReturns503(t *testing.T) {
	handler := newReadinessHandler(downProbe(dependencyPostgres, ""), upProbe(dependencyML))
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	report := decodeReport(t, rec)
	if report.Ready || report.Dependencies[0].State != stateDown {
		t.Fatalf("postgres down must be reported: %+v", report)
	}
	if len(report.DegradedCapabilities) != len(postgresCapabilities) {
		t.Fatalf("degraded_capabilities = %v, want every data-backed capability", report.DegradedCapabilities)
	}
}

func TestMLProbeUsesRealHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("probe path = %s, want /healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	state := mlProbe(server.URL)(context.Background())
	if state.Name != dependencyML || state.State != stateUp {
		t.Fatalf("reachable ML = %+v, want up", state)
	}
	if len(state.ReasonCodes) != 0 || state.LatencyMS == nil {
		t.Fatalf("reachable ML must report latency without reason codes: %+v", state)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	state = mlProbe(failing.URL)(context.Background())
	if state.State != stateDown {
		t.Fatalf("ML 500 = %+v, want down", state)
	}

	state = mlProbe("")(context.Background())
	if state.State != stateDown || state.LatencyMS != nil {
		t.Fatalf("unconfigured ML = %+v, want down without latency", state)
	}
}

func TestMLProbeRespectsDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
	defer cancel()
	start := time.Now()
	state := mlProbe(server.URL)(ctx)
	elapsed := time.Since(start)
	if state.State != stateDown {
		t.Fatalf("hanging ML = %+v, want down", state)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("probe took %v, want the 2s deadline", elapsed)
	}
	if !reflect.DeepEqual(state.ReasonCodes, []string{"ML_TIMEOUT"}) {
		t.Fatalf("timeout reason_codes = %v, want [ML_TIMEOUT]", state.ReasonCodes)
	}
}
