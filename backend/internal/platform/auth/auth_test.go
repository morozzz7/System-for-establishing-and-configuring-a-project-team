package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teamopt/backend/internal/platform/actor"
)

func fixedNow() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }

func testProvider(t *testing.T) *Provider {
	t.Helper()
	users, err := ParseUsers(`[{"login":"manager","password":"demo","actor":"emp_mgr01"}]`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProvider(users, "a-long-development-signing-secret", 60, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTokenRoundTrip(t *testing.T) {
	p := testProvider(t)
	token, ttl, err := p.Issue("emp_mgr01")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 3600 {
		t.Fatalf("ttl = %d, want 3600", ttl)
	}
	principal, err := p.Verify(token)
	if err != nil || principal != "emp_mgr01" {
		t.Fatalf("verify = %q, %v", principal, err)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	p := testProvider(t)
	token, _, err := p.Issue("emp_mgr01")
	if err != nil {
		t.Fatal(err)
	}
	tampered := token[:len(token)-2] + "xx"
	if _, err := p.Verify(tampered); err == nil {
		t.Fatal("tampered token accepted")
	}
	other, err := NewProvider([]User{{Login: "manager", Password: "demo", Actor: "emp_mgr01"}},
		"a-different-signing-secret-value", 60, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Verify(token); err == nil {
		t.Fatal("token signed by another key accepted")
	}
}

func TestTokenExpiry(t *testing.T) {
	p := testProvider(t)
	token, _, err := p.Issue("emp_mgr01")
	if err != nil {
		t.Fatal(err)
	}
	expired, err := NewProvider([]User{{Login: "manager", Password: "demo", Actor: "emp_mgr01"}},
		"a-long-development-signing-secret", 60, func() time.Time { return fixedNow().Add(2 * time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expired.Verify(token); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestProviderRejectsSystemSubject(t *testing.T) {
	p := testProvider(t)
	token, err := Sign("a-long-development-signing-secret", Claims{
		Sub: actor.System, Iat: fixedNow().Unix(), Exp: fixedNow().Add(time.Hour).Unix(),
	}, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(token); err == nil {
		t.Fatal("forged system principal accepted")
	}
}

func TestAuthenticate(t *testing.T) {
	p := testProvider(t)
	if actorID, ok := p.Authenticate("manager", "demo"); !ok || actorID != "emp_mgr01" {
		t.Fatalf("valid credentials = %q, %v", actorID, ok)
	}
	if _, ok := p.Authenticate("manager", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if _, ok := p.Authenticate("unknown", "demo"); ok {
		t.Fatal("unknown login accepted")
	}
}

func TestMiddlewareInjectsActorAndRejectsMissingToken(t *testing.T) {
	p := testProvider(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte(actor.FromContext(r.Context())))
	})
	handler := p.Middleware(next)

	token, _, err := p.Issue("emp_mgr01")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Body.String() != "emp_mgr01" {
		t.Fatalf("authorized request = %d %q", rec.Code, rec.Body.String())
	}

	for _, header := range []string{"", "Basic abc", "Bearer invalid.token.value"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("header %q: status = %d, want 401", header, rec.Code)
		}
	}
}

func TestMiddlewareExemptsPublicPaths(t *testing.T) {
	p := testProvider(t)
	handler := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/healthz", "/api/v1/readyz"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
	}
}

func TestNewProviderValidatesConfiguration(t *testing.T) {
	if _, err := NewProvider([]User{{Login: "manager", Password: "demo", Actor: "system"}}, "secret-long-enough-for-hs256", 60, nil); err == nil {
		t.Fatal("system actor accepted as a user")
	}
	if _, err := NewProvider(nil, "secret-long-enough-for-hs256", 60, nil); err == nil {
		t.Fatal("empty user set accepted")
	}
	if _, err := NewProvider([]User{{Login: "manager", Password: "demo", Actor: "emp_mgr01"}}, "short", 60, nil); err == nil {
		t.Fatal("short signing secret accepted")
	}
}
