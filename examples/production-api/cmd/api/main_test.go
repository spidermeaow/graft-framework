package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/migration"
	"github.com/spidermeaow/graft-framework/migration/postgres"
)

func TestMachinesWorkflow(t *testing.T) {
	dsn := os.Getenv("GRAFT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GRAFT_TEST_DATABASE_URL for PostgreSQL-backed example test")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		t.Fatal("expected PostgreSQL URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "graft_example_" + strings.ToLower(rand.Text())
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, `DROP DATABASE "`+name+`" WITH (FORCE)`); err != nil {
			t.Error(err)
		}
	}()
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Load(os.DirFS("../../migrations"), ".")
	if err != nil {
		t.Fatal(err)
	}
	runner := migration.Runner{Store: postgres.New(db), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := runner.Up(ctx, m); err != nil {
		t.Fatal(err)
	}
	health := &graft.Health{}
	health.SetReady(true)
	cfg := testConfig()
	server := httptest.NewServer(application(db, cfg, health, &graft.Metrics{}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+cfg.writeToken)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, data)
		}
		return data
	}
	request("GET", "/health", "", 200)
	if body := request("GET", "/api/machines", "", 200); strings.TrimSpace(string(body)) != "[]" {
		t.Fatal(string(body))
	}
	var created machine
	if err := json.Unmarshal(request("POST", "/api/machines", `{"name":"press-01"}`, 201), &created); err != nil || created.ID != 1 || created.Status != "ready" {
		t.Fatal(created, err)
	}
	request("GET", "/api/machines/1", "", 200)
	request("GET", "/api/machines/999", "", 404)
	request("GET", "/api/machines/invalid", "", 400)
	request("GET", "/api/machines?limit=0", "", 400)
	request("GET", "/api/machines?limit=1", "", 200)
	request("GET", "/api/machines?after_id=-1", "", 400)
	if body := request("GET", "/api/machines?after_id=1", "", 200); strings.TrimSpace(string(body)) != "[]" {
		t.Fatal(string(body))
	}
	request("POST", "/api/machines", `{"name":"press-01"}`, 409)
	request("POST", "/api/machines", `{"name":" "}`, 400)
	request("POST", "/api/machines", `{"name":"x","unexpected":true}`, 400)
	request("GET", "/swagger", "", 200)
	var spec map[string]any
	if err := json.Unmarshal(request("GET", "/openapi.json", "", 200), &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec["paths"].(map[string]any)) != 6 {
		t.Fatal(spec)
	}

	// A locked table must not hold the API connection indefinitely.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "LOCK TABLE machines IN ACCESS EXCLUSIVE MODE"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	request("GET", "/api/machines", "", 504)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/machines", "", 200)
	// The deadline also bounds waiting for a connection, not just executing SQL.
	db.SetMaxOpenConns(1)
	held, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/machines", "", 504)
	held.Close()
	request("GET", "/api/machines", "", 200)
	if db.Stats().WaitCount == 0 {
		t.Fatal("pool saturation was not exercised")
	}
	// Expand/contract compatibility: old queries and the new column coexist.
	if _, err := db.ExecContext(ctx, "ALTER TABLE machines ADD COLUMN notes TEXT"); err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/machines", `{"name":"old-writer"}`, 201)
	if _, err := db.ExecContext(ctx, "UPDATE machines SET notes=$1 WHERE name=$2", "new-writer", "old-writer"); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/machines", "", 200)
	if _, err := db.ExecContext(ctx, "ALTER TABLE machines DROP COLUMN notes"); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/machines", "", 200)
	if err := runner.Rollback(ctx, m, 0); err != nil {
		t.Fatal(err)
	}
	db.Close()
	request("GET", "/readyz", "", 503)
	request("GET", "/livez", "", 200)
}

func testConfig() appConfig {
	return appConfig{runtime: graft.RuntimeConfig{Docs: true, MaxConcurrent: 8, MaxBodyBytes: 1 << 20, RequestTimeout: 3 * time.Second}, queryTimeout: time.Second, rate: 10000, burst: 10000, readToken: strings.Repeat("r", 32), writeToken: strings.Repeat("w", 32)}
}
