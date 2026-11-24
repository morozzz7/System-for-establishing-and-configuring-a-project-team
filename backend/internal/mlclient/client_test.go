package mlclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newServerClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewWithHTTPClient(server.URL, server.Client()), server
}

func fastPolicies(c *Client, timeout time.Duration) {
	c.policies[PathExtract] = endpointPolicy{timeout: timeout, retries: 1, retryMalformed: true}
	c.policies[PathEmbed] = endpointPolicy{timeout: timeout, retries: 1}
	c.policies[PathOptimize] = endpointPolicy{timeout: timeout}
	c.optimizeGrace = 20 * time.Millisecond
}

func noRetryPolicies(c *Client, timeout time.Duration) {
	c.policies[PathExtract] = endpointPolicy{timeout: timeout}
	c.policies[PathEmbed] = endpointPolicy{timeout: timeout}
	c.policies[PathOptimize] = endpointPolicy{timeout: timeout}
	c.optimizeGrace = 20 * time.Millisecond
}

func writeOK(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func writeProblem(t *testing.T, w http.ResponseWriter, status int, code string, retryable bool, reasons ...string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"type":         "https://teamopt.internal/ml/problems/test",
		"title":        "Synthetic ML failure",
		"status":       status,
		"code":         code,
		"detail":       "synthetic ML failure for client tests",
		"request_id":   "req_01HX7Qaa",
		"retryable":    retryable,
		"reason_codes": reasons,
	}); err != nil {
		t.Errorf("write problem: %v", err)
	}
}

func asMlError(t *testing.T, err error) *Error {
	t.Helper()
	var mlErr *Error
	if !errors.As(err, &mlErr) {
		t.Fatalf("error %T (%v) is not *mlclient.Error", err, err)
	}
	return mlErr
}

func TestExtractSuccessAndLegacyMethod(t *testing.T) {
	var calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != PathExtract {
			t.Errorf("path = %s, want %s", r.URL.Path, PathExtract)
		}
		writeOK(t, w, map[string]any{"requirements": []map[string]any{{"id": "req_a"}}})
	})
	var response struct {
		Requirements []map[string]any `json:"requirements"`
	}
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{"brief": "text"}, &response)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if outcome.Calls != 1 || outcome.Status != StatusOK || outcome.Kind != "" || outcome.BreakerOpen {
		t.Fatalf("outcome = %+v, want ok after one call", outcome)
	}
	if outcome.Latency <= 0 {
		t.Fatalf("latency = %s, want positive", outcome.Latency)
	}
	if len(response.Requirements) != 1 || response.Requirements[0]["id"] != "req_a" {
		t.Fatalf("response not decoded: %+v", response)
	}
	if err := c.Extract(context.Background(), map[string]any{}, &response); err != nil {
		t.Fatalf("legacy Extract: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("server calls = %d, want 2", calls.Load())
	}
}

func TestExtractTimeoutRetriesAndClassifies(t *testing.T) {
	var calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-time.After(150 * time.Millisecond):
			writeOK(t, w, map[string]any{})
		case <-r.Context().Done():
		}
	})
	fastPolicies(c, 40*time.Millisecond)

	var response map[string]any
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != 2 || outcome.Calls != 2 {
		t.Fatalf("calls: server=%d outcome=%d, want 2 retries", calls.Load(), outcome.Calls)
	}
	if outcome.Status != StatusTimeout || outcome.Kind != KindTimeout {
		t.Fatalf("outcome = %+v, want timeout/timeout", outcome)
	}
	if mlErr.Kind != KindTimeout || mlErr.Status != http.StatusRequestTimeout || mlErr.Code != "ML_TIMEOUT" {
		t.Fatalf("error = %+v, want client timeout", mlErr)
	}
	if mlErr.Attempts != 2 || mlErr.Endpoint != PathExtract || mlErr.Latency <= 0 {
		t.Fatalf("error metadata = %+v", mlErr)
	}
	if !mlErr.Retryable || !strings.Contains(strings.Join(mlErr.ReasonCodes, ","), "ML_TIMEOUT") {
		t.Fatalf("retryable=%v reason_codes=%v, want ML_TIMEOUT", mlErr.Retryable, mlErr.ReasonCodes)
	}
}

