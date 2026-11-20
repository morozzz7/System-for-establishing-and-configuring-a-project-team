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
