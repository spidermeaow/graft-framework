// Package auth supplies optional authentication and authorization middleware.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/spidermeaow/graft-framework"
)

// Principal is the verified identity attached to a request.
type Principal struct {
	Subject     string   `json:"subject"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type contextKey struct{}

// PrincipalFromContext returns only identities established by auth middleware.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

// Authenticator verifies credentials from a request. Missing credentials must
// return ErrMissing; all other failures return an error without secret details.
type Authenticator interface {
	Authenticate(*http.Request) (Principal, error)
}

// AuthenticatorFunc adapts a function to Authenticator.
type AuthenticatorFunc func(*http.Request) (Principal, error)

func (f AuthenticatorFunc) Authenticate(r *http.Request) (Principal, error) { return f(r) }

var ErrMissing = errors.New("missing credentials")
var ErrInvalid = errors.New("invalid credentials")

// Require rejects missing or invalid credentials with 401.
func Require(a Authenticator) graft.Middleware { return authenticate(a, true) }

// Optional permits missing credentials, but rejects invalid ones.
func Optional(a Authenticator) graft.Middleware { return authenticate(a, false) }

func authenticate(a Authenticator, required bool) graft.Middleware {
	if a == nil {
		panic("auth: nil authenticator")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := a.Authenticate(r)
			if errors.Is(err, ErrMissing) && !required {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil || p.Subject == "" {
				reject(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)))
		})
	}
}

// RequireRole permits a request if its verified principal has the role.
func RequireRole(role string) graft.Middleware {
	return authorize(func(p Principal) bool { return contains(p.Roles, role) })
}

// RequirePermission permits a request if its verified principal has the permission.
func RequirePermission(permission string) graft.Middleware {
	return authorize(func(p Principal) bool { return contains(p.Permissions, permission) })
}

func authorize(allowed func(Principal) bool) graft.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok {
				reject(w, 401, "unauthorized")
				return
			}
			if !allowed(p) {
				reject(w, 403, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func contains(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func reject(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// APIKey accepts an X-API-Key header. The secret is kept as a digest and compared
// in constant time. Supply one identity for each key.
func APIKey(key string, principal Principal) Authenticator {
	if len(key) < 16 || principal.Subject == "" {
		panic("auth: API key must be at least 16 bytes and principal subject must be set")
	}
	want := sha256.Sum256([]byte(key))
	return AuthenticatorFunc(func(r *http.Request) (Principal, error) {
		value := r.Header.Get("X-API-Key")
		if value == "" {
			return Principal{}, ErrMissing
		}
		got := sha256.Sum256([]byte(value))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			return Principal{}, ErrInvalid
		}
		return principal, nil
	})
}

func bearer(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", ErrMissing
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrInvalid
	}
	return parts[1], nil
}
