package graft

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

// HTTPError is a public HTTP status and message; it must not contain private details.
type HTTPError struct {
	Code    int
	Message string
}

// Error implements error.
func (e *HTTPError) Error() string { return e.Message }

// NewHTTPError creates an HTTP error. Status must be in the 400–599 range.
func NewHTTPError(code int, message string) *HTTPError {
	if code < 400 || code > 599 {
		panic("graft: HTTP error status must be 400–599")
	}
	return &HTTPError{Code: code, Message: message}
}

// NotFound creates a 404 error.
func NotFound(message string) *HTTPError { return NewHTTPError(http.StatusNotFound, message) }

// ErrorHandler renders a handler error. It must not panic.
type ErrorHandler func(*Context, error)

// DefaultErrorHandler exposes HTTPError messages, logs other errors and returns a generic 500.
// Responses that have already started are not overwritten.
func DefaultErrorHandler(c *Context, err error) {
	status, message := http.StatusInternalServerError, "internal server error"
	var he *HTTPError
	if errors.As(err, &he) && he.Code >= 400 && he.Code <= 599 {
		status, message = he.Code, he.Message
	} else {
		slog.ErrorContext(c.Context(), "request error", "error", err, "request_id", c.Response().Header().Get("X-Request-ID"))
	}
	if committed(c.response) {
		return
	}
	_ = c.JSON(status, map[string]string{"error": message})
}

type errorHandlerKey struct{}

func withErrorHandler(r *http.Request, h ErrorHandler) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), errorHandlerKey{}, h))
}
func handleError(w http.ResponseWriter, r *http.Request, err error) {
	h, _ := r.Context().Value(errorHandlerKey{}).(ErrorHandler)
	if h == nil {
		h = DefaultErrorHandler
	}
	h(&Context{request: r, response: w}, err)
}
