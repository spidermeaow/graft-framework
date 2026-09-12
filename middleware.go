package graft

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func track(w http.ResponseWriter) *responseWriter {
	if tracked, ok := w.(*responseWriter); ok {
		return tracked
	}
	return &responseWriter{ResponseWriter: w}
}
func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 || status == 101 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func committed(w http.ResponseWriter) bool {
	for {
		if rw, ok := w.(*responseWriter); ok && rw.status != 0 {
			return true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = u.Unwrap()
	}
}

// Recovery catches handler panics and uses the centralized error handler.
// http.ErrAbortHandler retains net/http's connection-abort semantics.
func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					if value == http.ErrAbortHandler {
						panic(value)
					}
					slog.ErrorContext(r.Context(), "request panic", "panic_type", fmt.Sprintf("%T", value), "request_id", w.Header().Get("X-Request-ID"), "stack", string(debug.Stack()))
					if committed(w) {
						panic(http.ErrAbortHandler)
					}
					handleError(w, r, NewHTTPError(500, "internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID generates a server-owned random ID; untrusted incoming IDs are not reused.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", rand.Text())
			next.ServeHTTP(w, r)
		})
	}
}

// Logger records structured request data using slog. With no argument it uses slog.Default.
func Logger(loggers ...*slog.Logger) Middleware {
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rw := track(w)
			defer func() {
				status := rw.status
				if status == 0 {
					status = 200
				}
				logger.InfoContext(r.Context(), "request", "request_id", rw.Header().Get("X-Request-ID"), "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(started))
			}()
			next.ServeHTTP(rw, r)
		})
	}
}
