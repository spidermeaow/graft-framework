package graft

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/spidermeaow/graft-framework/openapi"
)

// OpenAPIMetadata is documentation contributed by a route, handler, middleware,
// or external package. It is copied at route registration.
type OpenAPIMetadata struct {
	Summary, Description, OperationID string
	Tags                              []string
	Parameters                        []OpenAPIParameter
	RequestBody                       *OpenAPIRequestBody
	Responses                         map[int]OpenAPIResponse
	Security                          []map[string][]string
	SecuritySchemes                   map[string]map[string]any
	Schemas                           map[string]openapi.Schema
	ComponentResponses                map[string]OpenAPIResponse
	ComponentParameters               map[string]OpenAPIParameter
	Deprecated                        bool
}

type OpenAPIParameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required"`
	Schema   openapi.Schema `json:"schema"`
}
type OpenAPIRequestBody struct {
	Required bool `json:"required"`
	Content  map[string]openapi.Schema
}
type OpenAPIResponse struct {
	Description string `json:"description"`
	Content     map[string]openapi.Schema
}

// OpenAPIMetadataProvider lets packages provide route documentation without
// coupling the generator to their implementation.
type OpenAPIMetadataProvider interface{ OpenAPIMetadata() OpenAPIMetadata }

// StaticMetadata adapts a metadata value to OpenAPIMetadataProvider.
type StaticMetadata OpenAPIMetadata

func (m StaticMetadata) OpenAPIMetadata() OpenAPIMetadata { return OpenAPIMetadata(m) }

// DocumentedMiddleware combines runtime behavior with a metadata provider.
type DocumentedMiddleware interface {
	Middleware() Middleware
	OpenAPIMetadataProvider
}

type documentedMiddleware struct {
	run      Middleware
	provider OpenAPIMetadataProvider
}

func (d documentedMiddleware) Middleware() Middleware           { return d.run }
func (d documentedMiddleware) OpenAPIMetadata() OpenAPIMetadata { return d.provider.OpenAPIMetadata() }

// Document combines an existing net/http-compatible middleware with a metadata
// provider, including one implemented by an external package.
func Document(run Middleware, provider OpenAPIMetadataProvider) DocumentedMiddleware {
	if run == nil || provider == nil {
		panic("graft: documented middleware requires runtime and metadata")
	}
	return documentedMiddleware{run: run, provider: provider}
}

// With adds a documented middleware to one route.
func With(component DocumentedMiddleware) RouteOption {
	if component == nil {
		panic("graft: nil documented middleware")
	}
	return func(r *routeDoc) {
		r.middleware = append(r.middleware, component.Middleware())
		r.apply(component.OpenAPIMetadata())
	}
}

// Metadata adds route or handler metadata supplied by an external provider.
func Metadata(provider OpenAPIMetadataProvider) RouteOption {
	if provider == nil {
		panic("graft: nil OpenAPI metadata provider")
	}
	return func(r *routeDoc) { r.apply(provider.OpenAPIMetadata()) }
}

// SecurityScheme registers a named, arbitrary OpenAPI security scheme for a route.
// The generator does not interpret the scheme, so OAuth2, cookies and custom
// drivers can use the same mechanism.
func SecurityScheme(name string, scheme map[string]any) RouteOption {
	return func(r *routeDoc) { r.apply(OpenAPIMetadata{SecuritySchemes: map[string]map[string]any{name: scheme}}) }
}

// HeaderParameter documents a request header.
func HeaderParameter(name string, required bool, schema openapi.Schema) RouteOption {
	return func(r *routeDoc) {
		r.Parameters = append(r.Parameters, parameterDoc{Name: name, In: "header", Required: required, Schema: schema})
	}
}

// OperationID assigns a stable operation identifier.
func OperationID(id string) RouteOption { return func(r *routeDoc) { r.OperationID = id } }

// Deprecated marks an operation as deprecated.
func Deprecated() RouteOption { return func(r *routeDoc) { r.Deprecated = true } }

// RequestContent documents any media type, including multipart/form-data.
func RequestContent(mediaType string, required bool, schema openapi.Schema) RouteOption {
	return func(r *routeDoc) {
		if r.RequestBody == nil {
			r.RequestBody = &bodyDoc{Content: map[string]mediaDoc{}}
		}
		r.RequestBody.Required = r.RequestBody.Required || required
		r.RequestBody.Content[mediaType] = mediaDoc{Schema: schema}
	}
}

// ResponseReference uses a response registered in components.responses.
func ResponseReference(status int, name string) RouteOption {
	if status < 100 || status > 599 {
		panic("graft: invalid response status")
	}
	return func(r *routeDoc) {
		r.Responses[strconv.Itoa(status)] = responseDoc{Ref: "#/components/responses/" + name}
	}
}

// ParameterReference uses a parameter registered in components.parameters.
func ParameterReference(name string) RouteOption {
	return func(r *routeDoc) {
		r.Parameters = append(r.Parameters, parameterDoc{Ref: "#/components/parameters/" + name})
	}
}

