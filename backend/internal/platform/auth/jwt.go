// Package auth implements the local token provider. Tokens are HS256 JWTs signed with a
// server-side key; the `sub` claim is the only accepted source of the acting principal.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidToken covers every verification failure; callers must not distinguish them.
var ErrInvalidToken = errors.New("invalid bearer token")

// Claims is the minimal claim set the API trusts.
type Claims struct {
	Sub string `json:"sub"`
	Iat int64 `json:"iat"`
	Exp int64 `json:"exp"`
}

const jwtHeader = `{"alg":"HS256","typ":"JWT"}`

// Sign issues a compact HS256 JWT. The caller supplies `now` so token creation is testable
// without wall-clock reads (AGENTS.md determinism rule).
func Sign(secret string, claims Claims, now time.Time) (string, error) {
	if secret == "" {
		return "", errors.New("auth signing secret is empty")
	}
	if claims.Sub == "" || claims.Exp <= now.Unix() {
		return "", ErrInvalidToken
	}
	encoding := base64.RawURLEncoding
	encodedHeader := encoding.EncodeToString([]byte(jwtHeader))
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode claims: %w", err)
	}
	encodedPayload := encoding.EncodeToString(payload)
	input := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + encoding.EncodeToString(mac.Sum(nil)), nil
}

// Parse verifies the signature, algorithm and expiry before returning the claims.
func Parse(secret, token string, now time.Time) (Claims, error) {
	if secret == "" {
		return Claims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	encoding := base64.RawURLEncoding
	headerBytes, err := encoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Alg != "HS256" || header.Typ != "JWT" {
		return Claims{}, ErrInvalidToken
	}
	signature, err := encoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), signature) {
		return Claims{}, ErrInvalidToken
	}
	payload, err := encoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Sub == "" || claims.Exp <= now.Unix() || claims.Iat > now.Unix()+60 {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}
