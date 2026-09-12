package graft

import (
	"os"
	"testing"
)

func TestRuntimeConfig(t *testing.T) {
	for _, key := range []string{"APP_HOST", "APP_PORT", "APP_DOCS", "APP_MAX_CONCURRENT", "APP_MAX_BODY_BYTES", "APP_REQUEST_TIMEOUT", "GRAFT_DEV"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.Address() != "127.0.0.1:8080" || cfg.Docs {
		t.Fatal(cfg, err)
	}
	t.Setenv("GRAFT_DEV", "1")
	cfg, err = ConfigFromEnv()
	if err != nil || !cfg.Docs {
		t.Fatal(cfg, err)
	}
	t.Setenv("APP_DOCS", "false")
	t.Setenv("APP_HOST", "::1")
	cfg, err = ConfigFromEnv()
	if err != nil || cfg.Docs || cfg.Address() != "[::1]:8080" {
		t.Fatal(cfg, err)
	}
	for _, tt := range []struct{ key, value string }{{"APP_HOST", "not/a/host"}, {"APP_PORT", "0"}, {"APP_DOCS", "oops"}, {"APP_MAX_CONCURRENT", "0"}, {"APP_MAX_BODY_BYTES", "-1"}, {"APP_REQUEST_TIMEOUT", "0s"}} {
		t.Run(tt.key, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
}
