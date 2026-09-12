package graft

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// RuntimeConfig holds explicit starter-app settings, not global framework state.
// Existing applications using Run(addr) retain their current behavior.
type RuntimeConfig struct {
	Host           string
	Port           string
	Docs           bool
	MaxConcurrent  int
	MaxBodyBytes   int64
	RequestTimeout time.Duration
}

// ConfigFromEnv reads configuration after LoadEnv. Starter values are conservative
// examples, not a capacity guarantee. Invalid values fail startup without logging secrets.
// APP_DOCS defaults off, except under graft dev (GRAFT_DEV=1).
func ConfigFromEnv() (RuntimeConfig, error) {
	cfg := RuntimeConfig{Host: "127.0.0.1", Port: "8080", Docs: os.Getenv("GRAFT_DEV") == "1", MaxConcurrent: 64, MaxBodyBytes: 1 << 20, RequestTimeout: 10 * time.Second}
	if host := os.Getenv("APP_HOST"); host != "" {
		cfg.Host = host
	}
	if cfg.Host != "localhost" && net.ParseIP(cfg.Host) == nil {
		return cfg, fmt.Errorf("APP_HOST must be an IP address or localhost")
	}
	if port := os.Getenv("APP_PORT"); port != "" {
		cfg.Port = port
	}
	p, err := strconv.Atoi(cfg.Port)
	if err != nil || p < 1 || p > 65535 {
		return cfg, fmt.Errorf("APP_PORT must be between 1 and 65535")
	}
	if docs, exists := os.LookupEnv("APP_DOCS"); exists {
		cfg.Docs, err = strconv.ParseBool(docs)
		if err != nil {
			return cfg, fmt.Errorf("APP_DOCS must be a boolean")
		}
	}
	if raw := os.Getenv("APP_MAX_CONCURRENT"); raw != "" {
		cfg.MaxConcurrent, err = strconv.Atoi(raw)
		if err != nil || cfg.MaxConcurrent <= 0 {
			return cfg, fmt.Errorf("APP_MAX_CONCURRENT must be positive")
		}
	}
	if raw := os.Getenv("APP_MAX_BODY_BYTES"); raw != "" {
		cfg.MaxBodyBytes, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cfg.MaxBodyBytes <= 0 {
			return cfg, fmt.Errorf("APP_MAX_BODY_BYTES must be positive")
		}
	}
	if raw := os.Getenv("APP_REQUEST_TIMEOUT"); raw != "" {
		cfg.RequestTimeout, err = time.ParseDuration(raw)
		if err != nil || cfg.RequestTimeout <= 0 {
			return cfg, fmt.Errorf("APP_REQUEST_TIMEOUT must be a positive duration")
		}
	}
	return cfg, nil
}

func (cfg RuntimeConfig) Address() string { return net.JoinHostPort(cfg.Host, cfg.Port) }
