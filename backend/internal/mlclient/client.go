// Package mlclient contains the contract-shaped HTTP client for the ML service.
package mlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Endpoint paths are also the keys accepted by State.
const (
	PathExtract  = "/v1/extract"
	PathEmbed    = "/v1/embed"
	PathOptimize = "/v1/optimize"
)

// Outcome.Status values from the MlStatus enum in contracts/openapi.yaml.
const (
	StatusOK            = "ok"
	StatusDegraded      = "degraded"
	StatusTimeout       = "timeout"
	StatusUnavailable   = "unavailable"
	StatusSchemaInvalid = "schema_invalid"
)

// Outcome.Kind and Error.Kind values. The first six are the ADR-004 failure
// classes; http_4xx, transport_error and canceled cover failures that do not
// fit those classes while keeping the wire-level cause visible.
const (
	KindTimeout        = "timeout"
	KindHTTP5xx        = "http_5xx"
	KindHTTP408        = "http_408"
	KindMalformedJSON  = "malformed_json"
	KindSchemaInvalid  = "schema_invalid"
	KindCircuitOpen    = "circuit_open"
	KindHTTP4xx        = "http_4xx"
	KindTransportError = "transport_error"
	KindCanceled       = "canceled"
)

const (
	maxResponseBytes     = 8 << 20
	defaultTimeLimitMS   = 10000 // contracts/reference.yaml solver.default_time_limit_ms
	minTimeLimitMS       = 100   // contracts/json-schema/optimize-request.schema.json
	defaultGrace         = 2 * time.Second
	globalRequestCeiling = 65 * time.Second
)

// Outcome is the machine-readable summary of one ML call. Status never reports
// ok when err != nil; a successful call that needed a retry is reported as
// degraded so the UI can show a degradation badge without guessing.
type Outcome struct {
	Calls       int
	Status      string
	Kind        string
	Latency     time.Duration
	BreakerOpen bool
}

// Error is the typed failure returned by every client method. Use errors.As to
// access it; Status is the HTTP status seen on the wire (0 for local failures).
// Retryable describes the failure class (never true for 422 or other 4xx),
// while Attempts shows how many calls the endpoint policy actually made.
type Error struct {
	Status      int
	Code        string
	ReasonCodes []string
	Retryable   bool
	Attempts    int
	Endpoint    string
	Latency     time.Duration
	Kind        string
	Detail      string
	RequestID   string
	RetryAfter  time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("ml %s: %s (status=%d kind=%s code=%s attempts=%d latency=%s reason_codes=%v)",
		e.Endpoint, e.Detail, e.Status, e.Kind, e.Code, e.Attempts, e.Latency, e.ReasonCodes)
}

// problem mirrors the RFC-9457 body returned by the ML service (ml-openapi.yaml).
type problem struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Status      int      `json:"status"`
	Code        string   `json:"code"`
	Detail      string   `json:"detail"`
	RequestID   string   `json:"request_id"`
	Retryable   bool     `json:"retryable"`
	ReasonCodes []string `json:"reason_codes"`
}

type endpointPolicy struct {
	timeout        time.Duration
	retries        int
	retryMalformed bool
}

type OptimizeOption func(*optimizeCall)

type optimizeCall struct {
	timeLimitMS int
	grace       time.Duration
}

// WithOptimizeTimeLimitMS overrides the value used to derive the optimization
// deadline. It does not modify the request body; use it when the caller knows
// the effective limit but the request is an opaque map.
func WithOptimizeTimeLimitMS(ms int) OptimizeOption {
	return func(call *optimizeCall) { call.timeLimitMS = ms }
}

// WithOptimizeGrace overrides the 2 s grace added to time_limit_ms.
func WithOptimizeGrace(grace time.Duration) OptimizeOption {
	return func(call *optimizeCall) { call.grace = grace }
}

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// endpointBreaker implements the ADR-004 policy: five consecutive failures in
// 30 s open the breaker for 60 s, followed by a single half-open probe.
type endpointBreaker struct {
	mu            sync.Mutex
	state         breakerState
	failures      []time.Time
	openedAt      time.Time
	probeInFlight bool
	threshold     int
	window        time.Duration
	openFor       time.Duration
}

