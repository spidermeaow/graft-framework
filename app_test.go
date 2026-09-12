package graft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRoutes(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			a := New()
			a.Handle(method, "/items/{id}", func(c *Context) error {
				return c.JSON(200, map[string]string{"id": c.Param("id"), "q": c.Query("q"), "header": c.Header("X-Test")})
			})
			r := httptest.NewRequest(method, "/items/42?q=go", nil)
			r.Header.Set("X-Test", "yes")
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			var data map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || data["id"] != "42" || data["q"] != "go" || data["header"] != "yes" {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
}

func TestGroupsMiddlewareAndMethods(t *testing.T) {
	a := New()
	var order []string
	m := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+" before")
				next.ServeHTTP(w, r)
				order = append(order, name+" after")
			})
		}
	}
	a.Use(m("app"))
	group := a.Group("/api", m("group")).Group("/v1", m("nested"))
	group.GET("/ping", func(c *Context) error { order = append(order, "handler"); return c.String(200, "pong") })
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ping", nil))
	want := []string{"app before", "group before", "nested before", "handler", "nested after", "group after", "app after"}
	if !reflect.DeepEqual(order, want) {
		t.Fatal(order)
	}
	for _, tt := range []struct {
		method, path string
		code         int
	}{{"GET", "/missing", 404}, {"POST", "/api/v1/ping", 405}, {"HEAD", "/api/v1/ping", 200}} {
		w = httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.code {
			t.Fatalf("%s: %d", tt.method, w.Code)
		}
		if tt.code == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal(w.Header())
		}
	}
}

func TestBind(t *testing.T) {
	for _, tt := range []struct {
		name, body, content string
		status              int
	}{{"valid", `{"name":"Graft"}`, "application/json", 200}, {"unknown", `{"other":1}`, "application/json", 400}, {"trailing", `{} {}`, "application/json", 400}, {"invalid", `{`, "application/json", 400}, {"empty", "", "application/json", 400}, {"media", `{}`, "text/plain", 415}, {"large", `{"name":"` + strings.Repeat("a", 100) + `"}`, "application/json", 413}} {
		t.Run(tt.name, func(t *testing.T) {
			a := New()
			a.POST("/", func(c *Context) error {
				var body struct {
					Name string `json:"name"`
				}
				if err := c.BindLimit(&body, 64); err != nil {
					return err
				}
				return c.JSON(200, body)
			})
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.content)
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		h      Handler
		status int
		text   string
	}{{"http", func(*Context) error { return fmt.Errorf("wrapped: %w", NotFound("missing")) }, 404, "missing"}, {"private", func(*Context) error { return errors.New("private secret") }, 500, "internal server error"}, {"panic", func(*Context) error { panic("secret") }, 500, "internal server error"}, {"encoding", func(c *Context) error { return c.JSON(200, make(chan int)) }, 500, "internal server error"}, {"committed", func(c *Context) error { _ = c.String(202, "accepted"); return errors.New("late") }, 202, "accepted"}} {
		t.Run(tt.name, func(t *testing.T) {
			a := New()
			a.GET("/", tt.h)
			w := httptest.NewRecorder()
			a.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.text) || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
}

func TestCustomErrorHandlerAndLogging(t *testing.T) {
	var log bytes.Buffer
	a := New()
	a.Use(RequestID(), Logger(slog.New(slog.NewJSONHandler(&log, nil))), Recovery())
	a.SetErrorHandler(func(c *Context, err error) { _ = c.JSON(503, map[string]string{"message": "unavailable"}) })
	a.GET("/", func(*Context) error { panic("failure") })
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 || w.Header().Get("X-Request-ID") == "" || !strings.Contains(log.String(), `"status":503`) {
		t.Fatalf("%d %s", w.Code, &log)
	}
}

func TestDefaultRecoveryLogs500(t *testing.T) {
	var output bytes.Buffer
	a := New()
	a.Use(Logger(slog.New(slog.NewJSONHandler(&output, nil))))
	a.GET("/", func(*Context) error { panic("test panic") })
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || !strings.Contains(output.String(), `"status":500`) {
		t.Fatalf("status %d log %s", w.Code, &output)
	}
}

func TestGracefulShutdown(t *testing.T) {
	a := New()
	entered, release := make(chan struct{}), make(chan struct{})
	a.GET("/", func(c *Context) error { close(entered); <-release; return c.String(200, "done") })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- a.Serve(ctx, listener) }()
	response := make(chan error, 1)
	go func() {
		client := http.Client{Timeout: 5 * time.Second}
		r, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			defer r.Body.Close()
			b, e := io.ReadAll(r.Body)
			err = e
			if string(b) != "done" {
				err = fmt.Errorf("body %q", b)
			}
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never entered")
	}
	cancel()
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown hung")
	}
}

func TestStreamingAndFreeze(t *testing.T) {
	a := New()
	a.GET("/", func(c *Context) error { return http.NewResponseController(c.Response()).Flush() })
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !w.Flushed {
		t.Fatal("not flushed")
	}
	defer func() {
		if recover() == nil {
			t.Error("configuration after serving must panic")
		}
	}()
	a.GET("/late", func(*Context) error { return nil })
}

func BenchmarkRouter(b *testing.B)     { benchmarkApp(b, false, false) }
func BenchmarkMiddleware(b *testing.B) { benchmarkApp(b, true, false) }
func BenchmarkJSON(b *testing.B)       { benchmarkApp(b, false, true) }
func benchmarkApp(b *testing.B, middleware, jsonBody bool) {
	a := New()
	if middleware {
		a.Use(RequestID())
	}
	a.GET("/items/{id}", func(c *Context) error {
		if jsonBody {
			return c.JSON(200, map[string]string{"id": c.Param("id")})
		}
		return c.String(200, c.Param("id"))
	})
	r := httptest.NewRequest("GET", "/items/42", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.ServeHTTP(httptest.NewRecorder(), r)
	}
}
