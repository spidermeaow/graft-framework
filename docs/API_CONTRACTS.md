# Struct-driven API contracts

Graft can infer common OpenAPI 3.0 schemas from Go types. The request or response
struct remains the source of truth, while route options keep the complete contract
visible beside the handler.

```go
type UpdateUserRequest struct {
    Name  string `json:"name" validate:"required,min=3,max=80"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"min=18,max=100"`
}

type UserResponse struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

type ErrorResponse struct {
    Error string `json:"error"`
}

app.PUT(
    "/api/users/{id}",
    updateUser,
    graft.Summary("Update user"),
    graft.Tag("Users"),
    graft.Path[int64]("id"),
    graft.QueryOptional[bool]("sendEmail"),
    graft.Body[UpdateUserRequest](),
    graft.Response[UserResponse](200),
    graft.Response[ErrorResponse](400),
    graft.Response[ErrorResponse](404),
)
```

## Generic route options

- `graft.Body[T]()` documents a required `application/json` body.
- `graft.Response[T](status)` documents an `application/json` response and uses
  Go's standard HTTP status text as its description.
- `graft.ResponseDescription[T](status, description)` sets a custom description.
- `graft.Path[T](name)` documents a required path parameter.
- `graft.Query[T](name)` documents a required query parameter.
- `graft.QueryOptional[T](name)` documents an optional query parameter.
- `graft.SchemaOf[T]()` returns the inferred schema when another metadata API
  needs it.

The inference supports booleans, strings, signed and unsigned integers, floats,
arrays, slices, string-keyed maps, pointers, nested structs, `time.Time`, and
`[]byte`. JSON field names come from `json` tags. `description` or `doc` tags can
add a property description. Unexported fields and `json:"-"` fields are ignored.
Recursive or otherwise unsupported representations fail immediately during route
registration so an incomplete contract is not published silently.

## Validation tags

The inferred schema and `toolkit/validate` share these rules:

| Tag | String | Number | Array/slice | Map | OpenAPI output |
| --- | --- | --- | --- | --- | --- |
| `required` | yes | yes | yes | yes | parent `required` list |
| `email` | yes | no | no | no | `format: email` |
| `min=N` | length | value | item count | property count | matching minimum |
| `max=N` | length | value | item count | property count | matching maximum |

Schema generation documents constraints; it does not execute validation. Use the
same DTO with `validate.Bind` when the handler must enforce those rules:

```go
var input UpdateUserRequest
if err := validate.Bind(c, &input); err != nil {
    return err
}
```

Business rules such as cross-field comparisons or database uniqueness still
belong in application code.

## Manual schemas for advanced cases

Manual schema support remains available. Keep using `openapi.Schema` for polymorphism,
hand-written component references, unusual media types, or precise overrides:

```go
app.POST("/files", upload,
    graft.RequestContent("multipart/form-data", true, uploadSchema),
    graft.ResponseSchema(202, "Upload queued", openapi.Schema{
        Ref: "#/components/schemas/UploadJob",
    }),
)
```

The existing manual APIs `RequestBody`, `PathParameter`, `QueryParameter`,
`HeaderParameter`, and `RequestContent` remain available. `ResponseSchema` is the
manual counterpart to generic `Response[T]`. Code using the former
`Response(status, description, schema)` signature must rename that call to
`ResponseSchema(status, description, schema)`; Go does not support overloading a
generic and non-generic function under the same name.
