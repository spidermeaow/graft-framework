package auth

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/spidermeaow/graft-framework"
)

// JWTConfig verifies HS256 or RS256 tokens with explicit issuer and audience.
// Exactly one of HMACSecret or RSAPublicKey must be provided.
type JWTConfig struct {
	Issuer, Audience string
	HMACSecret       []byte
	RSAPublicKey     *rsa.PublicKey
	Clock            func() time.Time
}

// JWT verifies signed access tokens. It never accepts the `none` algorithm.
func JWT(cfg JWTConfig) Authenticator {
	if cfg.Issuer == "" || cfg.Audience == "" || (len(cfg.HMACSecret) >= 32) == (cfg.RSAPublicKey != nil) {
		panic("auth: JWT needs issuer, audience, and exactly one HS256 secret (at least 32 bytes) or RSA public key")
	}
	secret := append([]byte(nil), cfg.HMACSecret...)
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return documentedAuthenticator{run: func(r *http.Request) (Principal, error) {
		token, err := bearer(r)
		if err != nil {
			return Principal{}, err
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 || len(token) > 16384 {
			return Principal{}, ErrInvalid
		}
		header, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			return Principal{}, ErrInvalid
		}
		var meta struct {
			Alg string `json:"alg"`
			Typ string `json:"typ"`
		}
		if json.Unmarshal(header, &meta) != nil {
			return Principal{}, ErrInvalid
		}
		signed := []byte(parts[0] + "." + parts[1])
		signature, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return Principal{}, ErrInvalid
		}
		if len(secret) > 0 {
			if meta.Alg != "HS256" {
				return Principal{}, ErrInvalid
			}
			mac := hmac.New(sha256.New, secret)
			_, _ = mac.Write(signed)
			if !hmac.Equal(signature, mac.Sum(nil)) {
				return Principal{}, ErrInvalid
			}
		} else {
			if meta.Alg != "RS256" {
				return Principal{}, ErrInvalid
			}
			digest := sha256.Sum256(signed)
			if rsa.VerifyPKCS1v15(cfg.RSAPublicKey, crypto.SHA256, digest[:], signature) != nil {
				return Principal{}, ErrInvalid
			}
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return Principal{}, ErrInvalid
		}
		var claims struct {
			Issuer      string          `json:"iss"`
			Audience    json.RawMessage `json:"aud"`
			Subject     string          `json:"sub"`
			Expires     int64           `json:"exp"`
			NotBefore   int64           `json:"nbf"`
			Roles       []string        `json:"roles"`
			Permissions []string        `json:"permissions"`
		}
		if json.Unmarshal(payload, &claims) != nil || claims.Issuer != cfg.Issuer || claims.Subject == "" || claims.Expires <= clock().Unix() || claims.NotBefore > clock().Unix() {
			return Principal{}, ErrInvalid
		}
		var audience string
		var audiences []string
		if json.Unmarshal(claims.Audience, &audience) == nil {
			audiences = []string{audience}
		} else if json.Unmarshal(claims.Audience, &audiences) != nil {
			return Principal{}, ErrInvalid
		}
		if !contains(audiences, cfg.Audience) {
			return Principal{}, ErrInvalid
		}
		return Principal{Subject: claims.Subject, Roles: claims.Roles, Permissions: claims.Permissions}, nil
	}, metadata: graft.OpenAPIMetadata{
		Security:        []map[string][]string{{"BearerAuth": {}}},
		SecuritySchemes: map[string]map[string]any{"BearerAuth": {"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}},
	}}
}