func newEndpointBreaker() *endpointBreaker {
	return &endpointBreaker{threshold: 5, window: 30 * time.Second, openFor: 60 * time.Second}
}

func (b *endpointBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if time.Since(b.openedAt) >= b.openFor {
			b.state = breakerHalfOpen
			b.probeInFlight = true
			return true
		}
		return false
	default:
		if b.probeInFlight {
			return false
		}
		b.probeInFlight = true
		return true
	}
}

func (b *endpointBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = nil
	b.openedAt = time.Time{}
	b.probeInFlight = false
	b.state = breakerClosed
}

func (b *endpointBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.trip()
		return
	}
	now := time.Now()
	b.failures = append(b.failures, now)
	cutoff := now.Add(-b.window)
	kept := b.failures[:0]
	for _, at := range b.failures {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	b.failures = kept
	if len(b.failures) >= b.threshold {
		b.trip()
	}
}

func (b *endpointBreaker) trip() {
	b.state = breakerOpen
	b.openedAt = time.Now()
	b.failures = nil
	b.probeInFlight = false
}

func (b *endpointBreaker) status() (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != breakerOpen {
		return false, 0
	}
	eta := b.openFor - time.Since(b.openedAt)
	if eta <= 0 {
		return false, 0
	}
	return true, eta
}

// Client applies ADR-004 per-endpoint timeouts, retries and circuit breakers
// on top of the contract-shaped POST calls.
type Client struct {
	baseURL        string
	httpClient     *http.Client
	policies       map[string]endpointPolicy
	breakers       map[string]*endpointBreaker
	optimizeGrace  time.Duration
	maxTimeLimitMS int
}

func New(baseURL string) *Client {
	return newClient(baseURL, &http.Client{Timeout: globalRequestCeiling})
}

// NewWithHTTPClient is used by deterministic unit tests and local adapters.
func NewWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	return newClient(baseURL, httpClient)
}

func newClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: globalRequestCeiling}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		policies: map[string]endpointPolicy{
			PathExtract:  {timeout: 20 * time.Second, retries: 1, retryMalformed: true},
			PathEmbed:    {timeout: 5 * time.Second, retries: 1},
			PathOptimize: {timeout: defaultTimeLimitMS*time.Millisecond + defaultGrace},
		},
		breakers: map[string]*endpointBreaker{
			PathExtract:  newEndpointBreaker(),
			PathEmbed:    newEndpointBreaker(),
			PathOptimize: newEndpointBreaker(),
		},
		optimizeGrace:  defaultGrace,
		maxTimeLimitMS: 60000,
	}
}

