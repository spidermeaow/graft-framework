package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spidermeaow/graft-framework"
)

type appConfig struct {
	runtime               graft.RuntimeConfig
	queryTimeout          time.Duration
	dbMaxOpen, dbMaxIdle  int
	rate                  float64
	burst                 int
	readToken, writeToken string
	adminAddress          string
	profiling             bool
	drainDelay            time.Duration
}

func readConfig() (appConfig, error) {
	runtime, err := graft.ConfigFromEnv()
	cfg := appConfig{runtime: runtime, queryTimeout: 2 * time.Second, dbMaxOpen: 10, dbMaxIdle: 5, rate: 100, burst: 100, adminAddress: os.Getenv("APP_ADMIN_ADDR")}
	if err != nil {
		return cfg, err
	}
	cfg.readToken, cfg.writeToken = os.Getenv("APP_READ_TOKEN"), os.Getenv("APP_WRITE_TOKEN")
	if len(cfg.readToken) < 32 || len(cfg.writeToken) < 32 || cfg.readToken == cfg.writeToken {
		return cfg, errors.New("set distinct APP_READ_TOKEN and APP_WRITE_TOKEN values of at least 32 characters")
	}
	for key, field := range map[string]*int{"DB_MAX_OPEN": &cfg.dbMaxOpen, "DB_MAX_IDLE": &cfg.dbMaxIdle, "APP_RATE_BURST": &cfg.burst} {
		if raw := os.Getenv(key); raw != "" {
			value, e := strconv.Atoi(raw)
			if e != nil || value < 1 {
				return cfg, fmt.Errorf("%s must be positive", key)
			}
			*field = value
		}
	}
	if cfg.dbMaxIdle > cfg.dbMaxOpen {
		return cfg, errors.New("DB_MAX_IDLE must not exceed DB_MAX_OPEN")
	}
	for key, field := range map[string]*time.Duration{"DB_QUERY_TIMEOUT": &cfg.queryTimeout, "APP_DRAIN_DELAY": &cfg.drainDelay} {
		if raw := os.Getenv(key); raw != "" {
			value, e := time.ParseDuration(raw)
			if e != nil || value < 0 || (key == "DB_QUERY_TIMEOUT" && value == 0) {
				return cfg, fmt.Errorf("invalid %s", key)
			}
			*field = value
		}
	}
	if raw := os.Getenv("APP_RATE_PER_SECOND"); raw != "" {
		value, e := strconv.ParseFloat(raw, 64)
		if e != nil || value <= 0 || value != value || value > 1e9 {
			return cfg, errors.New("invalid APP_RATE_PER_SECOND")
		}
		cfg.rate = value
	}
	if raw := os.Getenv("APP_PPROF"); raw != "" {
		cfg.profiling, err = strconv.ParseBool(raw)
		if err != nil {
			return cfg, errors.New("APP_PPROF must be a boolean")
		}
	}
	if cfg.adminAddress != "" {
		host, port, e := net.SplitHostPort(cfg.adminAddress)
		ip := net.ParseIP(host)
		p, pe := strconv.Atoi(port)
		if e != nil || ip == nil || !ip.IsLoopback() || pe != nil || p < 1 || p > 65535 {
			return cfg, errors.New("APP_ADMIN_ADDR must use a loopback IP and port")
		}
	}
	if cfg.profiling && cfg.adminAddress == "" {
		return cfg, errors.New("APP_PPROF requires APP_ADMIN_ADDR")
	}
	return cfg, nil
}

// This example uses separate read and write service credentials. Real multi-user
// apps should validate issuer/audience/expiry and perform object-level ownership
// checks using their identity provider. No forwarded header grants permissions.
func authorize(cfg appConfig) graft.Middleware {
	readHash, writeHash := sha256.Sum256([]byte(cfg.readToken)), sha256.Sum256([]byte(cfg.writeToken))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			hash := sha256.Sum256([]byte(value))
			read := subtle.ConstantTimeCompare(hash[:], readHash[:]) == 1 && cfg.readToken != ""
			write := subtle.ConstantTimeCompare(hash[:], writeHash[:]) == 1 && cfg.writeToken != ""
			if !ok || (!read && !write) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", 401)
				return
			}
			if !write && r.Method != "GET" && r.Method != "HEAD" {
				http.Error(w, "forbidden", 403)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func databaseError(ctx context.Context, err error) error {
	if ctx.Err() == context.DeadlineExceeded {
		return graft.NewHTTPError(504, "database deadline exceeded")
	}
	return err
}

func adminApp(db *sql.DB, cfg appConfig, metrics *graft.Metrics) *graft.App {
	app := graft.New()
	app.GET("/metrics", func(c *graft.Context) error {
		metrics.Handler().ServeHTTP(c.Response(), c.Request())
		s := db.Stats()
		_, err := fmt.Fprintf(c.Response(), "# TYPE graft_db_open_connections gauge\ngraft_db_open_connections %d\n# TYPE graft_db_in_use_connections gauge\ngraft_db_in_use_connections %d\n# TYPE graft_db_idle_connections gauge\ngraft_db_idle_connections %d\n# TYPE graft_db_wait_total counter\ngraft_db_wait_total %d\n# TYPE graft_db_wait_seconds_total counter\ngraft_db_wait_seconds_total %g\n", s.OpenConnections, s.InUse, s.Idle, s.WaitCount, s.WaitDuration.Seconds())
		return err
	})
	if cfg.profiling {
		// Explicit handlers on the private listener; never serve DefaultServeMux.
		app.GET("/debug/pprof/", func(c *graft.Context) error { pprof.Index(c.Response(), c.Request()); return nil })
		app.GET("/debug/pprof/profile", func(c *graft.Context) error { pprof.Profile(c.Response(), c.Request()); return nil })
		app.GET("/debug/pprof/trace", func(c *graft.Context) error { pprof.Trace(c.Response(), c.Request()); return nil })
	}
	return app
}

func serve(ctx context.Context, app *graft.App, db *sql.DB, cfg appConfig, health *graft.Health, metrics *graft.Metrics) error {
	listener, err := net.Listen("tcp", cfg.runtime.Address())
	if err != nil {
		return err
	}
	var admin net.Listener
	if cfg.adminAddress != "" {
		admin, err = net.Listen("tcp", cfg.adminAddress)
		if err != nil {
			listener.Close()
			return err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	count := 1
	if admin != nil {
		count++
		go func() { results <- adminApp(db, cfg, metrics).Serve(ctx, admin) }()
	}
	graft.PrintStartupAddress(listener.Addr().String(), cfg.runtime.Docs)
	go func() {
		results <- app.ServeWithConfig(ctx, listener, graft.ServerConfig{Health: health, DrainDelay: cfg.drainDelay, WriteTimeout: cfg.runtime.RequestTimeout + 5*time.Second})
	}()
	var result error
	for i := 0; i < count; i++ {
		result = errors.Join(result, <-results)
		cancel()
	}
	// Caller closes the pool only after both listeners finish draining.
	return result
}
