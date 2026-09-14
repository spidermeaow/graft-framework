package graft

import (
	"encoding/json"
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
