package graft

import (
	"context"
	"sync/atomic"
)

// Health separates process liveness from readiness. Its zero value is not ready.
// ServeWithConfig marks it ready at startup and unready before draining.
type Health struct{ ready atomic.Bool }

func (h *Health) SetReady(ready bool) { h.ready.Store(ready) }

// Liveness is intentionally independent of external services.
func (h *Health) Liveness(c *Context) error { return c.JSON(200, map[string]string{"status": "alive"}) }

// Readiness evaluates checks only while the process is accepting traffic.
// Checks must enforce their own deadlines; private errors are never returned.
func (h *Health) Readiness(checks ...func(context.Context) error) Handler {
	return func(c *Context) error {
		if !h.ready.Load() {
			return NewHTTPError(503, "not ready")
		}
		for _, check := range checks {
			if err := check(c.Context()); err != nil {
				return NewHTTPError(503, "not ready")
			}
		}
		if !h.ready.Load() {
			return NewHTTPError(503, "not ready")
		}
		return c.JSON(200, map[string]string{"status": "ready"})
	}
}
