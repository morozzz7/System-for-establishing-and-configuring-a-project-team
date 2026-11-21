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

func TestWorkflowExtractsAndStoresBrief(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/extract" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, _ := json.Marshal(map[string]any{"requirements": []any{}, "requirements_for_review": []any{}, "diagnostics": map[string]any{"model": "fixture"}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})
	svc := NewWorkflowService(mlclient.NewWithHTTPClient("http://fixture", &http.Client{Transport: transport}))
	brief, err := svc.CreateBrief(context.Background(), BriefInput{Title: "Synthetic brief", SourceType: "text", Language: "ru", Text: "Нужен Go инженер"})
	if err != nil {
		t.Fatal(err)
	}
	if brief.Status != "extracted" || brief.BriefID == "" {
		t.Fatalf("unexpected brief: %#v", brief)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHorizonWeeks(t *testing.T) {
	// 1. Default when horizon is omitted
	def, err := horizonWeeks(map[string]any{})
	if err != nil || len(def) != 3 {
		t.Fatalf("expected 3 default weeks, got %v (err=%v)", def, err)
	}

	// 2. Horizon with start_week and end_week
	rangeWeeks, err := horizonWeeks(map[string]any{
		"horizon": map[string]any{
			"start_week": "2026-W10",
			"end_week":   "2026-W12",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"2026-W10", "2026-W11", "2026-W12"}
	if len(rangeWeeks) != len(expected) {
		t.Fatalf("expected %d weeks, got %d", len(expected), len(rangeWeeks))
	}
	for i, w := range expected {
		if rangeWeeks[i] != w {
			t.Errorf("at index %d: expected %s, got %s", i, w, rangeWeeks[i])
		}
	}

	// 3. Horizon with explicit weeks array
	explicitWeeks, err := horizonWeeks(map[string]any{
		"horizon": map[string]any{
			"weeks": []any{"2026-W05", "2026-W06"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(explicitWeeks) != 2 || explicitWeeks[0] != "2026-W05" || explicitWeeks[1] != "2026-W06" {
		t.Fatalf("unexpected explicit weeks: %v", explicitWeeks)
	}
}