func TestExtractRetries5xxAndPropagatesProblem(t *testing.T) {
	var failAll, calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if failAll.Load() != 0 || n == 1 {
			writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
			return
		}
		writeOK(t, w, map[string]any{"requirements": []any{}})
	})
	fastPolicies(c, time.Second)

	var response map[string]any
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	if err != nil {
		t.Fatalf("retry should recover: %v", err)
	}
	if outcome.Calls != 2 || outcome.Status != StatusDegraded || outcome.Kind != "" {
		t.Fatalf("outcome = %+v, want 2 calls and degraded success", outcome)
	}

	failAll.Store(1)
	calls.Store(0)
	outcome, err = c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != 2 || outcome.Calls != 2 {
		t.Fatalf("calls: server=%d outcome=%d, want 2", calls.Load(), outcome.Calls)
	}
	if outcome.Status != StatusUnavailable || outcome.Kind != KindHTTP5xx {
		t.Fatalf("outcome = %+v, want unavailable/http_5xx", outcome)
	}
	if mlErr.Kind != KindHTTP5xx || mlErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("error = %+v, want 503", mlErr)
	}
	if mlErr.Code != "CAPABILITY_UNAVAILABLE" || mlErr.RequestID != "req_01HX7Qaa" {
		t.Fatalf("problem fields not propagated: %+v", mlErr)
	}
	if len(mlErr.ReasonCodes) != 1 || mlErr.ReasonCodes[0] != "ML_LLM_UNAVAILABLE" {
		t.Fatalf("reason_codes = %v", mlErr.ReasonCodes)
	}
	if !strings.Contains(mlErr.Detail, "synthetic ML failure") || !mlErr.Retryable || mlErr.Attempts != 2 {
		t.Fatalf("error = %+v, want detail/retryable/attempts", mlErr)
	}
}

func TestHTTP408RetriesAndMapsToTimeout(t *testing.T) {
	var failAll, calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if failAll.Load() != 0 || n == 1 {
			writeProblem(t, w, http.StatusRequestTimeout, "TIMEOUT", true, "ML_TIMEOUT")
			return
		}
		writeOK(t, w, map[string]any{"ok": true})
	})
	fastPolicies(c, time.Second)

	var response map[string]any
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	if err != nil || outcome.Status != StatusDegraded || outcome.Calls != 2 {
		t.Fatalf("408 recovery: outcome=%+v err=%v", outcome, err)
	}

	failAll.Store(1)
	calls.Store(0)
	outcome, err = c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != 2 || outcome.Calls != 2 || outcome.Status != StatusTimeout || outcome.Kind != KindHTTP408 {
		t.Fatalf("outcome = %+v server_calls=%d, want two 408 attempts", outcome, calls.Load())
	}
	if mlErr.Kind != KindHTTP408 || mlErr.Status != http.StatusRequestTimeout || !mlErr.Retryable {
		t.Fatalf("error = %+v, want retryable 408", mlErr)
	}
}

func Test422IsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeProblem(t, w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", true, "VALIDATION_FAILED")
	})
	fastPolicies(c, time.Second)

	var response map[string]any
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != 1 || outcome.Calls != 1 {
		t.Fatalf("calls: server=%d outcome=%d, want no retry on 422", calls.Load(), outcome.Calls)
	}
	if outcome.Status != StatusSchemaInvalid || outcome.Kind != KindSchemaInvalid {
		t.Fatalf("outcome = %+v, want schema_invalid", outcome)
	}
	if mlErr.Kind != KindSchemaInvalid || mlErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("error = %+v, want 422", mlErr)
	}
	if mlErr.Code != "VALIDATION_FAILED" || mlErr.Retryable || mlErr.Attempts != 1 {
		t.Fatalf("error = %+v, want non-retryable VALIDATION_FAILED", mlErr)
	}
	for i := 0; i < 6; i++ {
		if _, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response); err == nil {
			t.Fatalf("repeated 422 call %d succeeded", i+1)
		}
	}
	if calls.Load() != 7 {
		t.Fatalf("server calls = %d, want 7 (no retries)", calls.Load())
	}
	if open, _ := c.State(PathExtract); open {
		t.Fatal("client-side 422 failures must not trip the dependency breaker")
	}
}

func TestOther4xxIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeProblem(t, w, http.StatusBadRequest, "BAD_REQUEST", true, "VALIDATION_FAILED")
	})
	fastPolicies(c, time.Second)

	var response map[string]any
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != 1 || outcome.Calls != 1 || outcome.Status != StatusSchemaInvalid {
		t.Fatalf("outcome = %+v server_calls=%d, want no 4xx retry", outcome, calls.Load())
	}
	if mlErr.Kind != KindHTTP4xx || mlErr.Retryable {
		t.Fatalf("error = %+v, want non-retryable 4xx", mlErr)
	}
}