func (c *Client) call(ctx context.Context, path string, request, response any, opts []OptimizeOption) (Outcome, error) {
	start := time.Now()
	policy := c.policies[path]
	body, err := json.Marshal(request)
	if err != nil {
		mlErr := &Error{Endpoint: path, Kind: KindSchemaInvalid, Detail: err.Error(), Latency: time.Since(start)}
		return withLatency(Outcome{Calls: 0, Status: StatusSchemaInvalid, Kind: KindSchemaInvalid}, start), mlErr
	}

	timeout := policy.timeout
	if path == PathOptimize {
		timeout = c.optimizeTimeout(body, opts)
	}

	breakers := c.breakers[path]
	if !breakers.allow() {
		_, eta := breakers.status()
		mlErr := &Error{
			Endpoint: path, Kind: KindCircuitOpen, Status: http.StatusServiceUnavailable,
			Code: "CAPABILITY_UNAVAILABLE", ReasonCodes: []string{"ML_UNAVAILABLE"},
			Retryable: true, RetryAfter: eta, Detail: "circuit breaker open",
		}
		outcome := Outcome{Calls: 0, Status: StatusUnavailable, Kind: KindCircuitOpen, BreakerOpen: true}
		return withLatency(outcome, start), mlErr
	}

	var last *Error
	made := 0
	for attempt := 0; attempt <= policy.retries; attempt++ {
		if attempt > 0 && ctx.Err() != nil {
			break
		}
		made = attempt + 1
		last = c.attempt(ctx, path, body, timeout, response)
		if last == nil {
			breakers.recordSuccess()
			status := StatusOK
			if made > 1 {
				status = StatusDegraded
			}
			open, _ := breakers.status()
			outcome := Outcome{Calls: made, Status: status, BreakerOpen: open}
			return withLatency(outcome, start), nil
		}
		// Client-side errors (422 and other 4xx) prove the dependency is alive,
		// so they break the consecutive-failure chain instead of tripping it.
		if retryableKind(last.Kind) {
			breakers.recordFailure()
		} else {
			breakers.recordSuccess()
		}
		if attempt == policy.retries || !retryable(last, policy) || ctx.Err() != nil {
			break
		}
	}

	last.Attempts = made
	last.Latency = time.Since(start)
	if last.Kind != KindCircuitOpen {
		last.Retryable = retryableKind(last.Kind)
	}
	open, _ := breakers.status()
	outcome := Outcome{Calls: made, Status: statusForKind(last.Kind), Kind: last.Kind, BreakerOpen: open}
	return withLatency(outcome, start), last
}

