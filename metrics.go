package graft

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics keeps bounded, process-local counters. It never labels by URL, user,
// request ID, or other unbounded client input. Serve Handler on a private listener.
type Metrics struct {
	histMu   sync.Mutex
	inFlight atomic.Int64
	total    atomic.Uint64
	rejected atomic.Uint64
	nanos    atomic.Uint64
	statuses [600]atomic.Uint64
	buckets  [12]atomic.Uint64
}

type metricsKey struct{}

var durationBounds = [...]float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// MetricsSnapshot contains a point-in-time view; fields are independently atomic.
type MetricsSnapshot struct {
	Requests        uint64  `json:"requests"`
	InFlight        int64   `json:"in_flight"`
	Rejected        uint64  `json:"rejected"`
	DurationSeconds float64 `json:"duration_seconds"`
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{m.total.Load(), m.inFlight.Load(), m.rejected.Load(), float64(m.nanos.Load()) / 1e9}
}

func (m *Metrics) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rw := track(w)
			m.inFlight.Add(1)
			completed := false
			defer func() {
				elapsed := time.Since(started)
				status := rw.status
				if status == 0 {
					if completed {
						status = 200
					} else {
						status = 500
					}
				}
				if status >= 0 && status < len(m.statuses) {
					m.statuses[status].Add(1)
				}
				m.histMu.Lock()
				for i, bound := range durationBounds {
					if elapsed.Seconds() <= bound {
						m.buckets[i].Add(1)
					}
				}
				m.nanos.Add(uint64(elapsed))
				m.total.Add(1)
				m.histMu.Unlock()
				m.inFlight.Add(-1)
			}()
			next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), metricsKey{}, m)))
			completed = true
		})
	}
}

// Handler exposes Prometheus text metrics including Go heap and goroutine counts.
// It is not automatically mounted on App and has no authentication of its own.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		fmt.Fprintf(w, "# TYPE graft_requests_total counter\ngraft_requests_total %d\n# TYPE graft_in_flight gauge\ngraft_in_flight %d\n# TYPE graft_rejected_total counter\ngraft_rejected_total %d\n", m.total.Load(), m.inFlight.Load(), m.rejected.Load())
		fmt.Fprintln(w, "# TYPE graft_responses_total counter")
		for code := 100; code < 600; code++ {
			if n := m.statuses[code].Load(); n > 0 {
				fmt.Fprintf(w, "graft_responses_total{status=\"%d\"} %d\n", code, n)
			}
		}
		m.histMu.Lock()
		count, nanos := m.total.Load(), m.nanos.Load()
		var buckets [12]uint64
		for i := range buckets {
			buckets[i] = m.buckets[i].Load()
		}
		m.histMu.Unlock()
		fmt.Fprintln(w, "# TYPE graft_request_duration_seconds histogram")
		for i, bound := range durationBounds {
			fmt.Fprintf(w, "graft_request_duration_seconds_bucket{le=\"%g\"} %d\n", bound, buckets[i])
		}
		fmt.Fprintf(w, "graft_request_duration_seconds_bucket{le=\"+Inf\"} %d\ngraft_request_duration_seconds_count %d\ngraft_request_duration_seconds_sum %g\n", count, count, float64(nanos)/1e9)
		fmt.Fprintf(w, "# TYPE graft_go_heap_alloc_bytes gauge\ngraft_go_heap_alloc_bytes %d\n# TYPE graft_go_heap_sys_bytes gauge\ngraft_go_heap_sys_bytes %d\n# TYPE graft_go_goroutines gauge\ngraft_go_goroutines %d\n", mem.HeapAlloc, mem.HeapSys, runtime.NumGoroutine())
	})
}
