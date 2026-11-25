package auth

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/teamopt/backend/internal/platform/actor"
)

var (
	loginRe = regexp.MustCompile(`^[a-z0-9_-]{3,64}$`)
	actorRe = regexp.MustCompile(`^emp_[a-z0-9]{4,12}$`)
)

// User is one configured synthetic account. Passwords and pseudonyms are server-side
// configuration; no real personal data is stored in the repository.
type User struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Actor    string `json:"actor"`
}

// Provider validates configured accounts and issues/verifies tokens.
type Provider struct {
	users  map[string]User
	secret string
	ttl    time.Duration
	now    func() time.Time
}

// ParseUsers reads the AUTH_USERS JSON array.
func ParseUsers(raw string) ([]User, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("AUTH_USERS is empty")
	}
	var users []User
	if err := json.Unmarshal([]byte(raw), &users); err != nil {
		return nil, fmt.Errorf("parse AUTH_USERS: %w", err)
	}
	if len(users) == 0 {
		return nil, errors.New("AUTH_USERS has no accounts")
	}
	return users, nil
}

// NewProvider validates the configuration before any request is served.
func NewProvider(users []User, secret string, ttlMinutes int, now func() time.Time) (*Provider, error) {
	if len(secret) < 16 {
		return nil, errors.New("AUTH_JWT_SECRET must be at least 16 characters")
	}
	if ttlMinutes < 1 || ttlMinutes > 1440 {
		return nil, errors.New("AUTH_TOKEN_TTL_MINUTES must be between 1 and 1440")
	}
	if now == nil {
		now = time.Now
	}
	p := &Provider{
		users:  make(map[string]User, len(users)),
		secret: secret,
		ttl:    time.Duration(ttlMinutes) * time.Minute,
		now:    now,
	}
	for _, user := range users {
		if !loginRe.MatchString(user.Login) {
			return nil, fmt.Errorf("invalid auth login %q", user.Login)
		}
		if user.Password == "" || len(user.Password) > 128 {
			return nil, fmt.Errorf("invalid password for auth login %q", user.Login)
		}
		if !actorRe.MatchString(user.Actor) {
			return nil, fmt.Errorf("invalid actor %q for auth login %q", user.Actor, user.Login)
		}
		if _, exists := p.users[user.Login]; exists {
			return nil, fmt.Errorf("duplicate auth login %q", user.Login)
		}
		p.users[user.Login] = user
	}
	if len(p.users) == 0 {
		return nil, errors.New("no auth accounts configured")
	}
	return p, nil
}

// Authenticate compares credentials in constant time and returns the configured actor.
func (p *Provider) Authenticate(login, password string) (string, bool) {
	user, ok := p.users[strings.ToLower(login)]
	if !ok {
		return "", false
	}
	if !hmac.Equal([]byte(password), []byte(user.Password)) {
		return "", false
	}
	return user.Actor, true
}

// Issue signs a token for an already authenticated principal.
func (p *Provider) Issue(principal string) (string, int, error) {
	now := p.now().UTC()
	token, err := Sign(p.secret, Claims{
		Sub: principal,
		Iat: now.Unix(),
		Exp: now.Add(p.ttl).Unix(),
	}, now)
	if err != nil {
		return "", 0, err
	}
	return token, int(p.ttl / time.Second), nil
}

// Verify returns the token's principal, rejecting tokens for non-user subjects so that a
// client cannot obtain the reserved `system` actor by forging a claim with a leaked key.
func (p *Provider) Verify(token string) (string, error) {
	claims, err := Parse(p.secret, token, p.now().UTC())
	if err != nil {
		return "", err
	}
	if !actorRe.MatchString(claims.Sub) {
		return "", ErrInvalidToken
	}
	return claims.Sub, nil
}

// PublicPaths are the operations that do not require a token by contract.
var PublicPaths = map[string]bool{
	"/api/v1/auth/login": true,
	"/api/v1/healthz":    true,
	"/api/v1/readyz":     true,
}

// Middleware injects the verified principal into the request context. Every downstream
// service reads it through actor.FromContext; client-supplied identity fields are ignored.
func (p *Provider) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PublicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		fields := strings.Fields(header)
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			writeUnauthorized(w, r, "A bearer token is required")
			return
		}
		principal, err := p.Verify(fields[1])
		if err != nil {
			writeUnauthorized(w, r, "The bearer token is invalid or expired")
			return
		}
		next.ServeHTTP(w, r.WithContext(actor.WithContext(r.Context(), principal)))
	})
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	requestID := r.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = domain.GenerateID("req_", 12)
	}
	w.Header().Set("X-Request-Id", requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(domain.NewProblem(http.StatusUnauthorized,
		"UNAUTHORIZED", detail, requestID, []string{"INVALID_TOKEN"}))
}
