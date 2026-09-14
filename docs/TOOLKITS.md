# Graft Toolkits

Toolkits are optional packages. Import only the ones your service needs. They
use the existing `graft.Middleware`, `graft.Context`, and `graft.RouteOption` APIs.

## Authentication

`toolkit/auth` verifies API keys or signed JWT access tokens and attaches a
`Principal` to the request. Authorization checks return 401 for missing identity
and 403 for insufficient permission. API key values are hashed before storage in
the authenticator and compared in constant time.

```go
keyAuth := auth.APIKey(os.Getenv("API_KEY"), auth.Principal{
    Subject: "service-a", Permissions: []string{"items:read"},
})
api := app.Group("/api")
api.UseDocumented(auth.Required(keyAuth), auth.RequiredPermission("items:read"))
api.GET("/items", listItems)
```

For JWT, use `auth.JWT(auth.JWTConfig{Issuer: ..., Audience: ...,
HMACSecret: ...})` with a secret of at least 32 bytes, or supply `RSAPublicKey`.
The verifier checks signature, algorithm, issuer, audience, subject, expiration,
and not-before. Obtain and rotate public keys in your application until JWKS
support is added. `auth.Optional` accepts absent credentials but rejects invalid
ones. `auth.Required` attaches security and 401 metadata automatically; see the
[OpenAPI metadata guide](OPENAPI_METADATA.md) for group, route, and custom drivers.

## Request validation

`toolkit/validate` combines Graft's strict JSON binder with simple DTO tags:

```go
type CreateItem struct {
    Name string `json:"name" validate:"required,min=3,max=80"`
}
func createItem(c *graft.Context) error {
    var input CreateItem
    if err := validate.Bind(c, &input); err != nil { return err }
    return c.JSON(201, input)
}
```

`min` and `max` count bytes for strings, element counts for slices/maps, and
numeric values for integers. Unsupported rules fail validation; use a custom
validator for domain-specific checks.

## API and tests

`toolkit/api` provides `ParsePage(request, defaultLimit, maxLimit)` for bounded
`limit` and `offset` query parameters and `WriteProblem` for RFC 9457 problem
responses. `toolkit/testkit` provides `JSON` and `DecodeJSON` for HTTP handler
tests. Neither toolkit changes existing Graft response formats automatically.
