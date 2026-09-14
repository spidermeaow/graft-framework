// Package api contains small helpers for consistent HTTP API responses.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/spidermeaow/graft-framework"
)

// Page is a bounded offset pagination request.
type Page struct{ Limit, Offset int }

// ParsePage reads limit and offset with explicit defaults and a maximum limit.
func ParsePage(r *http.Request, defaultLimit, maxLimit int) (Page, error) {
	if defaultLimit < 1 || maxLimit < defaultLimit {
		return Page{}, errors.New("api: invalid pagination limits")
	}
	p := Page{Limit: defaultLimit}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxLimit {
			return Page{}, graft.NewHTTPError(400, "invalid limit")
		}
		p.Limit = value
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return Page{}, graft.NewHTTPError(400, "invalid offset")
		}
		p.Offset = value
	}
	return p, nil
}

// Problem is a minimal RFC 9457 problem detail body.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// WriteProblem writes a standards-based problem response.
func WriteProblem(c *graft.Context, status int, detail string) error {
	if status < 400 || status > 599 {
		return errors.New("api: problem status must be 400-599")
	}
	c.Response().Header().Set("Content-Type", "application/problem+json")
	data, err := json.Marshal(Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail})
	if err != nil {
		return err
	}
	c.Response().WriteHeader(status)
	_, err = c.Response().Write(append(data, '\n'))
	return err
}
