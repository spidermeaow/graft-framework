package graft

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func TestOverloadAndRecovery(t *testing.T) {
	app := New()
	metrics := &Metrics{}
	app.Use(metrics.Middleware(), ConcurrencyLimit(2))
	entered, release := make(chan bool, 2), make(chan struct{})
	app.GET("/slow", func(c *Context) error { entered <- true; <-release; return c.String(200, "ok") })
	app.GET("/fast", func(c *Context) error { return c.String(200, "ok") })
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/slow", nil))
		}()
	}
	await(t, entered)
	await(t, entered)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/fast", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code, w.Header())
	}
	close(release)
	workers.Wait()
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/fast", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	s := metrics.Snapshot()
	if s.Requests != 4 || s.Rejected != 1 || s.InFlight != 0 {
		t.Fatal(s)
	}
	app2 := New()
	app2.Use(ConcurrencyLimit(1))
	app2.GET("/panic", func(*Context) error { panic("private") })
	app2.GET("/", func(c *Context) error { return c.String(200, "ok") })
	app2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/panic", nil))
	w = httptest.NewRecorder()
	app2.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatal("slot leaked after panic")
	}
}

func TestBodyLimitRawAndUnknownLength(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		app := New()
		app.Use(BodyLimit(8))
		app.POST("/", func(c *Context) error { _, err := io.Copy(io.Discard, c.Request().Body); return err })
		r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("a", 9)))
		if unknown {
			r.ContentLength = -1
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != 413 {
			t.Fatal(unknown, w.Code, w.Body)
		}
	}
}

func TestDeadlineDoesNotReleaseRunningHandler(t *testing.T) {
	app := New()
	app.Use(ConcurrencyLimit(1), RequestDeadline(20*time.Millisecond))
	expired, release := make(chan bool, 1), make(chan struct{})
	app.GET("/", func(c *Context) error { <-c.Context().Done(); expired <- true; <-release; return c.Context().Err() })
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		done <- w.Code
	}()
	await(t, expired)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal("expired handler lost slot", w.Code)
	}
	close(release)
	if code := await(t, done); code != 504 {
		t.Fatal(code)
	}
}

func TestRequestDeadlinePreservesEarlierCancellation(t *testing.T) {
	app := New()
	app.Use(RequestDeadline(time.Hour))
	app.GET("/", func(c *Context) error { <-c.Context().Done(); return c.Context().Err() })
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	if w.Code != 504 {
		t.Fatal(w.Code)
	}
}

func TestDocsProtectedIncludingAssets(t *testing.T) {
	app := New()
	app.DocsWithMiddleware("private", "1", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "test" {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	for _, path := range []string{"/swagger", "/swagger/", "/swagger/initializer.js", "/swagger/swagger-ui-bundle.js", "/openapi.json"} {
		for _, auth := range []bool{false, true} {
			r := httptest.NewRequest("GET", path, nil)
			if auth {
				r.Header.Set("Authorization", "test")
			}
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			want := 401
			if auth {
				want = 200
			}
			if w.Code != want {
				t.Fatal(path, auth, w.Code)
			}
		}
	}
	w := httptest.NewRecorder()
	New().ServeHTTP(w, httptest.NewRequest("GET", "/swagger", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestRateLimitAndMetrics(t *testing.T) {
	m := &Metrics{}
	app := New()
	app.Use(m.Middleware(), RateLimit(.001, 1))
	app.GET("/", func(c *Context) error { return c.String(200, "ok") })
	for _, code := range []int{200, 429} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != code {
			t.Fatal(w.Code)
		}
	}
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	for _, part := range []string{"graft_requests_total 2", "graft_rejected_total 1", "graft_responses_total{status=\"429\"} 1", "graft_request_duration_seconds_count 2", "graft_go_heap_alloc_bytes"} {
		if !strings.Contains(w.Body.String(), part) {
			t.Fatal(part, w.Body)
		}
	}
}

func TestShutdownReadinessDrainAndHooks(t *testing.T) {
	health := &Health{}
	app := New()
	app.GET("/readyz", health.Readiness())
	entered, release := make(chan bool, 1), make(chan struct{})
	var finished atomic.Bool
	app.GET("/work", func(c *Context) error { entered <- true; <-release; finished.Store(true); return c.String(200, "done") })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	hook := make(chan bool, 1)
	go func() {
		done <- app.ServeWithConfig(ctx, listener, ServerConfig{Health: health, DrainDelay: 100 * time.Millisecond, ShutdownTimeout: time.Second, ShutdownHooks: []func(context.Context) error{func(context.Context) error { hook <- finished.Load(); return nil }}})
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	requestDone := make(chan error, 1)
	go func() {
		r, e := client.Get("http://" + listener.Addr().String() + "/work")
		if e == nil {
			_, e = io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		requestDone <- e
	}()
	await(t, entered)
	cancel()
	deadline := time.Now().Add(time.Second)
	for health.ready.Load() {
		if time.Now().After(deadline) {
			t.Fatal("readiness not withdrawn")
		}
		time.Sleep(time.Millisecond)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	if finished.Load() {
		t.Fatal("handler unexpectedly ended")
	}
	close(release)
	if e := await(t, requestDone); e != nil {
		t.Fatal(e)
	}
	if e := await(t, done); e != nil {
		t.Fatal(e)
	}
	if !await(t, hook) {
		t.Fatal("hook ran before requests drained")
	}
}

func TestForcedShutdownCancelsHandlers(t *testing.T) {
	app := New()
	entered, ended := make(chan bool, 1), make(chan bool, 1)
	app.GET("/", func(c *Context) error { entered <- true; <-c.Context().Done(); ended <- true; return nil })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- app.ServeWithConfig(ctx, listener, ServerConfig{ShutdownTimeout: 20 * time.Millisecond})
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	go func() {
		r, e := client.Get("http://" + listener.Addr().String())
		if e == nil {
			r.Body.Close()
		}
	}()
	await(t, entered)
	cancel()
	if e := await(t, done); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	await(t, ended)
}

func TestSlowRequestBodyTimeout(t *testing.T) {
	app := New()
	app.POST("/", func(c *Context) error { _, err := io.Copy(io.Discard, c.Request().Body); return err })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.ServeWithConfig(ctx, listener, ServerConfig{ReadTimeout: 50 * time.Millisecond}) }()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	io.WriteString(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\nConnection: close\r\n\r\nx")
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil {
		t.Fatal("no response to timed out body", err)
	}
	cancel()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestClientDisconnectReleasesSlot(t *testing.T) {
	app := New()
	app.Use(ConcurrencyLimit(1))
	entered, exited := make(chan bool, 1), make(chan bool, 1)
	app.GET("/wait", func(c *Context) error { entered <- true; <-c.Context().Done(); exited <- true; return nil })
	app.GET("/fast", func(c *Context) error { return c.String(200, "ok") })
	server := httptest.NewServer(app)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/wait", nil)
	done := make(chan error, 1)
	go func() {
		r, err := server.Client().Do(req)
		if err == nil {
			r.Body.Close()
		}
		done <- err
	}()
	await(t, entered)
	cancel()
	if err := await(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	await(t, exited)
	deadline := time.Now().Add(time.Second)
	for {
		r, err := server.Client().Get(server.URL + "/fast")
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slot not released after client cancellation")
		}
		time.Sleep(time.Millisecond)
	}
}
