package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spidermeaow/graft-framework"
)

func TestAPIKeyAndAuthorization(t *testing.T) {
	a := graft.New()
	g := a.Group("/api")
	g.Use(Require(APIKey("0123456789abcdef", Principal{Subject: "alice", Roles: []string{"admin"}})), RequireRole("admin"))
	g.GET("/me", func(c *graft.Context) error {
		p, ok := PrincipalFromContext(c.Context())
		if !ok {
			t.Fatal("missing principal")
		}
		return c.JSON(200, p)
	}, graft.Security("ApiKeyAuth"))
	for _, tt := range []struct {
		key    string
		status int
	}{{"", 401}, {"wrong", 401}, {"0123456789abcdef", 200}} {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.Header.Set("X-API-Key", tt.key)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("key %q: status %d", tt.key, w.Code)
		}
	}
	data, err := a.OpenAPI("test", "1")
	if err != nil || !strings.Contains(string(data), "ApiKeyAuth") || !strings.Contains(string(data), "securitySchemes") {
		t.Fatal(err, string(data))
	}
}

func TestJWTClaimsAndSignature(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	verifier := JWT(JWTConfig{Issuer: "issuer", Audience: "api", HMACSecret: secret, Clock: func() time.Time { return time.Unix(1000, 0) }})
	sign := func(claims map[string]any, key []byte) string {
		t.Helper()
		header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
		payload, _ := json.Marshal(claims)
		message := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(message))
		return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	claims := map[string]any{"iss": "issuer", "aud": "api", "sub": "alice", "exp": 1100, "roles": []string{"admin"}}
	for _, tt := range []struct {
		token string
		valid bool
	}{
		{sign(claims, secret), true},
		{sign(claims, []byte("wrongwrongwrongwrongwrongwrongwrongwrong")), false},
		{sign(map[string]any{"iss": "issuer", "aud": "api", "sub": "alice", "exp": 999}, secret), false},
		{sign(map[string]any{"iss": "issuer", "aud": "other", "sub": "alice", "exp": 1100}, secret), false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+tt.token)
		p, err := verifier.Authenticate(r)
		if (err == nil) != tt.valid || tt.valid && p.Subject != "alice" {
			t.Fatalf("valid=%v principal=%+v error=%v", tt.valid, p, err)
		}
	}
}

func TestMissingAndForbidden(t *testing.T) {
	a := graft.New()
	a.Group("/secure", Require(APIKey("0123456789abcdef", Principal{Subject: "alice"})), RequirePermission("write")).GET("/item", func(c *graft.Context) error { return c.String(200, "ok") })
	r := httptest.NewRequest("GET", "/secure/item", nil)
	r.Header.Set("X-API-Key", "0123456789abcdef")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
