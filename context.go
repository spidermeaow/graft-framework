package graft

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Context contains only the current HTTP request and response.
type Context struct {
	request  *http.Request
	response http.ResponseWriter
}

// Param reads a ServeMux path parameter.
func (c *Context) Param(name string) string { return c.request.PathValue(name) }

// Query reads the first query parameter value.
func (c *Context) Query(name string) string { return c.request.URL.Query().Get(name) }

// Header reads a request header.
func (c *Context) Header(name string) string { return c.request.Header.Get(name) }

// Request returns the original HTTP request.
func (c *Context) Request() *http.Request { return c.request }

// Response returns the response writer. Use http.ResponseController for streaming.
func (c *Context) Response() http.ResponseWriter { return c.response }

// Context returns the request's cancellation context.
func (c *Context) Context() context.Context { return c.request.Context() }

// Bind decodes exactly one JSON value, limited to 1 MiB, rejecting unknown fields.
// A Content-Type of application/json or application/*+json is required.
func (c *Context) Bind(dst any) error { return c.BindLimit(dst, 1<<20) }

// BindLimit decodes JSON with an explicit maximum request-body size in bytes.
func (c *Context) BindLimit(dst any, limit int64) error {
	if limit <= 0 {
		return errors.New("graft: JSON body limit must be positive")
	}
	media, _, err := mime.ParseMediaType(c.Header("Content-Type"))
	if err != nil || (media != "application/json" && !(strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json"))) {
		return NewHTTPError(415, "Content-Type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.response, c.request.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return bindError(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return bindError(err)
		}
		return NewHTTPError(400, "request body must contain exactly one JSON value")
	}
	return nil
}

func bindError(err error) error {
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		return NewHTTPError(413, "request body too large")
	}
	var invalid *json.InvalidUnmarshalError
	if errors.As(err, &invalid) {
		return err
	}
	return NewHTTPError(400, "invalid JSON request body")
}

// JSON encodes before committing headers, allowing encoding failures to reach the error handler.
func (c *Context) JSON(status int, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.response.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.response.WriteHeader(status)
	_, err = c.response.Write(append(b, '\n'))
	return err
}

// String writes a plain-text response.
func (c *Context) String(status int, text string) error {
	c.response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.response.WriteHeader(status)
	_, err := io.WriteString(c.response, text)
	return err
}
