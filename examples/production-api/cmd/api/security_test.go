package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorization(t *testing.T) {
	cfg := testConfig()
	h := authorize(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tt := range []struct {
		method, token string
		status        int
	}{{"GET", "", 401}, {"GET", "wrong", 401}, {"GET", cfg.readToken, 204}, {"POST", cfg.readToken, 403}, {"POST", cfg.writeToken, 204}, {"GET", cfg.writeToken, 204}} {
		r := httptest.NewRequest(tt.method, "/api/machines", nil)
		r.Header.Set("Authorization", "Bearer "+tt.token)
		r.Header.Set("X-Forwarded-User", "admin")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatal(tt.method, tt.status, w.Code)
		}
	}
}

func TestPrivateAdminConfig(t *testing.T) {
	t.Setenv("APP_READ_TOKEN", strings.Repeat("r", 32))
	t.Setenv("APP_WRITE_TOKEN", strings.Repeat("w", 32))
	t.Setenv("APP_ADMIN_ADDR", "0.0.0.0:9090")
	if _, err := readConfig(); err == nil {
		t.Fatal("public admin listener accepted")
	}
	t.Setenv("APP_ADMIN_ADDR", "127.0.0.1:9090")
	if _, err := readConfig(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WRITE_TOKEN", strings.Repeat("r", 32))
	if _, err := readConfig(); err == nil {
		t.Fatal("same read and write credentials accepted")
	}
}