func (r *routeDoc) apply(m OpenAPIMetadata) {
	if m.Summary != "" {
		r.Summary = m.Summary
	}
	if m.Description != "" {
		r.Description = m.Description
	}
	if m.OperationID != "" {
		r.OperationID = m.OperationID
	}
	if m.Deprecated {
		r.Deprecated = true
	}
	for _, tag := range m.Tags {
		if !hasString(r.Tags, tag) {
			r.Tags = append(r.Tags, tag)
		}
	}
	for _, p := range m.Parameters {
		newParam := parameterDoc{Name: p.Name, In: p.In, Required: p.Required, Schema: p.Schema}
		found := false
		for i := range r.Parameters {
			if r.Parameters[i].Name == p.Name && r.Parameters[i].In == p.In {
				if !reflect.DeepEqual(r.Parameters[i], newParam) {
					r.metadataErr = fmt.Errorf("conflicting parameter %s in %s", p.Name, p.In)
				}
				found = true
				break
			}
		}
		if !found {
			r.Parameters = append(r.Parameters, newParam)
		}
	}
	if m.RequestBody != nil {
		if r.RequestBody == nil {
			r.RequestBody = &bodyDoc{Content: map[string]mediaDoc{}}
		}
		r.RequestBody.Required = r.RequestBody.Required || m.RequestBody.Required
		for media, schema := range m.RequestBody.Content {
			if previous, ok := r.RequestBody.Content[media]; ok && !reflect.DeepEqual(previous.Schema, schema) {
				r.metadataErr = fmt.Errorf("conflicting request body for %s", media)
			}
			r.RequestBody.Content[media] = mediaDoc{Schema: schema}
		}
	}
	if r.Responses == nil {
		r.Responses = map[string]responseDoc{}
	}
	for status, response := range m.Responses {
		if status < 100 || status > 599 {
			r.metadataErr = fmt.Errorf("invalid response status %d", status)
			continue
		}
		item := responseDoc{Description: response.Description}
		if len(response.Content) > 0 {
			item.Content = map[string]mediaDoc{}
			for media, schema := range response.Content {
				item.Content[media] = mediaDoc{Schema: schema}
			}
		}
		key := strconv.Itoa(status)
		if previous, ok := r.Responses[key]; ok {
			if previous.Description == item.Description && reflect.DeepEqual(previous.Content, item.Content) {
				continue
			}
			// Explicit route options may override middleware response descriptions.
			continue
		}
		r.Responses[key] = item
	}
	r.Security = mergeSecurity(r.Security, m.Security)
	if r.components.SecuritySchemes == nil {
		r.components.SecuritySchemes = map[string]map[string]any{}
	}
	for name, scheme := range m.SecuritySchemes {
		if previous, exists := r.components.SecuritySchemes[name]; exists && !reflect.DeepEqual(previous, scheme) {
			r.metadataErr = fmt.Errorf("conflicting security scheme %q", name)
		}
		r.components.SecuritySchemes[name] = scheme
	}
	if r.components.Schemas == nil {
		r.components.Schemas = map[string]openapi.Schema{}
	}
	for name, schema := range m.Schemas {
		if previous, exists := r.components.Schemas[name]; exists && !reflect.DeepEqual(previous, schema) {
			r.metadataErr = fmt.Errorf("conflicting schema %q", name)
		}
		r.components.Schemas[name] = schema
	}
	if r.components.ComponentResponses == nil {
		r.components.ComponentResponses = map[string]OpenAPIResponse{}
	}
	for name, response := range m.ComponentResponses {
		if previous, exists := r.components.ComponentResponses[name]; exists && !reflect.DeepEqual(previous, response) {
			r.metadataErr = fmt.Errorf("conflicting response component %q", name)
		}
		r.components.ComponentResponses[name] = response
	}
	if r.components.ComponentParameters == nil {
		r.components.ComponentParameters = map[string]OpenAPIParameter{}
	}
	for name, parameter := range m.ComponentParameters {
		if previous, exists := r.components.ComponentParameters[name]; exists && !reflect.DeepEqual(previous, parameter) {
			r.metadataErr = fmt.Errorf("conflicting parameter component %q", name)
		}
		r.components.ComponentParameters[name] = parameter
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func mergeSecurity(existing, incoming []map[string][]string) []map[string][]string {
	// Security alternatives within one provider are OR; requirements contributed
	// by different middleware are AND, represented by a Cartesian product.
	if len(incoming) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return uniqueSecurity(incoming)
	}
	var merged []map[string][]string
	for _, left := range existing {
		for _, right := range incoming {
			combined := map[string][]string{}
			for name, scopes := range left {
				combined[name] = append([]string{}, scopes...)
			}
			for name, scopes := range right {
				for _, scope := range scopes {
					if !hasString(combined[name], scope) {
						combined[name] = append(combined[name], scope)
					}
				}
				if _, exists := combined[name]; !exists {
					combined[name] = []string{}
				}
			}
			merged = append(merged, combined)
		}
	}
	return uniqueSecurity(merged)
}

func uniqueSecurity(requirements []map[string][]string) []map[string][]string {
	unique := make([]map[string][]string, 0, len(requirements))
	for _, requirement := range requirements {
		normalized := map[string][]string{}
		for name, scopes := range requirement {
			normalized[name] = append([]string{}, scopes...)
		}
		seen := false
		for _, previous := range unique {
			if reflect.DeepEqual(previous, normalized) {
				seen = true
				break
			}
		}
		if !seen {
			unique = append(unique, normalized)
		}
	}
	return unique
}

func legacySecurityScheme(name string) map[string]any {
	switch name {
	case "BearerAuth":
		return map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}
	case "ApiKeyAuth":
		return map[string]any{"type": "apiKey", "in": "header", "name": "X-API-Key"}
	default:
		return nil
	}
}

func cloneMetadata(m OpenAPIMetadata) OpenAPIMetadata {
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	var snapshot OpenAPIMetadata
	if err := json.Unmarshal(data, &snapshot); err != nil {
		panic(err)
	}
	return snapshot
}

// MiddlewareStatus describes a middleware-generated error response.
func MiddlewareStatus(code int, description string) OpenAPIMetadata {
	return OpenAPIMetadata{Responses: map[int]OpenAPIResponse{code: {Description: description}}}
}
