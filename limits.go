package graft

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// ConcurrencyLimit admits at most maximum active requests per middleware instance.
// There is no waiting queue. A slot is retained until the handler actually returns,
// including when its context expires. Place this outside RequestDeadline.
func ConcurrencyLimit(maximum int) Middleware {
	if maximum <= 0 {
		panic("graft: concurrency limit must be positive")
	}
	slots := make(chan struct{}, maximum)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				next.ServeHTTP(w, r)
			default:
				reject(w, r, http.StatusServiceUnavailable, "server busy")
			}
		})
	}
}

func reject(w http.ResponseWriter, r *http.Request, status int, message string) {
	if m, ok := r.Context().Value(metricsKey{}).(*Metrics); ok {
		m.rejected.Add(1)
	}
	w.Header().Set("Retry-After", "1")
	handleError(w, r, NewHTTPError(status, message))
}

// BodyLimit limits bytes read even for handlers that do not use Bind. Handlers
// must propagate body read errors. Known oversized bodies are rejected up front.
func BodyLimit(bytes int64) Middleware {
	if bytes <= 0 {
		panic("graft: body limit must be positive")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > bytes {
				handleError(w, r, NewHTTPError(413, "request body too large"))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, bytes)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestDeadline adds a cooperative deadline. It does not spawn a goroutine,
// buffer responses, or forcibly terminate code that ignores context cancellation.
// Use connection timeouts for blocked network I/O and pass Context to downstream
// calls. Responses already committed cannot be replaced with a timeout response.
func RequestDeadline(timeout time.Duration) Middleware {
	if timeout <= 0 {
		panic("graft: request deadline must be positive")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			r = r.WithContext(ctx)
			w = track(w)
			next.ServeHTTP(w, r)
			if ctx.Err() == context.DeadlineExceeded && !committed(w) {
				handleError(w, r, NewHTTPError(504, "request deadline exceeded"))
			}
		})
	}
}

// RateLimit is a process-local token bucket shared by all requests using this
// middleware. It has constant memory use and does not trust client/IP headers.
// Use a gateway/shared store for per-identity limits across multiple instances.
func RateLimit(perSecond float64, burst int) Middleware {
	if perSecond <= 0 || perSecond != perSecond || perSecond > 1e9 || burst <= 0 {
		panic("graft: invalid rate limit")
	}
	var mu sync.Mutex
	tokens, updated := float64(burst), time.Now()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			now := time.Now()
			tokens = min(float64(burst), tokens+now.Sub(updated).Seconds()*perSecond)
			updated = now
			allowed := tokens >= 1
			if allowed {
				tokens--
			}
			mu.Unlock()
			if !allowed {
				reject(w, r, 429, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
