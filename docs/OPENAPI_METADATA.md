# Context-aware OpenAPI metadata

Graft now builds each OpenAPI operation from metadata contributed by app-wide
middleware, group middleware, route middleware, handler metadata, and explicit
route options. The generator reads the resulting metadata; it never checks a
middleware's concrete Go type. Routes without new metadata retain their previous
OpenAPI shape and runtime behavior.

## Attach metadata to a route

Existing options such as `graft.Summary`, `graft.Tag`, `graft.RequestBody` and
`graft.Response` still work. New options include `HeaderParameter`,
`RequestContent`, `OperationID`, `Deprecated`, `SecurityScheme`, and `Metadata`.
Use `Metadata` when a handler or another package supplies several fields:

```go
type orderDocs struct{}
func (orderDocs) OpenAPIMetadata() graft.OpenAPIMetadata {
    return graft.OpenAPIMetadata{
        Summary: "Create order",
        OperationID: "createOrder",
        Tags: []string{"Orders"},
        RequestBody: &graft.OpenAPIRequestBody{
            Required: true,
            Content: map[string]openapi.Schema{
                "application/json": {Type: "object"},
            },
        },
        Responses: map[int]graft.OpenAPIResponse{
            201: {Description: "Created"},
        },
    }
}

app.POST("/orders", createOrder, graft.Metadata(orderDocs{}))
```

The metadata structs also support query/path/header parameters, arbitrary
content types such as `multipart/form-data`, security requirements, security
schemes, shared schemas, shared responses and shared parameters. `openapi.Schema`
supports `$ref` for component schemas.
Use `graft.ResponseReference(status, name)` and `graft.ParameterReference(name)`
to reuse registered response and parameter components.

## Document a custom middleware

A `graft.Middleware` is a Go function and cannot implement an interface itself.
Wrap it with `graft.Document(runtime, provider)`, then use it on one route with
`graft.With(...)`, on a group with `group.UseDocumented(...)`, or app-wide with
`app.UseDocumented(...)`. App-wide documented middleware must be registered before
routes because app-wide runtime middleware also affects every route.

```go
type tenantDocs struct{}
func (tenantDocs) OpenAPIMetadata() graft.OpenAPIMetadata {
    return graft.OpenAPIMetadata{
        Parameters: []graft.OpenAPIParameter{{
            Name: "X-Tenant", In: "header", Required: true,
            Schema: openapi.Schema{Type: "string"},
        }},
        Responses: map[int]graft.OpenAPIResponse{
            400: {Description: "Missing tenant"},
        },
    }
}

tenant := graft.Document(tenantMiddleware(), tenantDocs{})
app.GET("/example", exampleHandler, graft.With(tenant))
```

No Graft core change is needed for this provider. Existing undocumented
middleware continues to work with `app.Use`, `group.Use`, and `Group(prefix, ...)`.

## Authentication and other drivers

Built-in authenticators declare their scheme. `auth.Required` combines their
runtime middleware, a 401 response and that scheme in one component:

```go
bearer := auth.JWT(auth.JWTConfig{
    Issuer: "https://issuer.example", Audience: "my-api",
    HMACSecret: secret,
})
private := app.Group("/api")
private.UseDocumented(auth.Required(bearer), auth.RequiredRole("admin"))
private.GET("/me", meHandler)
```

The generated operation has `security: [{BearerAuth: []}]` and 401/403
responses. Swagger UI shows Authorize and sends the configured Bearer token.
For an API key, use `auth.Required(auth.APIKey(key, principal))`; it declares
`ApiKeyAuth` in the `X-API-Key` header. Both use the same metadata mechanism.
`graft.DocumentedRateLimit(...)` contributes a 429 response.

An external cookie or OAuth2 driver can provide its own scheme map without a
change to the generator:

```go
graft.OpenAPIMetadata{
    Security: []map[string][]string{{"SessionCookie": {}}},
    SecuritySchemes: map[string]map[string]any{
        "SessionCookie": {"type": "apiKey", "in": "cookie", "name": "session"},
    },
}
```

For a route without a middleware provider, `graft.Security("BearerAuth")` and
`graft.Security("ApiKeyAuth")` remain compatible. A custom security name needs a
matching `SecurityScheme` or provider registration. Adding security metadata
alone never enforces authentication at runtime.

## Merge and validation

The merge order is app-wide, group, route/handler providers and route options.
Explicit route options can override scalar fields and response descriptions.
Tags, identical parameters, responses and component definitions are deduplicated.
Conflicting component definitions or request/parameter schemas cause `OpenAPI`
to return an error. The first middleware response for a status wins unless an
explicit route `Response` option overrides it. Multiple middleware security
requirements are combined with AND; alternatives declared within one provider's
`Security` slice remain OR alternatives. A security requirement must reference a
registered scheme. Metadata is snapshotted when the route is registered, so later
changes to provider maps do not silently alter the document.