func TestMalformedJSONRetryDependsOnEndpoint(t *testing.T) {
	t.Run("extract retries malformed JSON", func(t *testing.T) {
		var calls atomic.Int32
		c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"requirements":[`))
				return
			}
			writeOK(t, w, map[string]any{"requirements": []any{}})
		})
		fastPolicies(c, time.Second)
		var response map[string]any
		outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
		if err != nil || outcome.Calls != 2 || outcome.Status != StatusDegraded {
			t.Fatalf("outcome=%+v err=%v, want recovery on second call", outcome, err)
		}
	})

	t.Run("embed does not retry malformed JSON", func(t *testing.T) {
		var calls atomic.Int32
		c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, _ = w.Write([]byte(`not json`))
		})
		fastPolicies(c, time.Second)
		var response map[string]any
		outcome, err := c.EmbedWithOutcome(context.Background(), map[string]any{}, &response)
		mlErr := asMlError(t, err)
		if calls.Load() != 1 || outcome.Calls != 1 {
			t.Fatalf("calls: server=%d outcome=%d, want one", calls.Load(), outcome.Calls)
		}
		if outcome.Status != StatusSchemaInvalid || outcome.Kind != KindMalformedJSON {
			t.Fatalf("outcome = %+v, want malformed_json", outcome)
		}
		if mlErr.Kind != KindMalformedJSON || mlErr.Status != http.StatusOK || mlErr.Attempts != 1 {
			t.Fatalf("error = %+v, want malformed 200 response", mlErr)
		}
	})
}

func TestCircuitBreakerOpensAfterFiveFailuresAndShortCircuits(t *testing.T) {
	var calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
	})
	noRetryPolicies(c, time.Second)

	var response map[string]any
	for i := 0; i < 5; i++ {
		outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
		if err == nil {
			t.Fatalf("call %d succeeded, want 503", i+1)
		}
		if i < 4 && outcome.BreakerOpen {
			t.Fatalf("breaker opened after only %d failures", i+1)
		}
	}
	open, eta := c.State(PathExtract)
	if !open || eta <= 0 {
		t.Fatalf("State = %v, %s; want open with positive ETA", open, eta)
	}
	before := calls.Load()
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != before || outcome.Calls != 0 {
		t.Fatalf("short circuit hit server: before=%d after=%d outcome=%+v", before, calls.Load(), outcome)
	}
	if outcome.Status != StatusUnavailable || outcome.Kind != KindCircuitOpen || !outcome.BreakerOpen {
		t.Fatalf("outcome = %+v, want circuit_open/unavailable", outcome)
	}
	if mlErr.Kind != KindCircuitOpen || mlErr.Attempts != 0 || mlErr.RetryAfter <= 0 {
		t.Fatalf("error = %+v, want circuit-open with RetryAfter", mlErr)
	}
	if len(mlErr.ReasonCodes) != 1 || mlErr.ReasonCodes[0] != "ML_UNAVAILABLE" {
		t.Fatalf("reason_codes = %v, want ML_UNAVAILABLE", mlErr.ReasonCodes)
	}
}

func TestCircuitBreakerHalfOpenAllowsOneProbeAndRecovers(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{}, 1)
	var calls atomic.Int32
	var fail, block atomic.Bool
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
			return
		}
		if block.Load() {
			select {
			case arrived <- struct{}{}:
			default:
			}
			<-release
		}
		writeOK(t, w, map[string]any{"ok": true})
	})
	noRetryPolicies(c, 2*time.Second)
	c.breakers[PathExtract].openFor = 30 * time.Millisecond

	var response map[string]any
	fail.Store(true)
	for i := 0; i < 5; i++ {
		if _, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response); err == nil {
			t.Fatalf("call %d succeeded, want 503", i+1)
		}
	}
	fail.Store(false)
	if open, _ := c.State(PathExtract); !open {
		t.Fatal("breaker not open after five failures")
	}
	time.Sleep(40 * time.Millisecond)
	if open, _ := c.State(PathExtract); open {
		t.Fatal("breaker stayed open past openFor")
	}

	block.Store(true)
	type result struct {
		outcome Outcome
		err     error
	}
	probeResult := make(chan result, 1)
	go func() {
		var probeResponse map[string]any
		outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &probeResponse)
		probeResult <- result{outcome, err}
	}()
	select {
	case <-arrived:
	case <-time.After(time.Second):
		t.Fatal("half-open probe did not reach the server")
	}

	second, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	if second.Calls != 0 || second.Kind != KindCircuitOpen || asMlError(t, err).Kind != KindCircuitOpen {
		t.Fatalf("second half-open call = %+v, err=%v, want short circuit", second, err)
	}
	close(release)
	probe := <-probeResult
	if probe.err != nil || probe.outcome.Status != StatusOK || probe.outcome.Calls != 1 {
		t.Fatalf("probe = %+v, err=%v, want successful recovery", probe.outcome, probe.err)
	}
	if open, _ := c.State(PathExtract); open {
		t.Fatal("breaker stayed open after successful probe")
	}
	if calls.Load() != 6 {
		t.Fatalf("server calls = %d, want 5 failures + 1 probe", calls.Load())
	}
	block.Store(false)
	if _, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response); err != nil {
		t.Fatalf("call after recovery: %v", err)
	}
	if calls.Load() != 7 {
		t.Fatalf("server calls = %d, want 7 after recovery", calls.Load())
	}
}

func TestCircuitBreakerFailedProbeReopens(t *testing.T) {
	var mode, calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 0 {
			writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
			return
		}
		writeOK(t, w, map[string]any{})
	})
	noRetryPolicies(c, time.Second)
	c.breakers[PathExtract].openFor = 30 * time.Millisecond

	var response map[string]any
	for i := 0; i < 5; i++ {
		if _, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response); err == nil {
			t.Fatalf("call %d succeeded, want 503", i+1)
		}
	}
	time.Sleep(40 * time.Millisecond)
	outcome, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	if err == nil || outcome.Status != StatusUnavailable {
		t.Fatalf("probe = %+v, err=%v, want failure", outcome, err)
	}
	if open, _ := c.State(PathExtract); !open {
		t.Fatal("failed probe did not reopen the breaker")
	}
	before := calls.Load()
	outcome, err = c.ExtractWithOutcome(context.Background(), map[string]any{}, &response)
	if outcome.Calls != 0 || calls.Load() != before || asMlError(t, err).Kind != KindCircuitOpen {
		t.Fatalf("call after failed probe = %+v err=%v, want short circuit", outcome, err)
	}
}

func TestOutcomeStatusMapping(t *testing.T) {
	type scenario struct {
		name       string
		setup      func(t *testing.T) (*Client, func() (Outcome, error))
		wantStatus string
		wantKind   string
	}
	scenarios := []scenario{
		{
			name: "ok",
			setup: func(t *testing.T) (*Client, func() (Outcome, error)) {
				c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
					writeOK(t, w, map[string]any{})
				})
				return c, func() (Outcome, error) {
					return c.ExtractWithOutcome(context.Background(), map[string]any{}, &map[string]any{})
				}
			},
			wantStatus: StatusOK,
		},
		{
			name: "degraded after retry",
			setup: func(t *testing.T) (*Client, func() (Outcome, error)) {
				var calls atomic.Int32
				c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
					if calls.Add(1) == 1 {
						writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
						return
					}
					writeOK(t, w, map[string]any{})
				})
				fastPolicies(c, time.Second)
				return c, func() (Outcome, error) {
					return c.ExtractWithOutcome(context.Background(), map[string]any{}, &map[string]any{})
				}
			},
			wantStatus: StatusDegraded,
		},
		{
			name: "timeout",
			setup: func(t *testing.T) (*Client, func() (Outcome, error)) {
				c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
					select {
					case <-r.Context().Done():
					case <-time.After(150 * time.Millisecond):
					}
				})
				fastPolicies(c, 30*time.Millisecond)
				return c, func() (Outcome, error) {
					return c.ExtractWithOutcome(context.Background(), map[string]any{}, &map[string]any{})
				}
			},
			wantStatus: StatusTimeout,
			wantKind:   KindTimeout,
		},
		{
			name: "unavailable",
			setup: func(t *testing.T) (*Client, func() (Outcome, error)) {
				c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
					writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
				})
				return c, func() (Outcome, error) {
					var response map[string]any
					return c.OptimizeWithOutcome(context.Background(), map[string]any{"time_limit_ms": 100}, &response)
				}
			},
			wantStatus: StatusUnavailable,
			wantKind:   KindHTTP5xx,
		},
		{
			name: "schema invalid",
			setup: func(t *testing.T) (*Client, func() (Outcome, error)) {
				c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
					writeProblem(t, w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", false, "VALIDATION_FAILED")
				})
				return c, func() (Outcome, error) {
					return c.ExtractWithOutcome(context.Background(), map[string]any{}, &map[string]any{})
				}
			},
			wantStatus: StatusSchemaInvalid,
			wantKind:   KindSchemaInvalid,
		},
		{
			name: "circuit open",
			setup: func(t *testing.T) (*Client, func() (Outcome, error)) {
				c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
					writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
				})
				noRetryPolicies(c, time.Second)
				for i := 0; i < 5; i++ {
					if _, err := c.ExtractWithOutcome(context.Background(), map[string]any{}, &map[string]any{}); err == nil {
						t.Fatalf("trip call %d succeeded", i+1)
					}
				}
				return c, func() (Outcome, error) {
					return c.ExtractWithOutcome(context.Background(), map[string]any{}, &map[string]any{})
				}
			},
			wantStatus: StatusUnavailable,
			wantKind:   KindCircuitOpen,
		},
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			c, call := tc.setup(t)
			outcome, err := call()
			if outcome.Status != tc.wantStatus || outcome.Kind != tc.wantKind {
				t.Fatalf("outcome = %+v, want status=%s kind=%s", outcome, tc.wantStatus, tc.wantKind)
			}
			if (err == nil) != (tc.wantStatus == StatusOK || tc.wantStatus == StatusDegraded) {
				t.Fatalf("err = %v, want err only for failing statuses", err)
			}
			if open, _ := c.State(PathExtract); open && tc.wantStatus != StatusUnavailable {
				t.Fatalf("unexpected open breaker after %s", tc.name)
			}
		})
	}
}

func TestOptimizeTimeoutDerivation(t *testing.T) {
	c := New("http://ml")
	check := func(want time.Duration, body string, opts ...OptimizeOption) {
		t.Helper()
		if got := c.optimizeTimeout([]byte(body), opts); got != want {
			t.Fatalf("optimizeTimeout(%s) = %s, want %s", body, got, want)
		}
	}
	check(9*time.Second, `{"time_limit_ms":7000}`)
	check(12*time.Second, `{}`)
	check(62*time.Second, `{"time_limit_ms":999999}`)
	check(2500*time.Millisecond, `{"time_limit_ms":7000}`, WithOptimizeTimeLimitMS(500))
	check(7050*time.Millisecond, `{"time_limit_ms":7000}`, WithOptimizeGrace(50*time.Millisecond))
}

func TestOptimizeUsesRequestDeadlineAndDoesNotRetry(t *testing.T) {
	var mode, calls atomic.Int32
	c, _ := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 0 {
			writeProblem(t, w, http.StatusServiceUnavailable, "CAPABILITY_UNAVAILABLE", true, "ML_LLM_UNAVAILABLE")
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(150 * time.Millisecond):
		}
	})
	fastPolicies(c, 5*time.Second)

	var response map[string]any
	outcome, err := c.OptimizeWithOutcome(context.Background(), map[string]any{"time_limit_ms": 60}, &response)
	mlErr := asMlError(t, err)
	if calls.Load() != 1 || outcome.Calls != 1 || outcome.Status != StatusUnavailable || outcome.Kind != KindHTTP5xx {
		t.Fatalf("outcome = %+v server_calls=%d, want one 503", outcome, calls.Load())
	}
	if mlErr.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (optimize never retries)", mlErr.Attempts)
	}

	mode.Store(1)
	calls.Store(0)
	start := time.Now()
	outcome, err = c.OptimizeWithOutcome(context.Background(), map[string]any{"time_limit_ms": 60}, &response)
	mlErr = asMlError(t, err)
	elapsed := time.Since(start)
	if calls.Load() != 1 || outcome.Calls != 1 || outcome.Status != StatusTimeout || outcome.Kind != KindTimeout {
		t.Fatalf("outcome = %+v server_calls=%d, want one timeout", outcome, calls.Load())
	}
	if mlErr.Kind != KindTimeout || mlErr.Attempts != 1 || mlErr.Code != "ML_TIMEOUT" {
		t.Fatalf("error = %+v, want timeout without retry", mlErr)
	}
	if elapsed > time.Second {
		t.Fatalf("deadline derived from time_limit_ms was ignored: %s", elapsed)
	}

	calls.Store(0)
	start = time.Now()
	if err := c.Optimize(context.Background(), map[string]any{"time_limit_ms": 60}, &response); err == nil {
		t.Fatal("legacy Optimize should return the derived timeout error")
	}
	if calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("legacy Optimize ignored the deadline: calls=%d elapsed=%s", calls.Load(), time.Since(start))
	}
}
