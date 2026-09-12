package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lib/pq"
	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/openapi"
)

type machine struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type createMachine struct {
	Name string `json:"name"`
}

var machineSchema = openapi.Schema{Type: "object", Required: []string{"id", "name", "status", "created_at"}, Properties: map[string]openapi.Schema{
	"id": {Type: "integer", Format: "int64"}, "name": {Type: "string"}, "status": {Type: "string", Enum: []string{"ready", "offline"}}, "created_at": {Type: "string", Format: "date-time"},
}}
var errorSchema = openapi.Schema{Type: "object", Properties: map[string]openapi.Schema{"error": {Type: "string"}}}

func application(db *sql.DB, cfg appConfig, health *graft.Health, metrics *graft.Metrics) *graft.App {
	app := graft.New()
	app.Use(graft.RequestID(), metrics.Middleware(), graft.Logger(), graft.Recovery())
	app.GET("/livez", health.Liveness)
	app.GET("/readyz", health.Readiness(func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return db.PingContext(ctx)
	}))
	app.GET("/{$}", func(c *graft.Context) error {
		return c.JSON(200, map[string]string{"message": "Machines API", "documentation": "/swagger"})
	}, graft.Summary("API information"))
	app.GET("/health", func(c *graft.Context) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return graft.NewHTTPError(503, "database unavailable")
		}
		return c.JSON(200, map[string]string{"status": "ok"})
	}, graft.Summary("Database readiness"), graft.Response(200, "Ready", openapi.Schema{Type: "object"}), graft.Response(503, "Unavailable", errorSchema))
	admission := graft.ConcurrencyLimit(cfg.runtime.MaxConcurrent)
	rate := graft.RateLimit(cfg.rate, cfg.burst)
	api := app.Group("/api", authorize(cfg), rate, admission, graft.BodyLimit(cfg.runtime.MaxBodyBytes), graft.RequestDeadline(cfg.runtime.RequestTimeout))
	api.GET("/machines", func(c *graft.Context) error {
		limit := 100
		if raw := c.Query("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				return graft.NewHTTPError(400, "limit must be between 1 and 100")
			}
			limit = n
		}
		var afterID int64
		if raw := c.Query("after_id"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				return graft.NewHTTPError(400, "after_id must be a nonnegative integer")
			}
			afterID = n
		}
		ctx, cancel := context.WithTimeout(c.Context(), cfg.queryTimeout)
		defer cancel()
		rows, err := db.QueryContext(ctx, "SELECT id,name,status,created_at FROM machines WHERE id > $2 ORDER BY id LIMIT $1", limit, afterID)
		if err != nil {
			return databaseError(ctx, err)
		}
		defer rows.Close()
		items := make([]machine, 0)
		for rows.Next() {
			var m machine
			if err := rows.Scan(&m.ID, &m.Name, &m.Status, &m.CreatedAt); err != nil {
				return databaseError(ctx, err)
			}
			items = append(items, m)
		}
		if err := rows.Err(); err != nil {
			return databaseError(ctx, err)
		}
		return c.JSON(200, items)
	}, graft.Summary("List machines"), graft.Tag("Machines"), graft.QueryParameter("limit", false, openapi.Schema{Type: "integer", Description: "1 to 100; default 100"}), graft.QueryParameter("after_id", false, openapi.Schema{Type: "integer", Format: "int64", Description: "Last ID from the previous page"}), graft.Response(200, "Machines", openapi.Schema{Type: "array", Items: &machineSchema}), graft.Response(400, "Invalid limit", errorSchema))
	api.GET("/machines/{id}", func(c *graft.Context) error {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			return graft.NewHTTPError(400, "id must be a positive integer")
		}
		var m machine
		ctx, cancel := context.WithTimeout(c.Context(), cfg.queryTimeout)
		defer cancel()
		err = db.QueryRowContext(ctx, "SELECT id,name,status,created_at FROM machines WHERE id=$1", id).Scan(&m.ID, &m.Name, &m.Status, &m.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return graft.NotFound("machine not found")
		}
		if err != nil {
			return databaseError(ctx, err)
		}
		return c.JSON(200, m)
	}, graft.Summary("Get machine"), graft.Tag("Machines"), graft.PathParameter("id", openapi.Schema{Type: "integer", Format: "int64"}), graft.Response(200, "Machine", machineSchema), graft.Response(400, "Invalid ID", errorSchema), graft.Response(404, "Not found", errorSchema))
	api.POST("/machines", func(c *graft.Context) error {
		var body createMachine
		if err := c.Bind(&body); err != nil {
			return err
		}
		body.Name = strings.TrimSpace(body.Name)
		if len(body.Name) == 0 || len([]rune(body.Name)) > 100 {
			return graft.NewHTTPError(400, "name must contain 1 to 100 characters")
		}
		var m machine
		ctx, cancel := context.WithTimeout(c.Context(), cfg.queryTimeout)
		defer cancel()
		err := db.QueryRowContext(ctx, "INSERT INTO machines(name) VALUES($1) RETURNING id,name,status,created_at", body.Name).Scan(&m.ID, &m.Name, &m.Status, &m.CreatedAt)
		if err != nil {
			var pg *pq.Error
			if errors.As(err, &pg) && pg.Code == "23505" {
				return graft.NewHTTPError(409, "machine name already exists")
			}
			return databaseError(ctx, err)
		}
		c.Response().Header().Set("Location", "/api/machines/"+strconv.FormatInt(m.ID, 10))
		return c.JSON(http.StatusCreated, m)
	}, graft.Summary("Create machine"), graft.Tag("Machines"), graft.RequestBody(openapi.Schema{Type: "object", Required: []string{"name"}, Properties: map[string]openapi.Schema{"name": {Type: "string"}}}), graft.Response(201, "Created", machineSchema), graft.Response(400, "Invalid request", errorSchema), graft.Response(409, "Duplicate name", errorSchema))
	if cfg.runtime.Docs {
		app.DocsWithMiddleware("Graft Machines API", "0.2.0", authorize(cfg), rate, admission, graft.RequestDeadline(cfg.runtime.RequestTimeout))
	}
	return app
}

func run() error {
	if err := graft.LoadEnv(); err != nil {
		return err
	}
	cfg, err := readConfig()
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required; see the example README")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.dbMaxOpen)
	db.SetMaxIdleConns(cfg.dbMaxIdle)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	health := &graft.Health{}
	metrics := &graft.Metrics{}
	app := application(db, cfg, health, metrics)
	service, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(service, app, db, cfg, health, metrics)

}
func main() {
	if err := run(); err != nil {
		graft.Fatal(err)
	}
}
