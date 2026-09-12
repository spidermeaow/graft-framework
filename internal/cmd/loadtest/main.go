// Command loadtest runs a bounded, repeatable local HTTP overload/soak experiment.
// Memory includes both the client and server; it is not a production capacity claim.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spidermeaow/graft-framework"
)

type result struct {
	Phase             string         `json:"phase"`
	Workers           int            `json:"workers"`
	Seconds           float64        `json:"seconds"`
	Requests          uint64         `json:"requests"`
	RequestsPerSecond float64        `json:"requests_per_second"`
	Statuses          map[int]uint64 `json:"statuses"`
	TransportErrors   uint64         `json:"transport_errors"`
	P95Milliseconds   int            `json:"p95_upper_bound_ms"`
	P99Milliseconds   int            `json:"p99_upper_bound_ms"`
	PeakHeapBytes     uint64         `json:"peak_heap_bytes"`
	AfterGCHeapBytes  uint64         `json:"after_gc_heap_bytes"`
	GoroutinesAfter   int            `json:"goroutines_after"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	duration := flag.Duration("duration", 10*time.Second, "duration of each of three phases (e.g. 10m for a soak)")
	workers := flag.Int("workers", 4, "steady/recovery workers")
	limit := flag.Int("limit", 8, "server concurrency limit")
	delay := flag.Duration("delay", 5*time.Millisecond, "simulated cooperative handler work")
	timeout := flag.Duration("timeout", time.Second, "handler deadline")
	bodySize := flag.Int("body-bytes", 1024, "POST body size; set above 1 MiB to exercise rejection")
	responseItems := flag.Int("response-items", 1, "bounded JSON response item count (1..100)")
	flag.Parse()
	if *responseItems < 1 || *responseItems > 100 || *duration <= 0 || *workers < 1 || *workers > 4096 || *limit < 1 || *limit > 1024 || *bodySize < 0 || *bodySize > 2<<20 || *timeout <= 0 || *delay < 0 {
		return fmt.Errorf("invalid load parameters")
	}
	items := make([]map[string]string, *responseItems)
	for i := range items {
		items[i] = map[string]string{"name": strings.Repeat("x", 100), "status": "ready"}
	}
	app := graft.New()
	metrics := &graft.Metrics{}
	app.Use(metrics.Middleware(), graft.ConcurrencyLimit(*limit), graft.BodyLimit(1<<20), graft.RequestDeadline(*timeout))
	app.POST("/", func(c *graft.Context) error {
		if _, err := io.Copy(io.Discard, c.Request().Body); err != nil {
			return err
		}
		timer := time.NewTimer(*delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-c.Context().Done():
			return c.Context().Err()
		}
		return c.JSON(200, items)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, listener) }()
	defer func() { cancel(); <-done }()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	var results []result
	body := strings.Repeat("x", *bodySize)
	for _, phase := range []struct {
		name    string
		workers int
	}{{"steady", *workers}, {"overload", max(*workers, *limit*4)}, {"recovery", *workers}} {
		results = append(results, measure("http://"+listener.Addr().String(), phase.name, phase.workers, *duration, body))
	}
	if err := enc.Encode(map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "memory_scope": "client and server in one process; heap is not RSS", "limit": *limit, "body_bytes": *bodySize, "response_items": *responseItems, "handler_delay": delay.String(), "handler_deadline": timeout.String(), "results": results, "metrics": metrics.Snapshot()}); err != nil {
		return err
	}
	want := 200
	if *bodySize > 1<<20 {
		want = 413
	} else if *delay > *timeout {
		want = 504
	}
	for _, r := range results {
		if r.TransportErrors != 0 || r.Statuses[want] == 0 {
			return fmt.Errorf("%s: missing expected HTTP %d or transport failures", r.Phase, want)
		}
		for code := range r.Statuses {
			if code != want && code != 503 {
				return fmt.Errorf("%s: unexpected HTTP %d", r.Phase, code)
			}
		}
	}
	return nil
}

func measure(url, phase string, workers int, duration time.Duration, body string) result {
	transport := &http.Transport{MaxIdleConns: workers, MaxIdleConnsPerHost: workers}
	client := &http.Client{Transport: transport, Timeout: 35 * time.Second}
	var statuses [600]atomic.Uint64
	var histogram [60001]atomic.Uint64
	var count, failures, peak atomic.Uint64
	stop := make(chan struct{})
	sampleDone := make(chan struct{})
	go func() {
		defer close(sampleDone)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				for old := peak.Load(); mem.HeapAlloc > old; old = peak.Load() {
					if peak.CompareAndSwap(old, mem.HeapAlloc) {
						break
					}
				}
			}
		}
	}()
	start := time.Now()
	end := start.Add(duration)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(end) {
				started := time.Now()
				response, err := client.Post(url, "application/octet-stream", strings.NewReader(body))
				if err != nil {
					failures.Add(1)
				} else {
					_, readErr := io.Copy(io.Discard, response.Body)
					response.Body.Close()
					if readErr != nil {
						failures.Add(1)
					}
					if response.StatusCode < 600 {
						statuses[response.StatusCode].Add(1)
					}
				}
				histogram[min(60000, time.Since(started).Milliseconds()+1)].Add(1)
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	close(stop)
	<-sampleDone
	transport.CloseIdleConnections()
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	quantile := func(p float64) int {
		target := uint64(float64(count.Load())*p + .999999)
		var n uint64
		for i := range histogram {
			n += histogram[i].Load()
			if n >= target {
				return i
			}
		}
		return 60000
	}
	r := result{Phase: phase, Workers: workers, Seconds: elapsed, Requests: count.Load(), RequestsPerSecond: float64(count.Load()) / elapsed, Statuses: map[int]uint64{}, TransportErrors: failures.Load(), P95Milliseconds: quantile(.95), P99Milliseconds: quantile(.99), PeakHeapBytes: peak.Load(), AfterGCHeapBytes: mem.HeapAlloc, GoroutinesAfter: runtime.NumGoroutine()}
	for i := range statuses {
		if n := statuses[i].Load(); n > 0 {
			r.Statuses[i] = n
		}
	}
	return r
}