func (c *Client) attempt(ctx context.Context, path string, body []byte, timeout time.Duration, response any) *Error {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return &Error{Endpoint: path, Kind: KindTransportError, Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, application/problem+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return classifyTransportError(path, attemptCtx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return classifyTransportError(path, attemptCtx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpError(path, resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, response); err != nil {
		return &Error{Endpoint: path, Status: resp.StatusCode, Kind: KindMalformedJSON, Detail: err.Error()}
	}
	return nil
}

func (c *Client) optimizeTimeout(body []byte, opts []OptimizeOption) time.Duration {
	call := optimizeCall{grace: c.optimizeGrace}
	for _, opt := range opts {
		if opt != nil {
			opt(&call)
		}
	}
	ms := call.timeLimitMS
	if ms <= 0 {
		ms = timeLimitMSFrom(body)
	}
	if ms < minTimeLimitMS {
		ms = minTimeLimitMS
	}
	if ms > c.maxTimeLimitMS {
		ms = c.maxTimeLimitMS
	}
	if call.grace < 0 {
		call.grace = 0
	}
	return time.Duration(ms)*time.Millisecond + call.grace
}

func timeLimitMSFrom(body []byte) int {
	var value struct {
		TimeLimitMS int `json:"time_limit_ms"`
	}
	if err := json.Unmarshal(body, &value); err != nil || value.TimeLimitMS <= 0 {
		return defaultTimeLimitMS
	}
	return value.TimeLimitMS
}

func classifyTransportError(path string, ctx context.Context, err error) *Error {
	mlErr := &Error{Endpoint: path, Kind: KindTransportError, Detail: err.Error()}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		mlErr.Kind = KindTimeout
		mlErr.Status = http.StatusRequestTimeout
		mlErr.Code = "ML_TIMEOUT"
		mlErr.ReasonCodes = []string{"ML_TIMEOUT"}
		mlErr.Retryable = true
		return mlErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		mlErr.Kind = KindCanceled
		return mlErr
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		mlErr.Kind = KindTimeout
		mlErr.Status = http.StatusRequestTimeout
		mlErr.Code = "ML_TIMEOUT"
		mlErr.ReasonCodes = []string{"ML_TIMEOUT"}
	}
	mlErr.Retryable = true
	return mlErr
}

func httpError(path string, status int, data []byte) *Error {
	mlErr := &Error{Endpoint: path, Status: status}
	var p problem
	if json.Unmarshal(data, &p) == nil {
		mlErr.Code = p.Code
		mlErr.Detail = p.Detail
		mlErr.RequestID = p.RequestID
		mlErr.ReasonCodes = append([]string(nil), p.ReasonCodes...)
		mlErr.Retryable = p.Retryable
	}
	switch {
	case status == http.StatusRequestTimeout:
		mlErr.Kind = KindHTTP408
		mlErr.Retryable = true
		if mlErr.Code == "" {
			mlErr.Code = "TIMEOUT"
		}
		if len(mlErr.ReasonCodes) == 0 {
			mlErr.ReasonCodes = []string{"ML_TIMEOUT"}
		}
	case status >= 500:
		mlErr.Kind = KindHTTP5xx
		mlErr.Retryable = true
	case status == http.StatusUnprocessableEntity:
		mlErr.Kind = KindSchemaInvalid
		mlErr.Retryable = false
	default:
		mlErr.Kind = KindHTTP4xx
		mlErr.Retryable = false
	}
	if mlErr.Detail == "" {
		mlErr.Detail = http.StatusText(status)
	}
	return mlErr
}

// retryableKind reports whether the failure class may be retried at all.
// Error.Retryable exposes it to callers; retryable also applies the endpoint policy.
func retryableKind(kind string) bool {
	switch kind {
	case KindTimeout, KindHTTP408, KindHTTP5xx, KindMalformedJSON, KindTransportError:
		return true
	default:
		return false
	}
}

func retryable(mlErr *Error, policy endpointPolicy) bool {
	if mlErr == nil || policy.retries == 0 {
		return false
	}
	if mlErr.Kind == KindMalformedJSON {
		return policy.retryMalformed
	}
	return retryableKind(mlErr.Kind)
}

func statusForKind(kind string) string {
	switch kind {
	case KindTimeout, KindHTTP408:
		return StatusTimeout
	case KindMalformedJSON, KindSchemaInvalid, KindHTTP4xx:
		return StatusSchemaInvalid
	default:
		return StatusUnavailable
	}
}

func withLatency(outcome Outcome, start time.Time) Outcome {
	outcome.Latency = time.Since(start)
	return outcome
}

// State reports whether the endpoint's breaker is open and how long remains
// until the next half-open probe is allowed.
func (c *Client) State(path string) (bool, time.Duration) {
	if breaker := c.breakers[path]; breaker != nil {
		return breaker.status()
	}
	return false, 0
}

// Extract calls /v1/extract with the ADR-004 timeout and retry policy.
func (c *Client) Extract(ctx context.Context, request, response any) error {
	_, err := c.ExtractWithOutcome(ctx, request, response)
	return err
}

// ExtractWithOutcome fills response in place and also returns the call summary
// for run/brief diagnostics.
func (c *Client) ExtractWithOutcome(ctx context.Context, request, response any) (Outcome, error) {
	return c.call(ctx, PathExtract, request, response, nil)
}

// Embed calls /v1/embed with the ADR-004 timeout and retry policy.
func (c *Client) Embed(ctx context.Context, request, response any) error {
	_, err := c.EmbedWithOutcome(ctx, request, response)
	return err
}

// EmbedWithOutcome fills response in place and also returns the call summary.
func (c *Client) EmbedWithOutcome(ctx context.Context, request, response any) (Outcome, error) {
	return c.call(ctx, PathEmbed, request, response, nil)
}

// Optimize calls /v1/optimize without retries and with a deadline derived from
// time_limit_ms in the request plus the grace period.
func (c *Client) Optimize(ctx context.Context, request, response any) error {
	_, err := c.OptimizeWithOutcome(ctx, request, response)
	return err
}

// OptimizeWithOutcome fills response in place and also returns the call summary
// for run diagnostics. Options override the deadline derivation when the
// request is opaque to the caller.
func (c *Client) OptimizeWithOutcome(ctx context.Context, request, response any, opts ...OptimizeOption) (Outcome, error) {
	return c.call(ctx, PathOptimize, request, response, opts)
}
