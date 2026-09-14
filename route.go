package graft

import (
	"encoding/json"
	"strconv"

	"github.com/spidermeaow/graft-framework/openapi"
)

type routeDoc struct {
	method      string
	path        string
	Summary     string                 `json:"summary,omitempty"`
	Description string                 `json:"description,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Parameters  []parameterDoc         `json:"parameters,omitempty"`
	RequestBody *bodyDoc               `json:"requestBody,omitempty"`
	Responses   map[string]responseDoc `json:"responses"`
	Security    []map[string][]string  `json:"security,omitempty"`
}
type parameterDoc struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required"`
	Schema   openapi.Schema `json:"schema"`
}
type mediaDoc struct {
	Schema openapi.Schema `json:"schema"`
}
type bodyDoc struct {
	Required bool                `json:"required"`
	Content  map[string]mediaDoc `json:"content"`
}
type responseDoc struct {
	Description string              `json:"description"`
	Content     map[string]mediaDoc `json:"content,omitempty"`
}

// RouteOption adds explicit documentation metadata to a route.
type RouteOption func(*routeDoc)

// Summary documents an operation's short summary.
func Summary(text string) RouteOption { return func(r *routeDoc) { r.Summary = text } }

// Description documents an operation in more detail.
func Description(text string) RouteOption { return func(r *routeDoc) { r.Description = text } }

// Tag appends an operation tag.
func Tag(text string) RouteOption { return func(r *routeDoc) { r.Tags = append(r.Tags, text) } }

// Security marks an operation as requiring a named OpenAPI security scheme.
func Security(scheme string) RouteOption {
	return func(r *routeDoc) { r.Security = append(r.Security, map[string][]string{scheme: {}}) }
}

// RequestBody documents a required JSON request body.
func RequestBody(schema openapi.Schema) RouteOption {
	return func(r *routeDoc) {
		r.RequestBody = &bodyDoc{Required: true, Content: map[string]mediaDoc{"application/json": {Schema: schema}}}
	}
}

// Response documents a JSON response. A zero Schema documents a bodyless response.
func Response(status int, description string, schema openapi.Schema) RouteOption {
	if status < 100 || status > 599 {
		panic("graft: invalid response status")
	}
	return func(r *routeDoc) {
		response := responseDoc{Description: description}
		if schema.Type != "" {
			response.Content = map[string]mediaDoc{"application/json": {Schema: schema}}
		}
		r.Responses[strconv.Itoa(status)] = response
	}
}

// QueryParameter documents a query parameter.
func QueryParameter(name string, required bool, schema openapi.Schema) RouteOption {
	return func(r *routeDoc) {
		r.Parameters = append(r.Parameters, parameterDoc{Name: name, In: "query", Required: required, Schema: schema})
	}
}

// PathParameter overrides an automatically documented string path parameter.
func PathParameter(name string, schema openapi.Schema) RouteOption {
	return func(r *routeDoc) {
		r.Parameters = append(r.Parameters, parameterDoc{Name: name, In: "path", Required: true, Schema: schema})
	}
}

func newRouteDoc(method, path string, options []RouteOption) routeDoc {
	r := routeDoc{method: method, path: path, Responses: map[string]responseDoc{}}
	for _, option := range options {
		option(&r)
	}
	if len(r.Responses) == 0 {
		r.Responses["default"] = responseDoc{Description: "Response"}
	}
	// Detach mutable schema maps and slices from the caller's configuration.
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	var snapshot routeDoc
	if err = json.Unmarshal(b, &snapshot); err != nil {
		panic(err)
	}
	snapshot.method, snapshot.path = method, path
	return snapshot
}
