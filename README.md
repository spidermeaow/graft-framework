# Graft

**Write Routes. Migrate. Document. Ship.**

Graft is a lean Go API framework with SQL-first PostgreSQL migrations, built-in
OpenAPI/Swagger documentation, and native deployment. It uses `net/http`,
`http.ServeMux`, `database/sql`, `log/slog`, and the standard Go toolchain.
There is no ORM, DI container, or required application architecture.

## Start from this checkout

Requires Go 1.26.6 or later. PostgreSQL is needed only for database applications
and migration commands.

```sh
go build -o bin/graft ./cmd/graft
./bin/graft new hello-api
cd hello-api
../bin/graft dev
```

Windows PowerShell:

```powershell
go build -o bin/graft.exe ./cmd/graft
./bin/graft.exe new hello-api
cd hello-api
../bin/graft.exe dev
```

Open http://localhost:8080, http://localhost:8080/swagger and
http://localhost:8080/openapi.json. Add the CLI to PATH to use `graft` directly.
Development CLI builds infer this checkout and add an explicit `replace` to the
generated go.mod. Use `--framework /path/to/graft-framework` if the binary was
built with `-trimpath` or moved away from its source.

The public module identity is `github.com/spidermeaow/graft-framework`. After a
version tag has been published, onboarding can use:

```sh
go install github.com/spidermeaow/graft-framework/cmd/graft@latest
graft new hello-api
```

For a different hosting namespace, update the module path and generator imports
before release. `new --module example.com/my-api` names the application module;
`--version` selects a published framework version.

## Write routes

```go
package main

import (
    "log"
    "github.com/spidermeaow/graft-framework"
)

func main() {
    app := graft.New()
    app.Use(graft.RequestID(), graft.Logger(), graft.Recovery())
    app.GET("/ping", func(c *graft.Context) error {
        return c.JSON(200, map[string]string{"message": "pong"})
    }, graft.Summary("Ping"), graft.Tag("Health"))
    app.Group("/api").GET("/users/{id}", func(c *graft.Context) error {
        return c.JSON(200, map[string]string{"id": c.Param("id")})
    })
    app.Docs("My API", "0.1.0")
    if err := app.Run(":8080"); err != nil {
        log.Fatal(err)
    }
}
```

Methods: GET, POST, PUT, PATCH, DELETE and `Handle(method, path, handler)`.
`Group(prefix, middleware...)` supports nesting. Middleware added first runs
first. Configure the app before serving; later registration panics. App itself is
an `http.Handler`, so use it with `httptest` or your own `http.Server`.

ServeMux semantics apply: GET serves HEAD, unsupported methods return 405 with
Allow, unmatched paths return 404, and paths ending in `/` match subtrees.
Use `/{$}` for the exact root. Router 404/405 responses use net/http's plain text;
handler errors use the configured Graft error handler.

### Request and response

`c.Param`, `c.Query` and `c.Header` read request data. `c.Request()`,
`c.Response()` and `c.Context()` expose standard Go primitives. JSON structures
are application-owned; no response envelope is required.

```go
var input struct { Name string `json:"name"` }
if err := c.Bind(&input); err != nil { return err }
if input.Name == "" { return graft.NewHTTPError(400, "name is required") }
return c.JSON(201, input)
```

Bind requires a JSON Content-Type, rejects unknown fields and extra JSON values,
and limits the body to 1 MiB. `BindLimit` sets another byte limit. Validate business
rules explicitly. Return `graft.NotFound("user not found")` for a public 404;
unrecognized errors are logged and rendered as generic 500 responses. Override
`SetErrorHandler` for another error format. Responses already committed are not
rewritten; a panic after a partial response aborts the connection.

Recovery is installed around routing by default, so Logger observes recovered
handler status. An outer recovery also protects middleware. RequestID generates a new
server-owned `X-Request-ID`. For streaming, use `http.NewResponseController` with
`c.Response()`; direct assertions for optional writer interfaces are not required.

`Run` handles SIGINT/SIGTERM and drains requests for up to 10 seconds. Defaults:
5s header timeout, 30s read/write timeouts, 60s idle timeout, 1 MiB headers.
`RunContext` and `Serve(ctx, listener)` support application-owned cancellation.
Use a custom http.Server for TLS or different streaming/timeouts requirements.

## SQL-first migrations

Set DATABASE_URL explicitly; `.env.example` is documentation, not auto-loaded.

```sh
export DATABASE_URL='postgres://user:password@localhost:5432/app?sslmode=disable'
graft make:migration create_users_table
```

PowerShell: `$env:DATABASE_URL='postgres://...'`.
Edit the generated SQL file; empty sections are deliberately rejected:

```sql
-- +graft Up

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(100) NOT NULL
);

-- +graft Down

DROP TABLE users;
```

```sh
graft migrate
graft migrate:status
graft migrate:rollback
graft migrate:rollback --step 2
```

Flags: `--dir` (default migrations), `--timeout` (default 2m), and rollback
`--step`. Flags precede positional arguments, e.g.
`graft make:migration --dir db/migrations create_users_table`.

Positive numeric versions sort numerically. Each migration's SQL and history row
commit together. A failed migration rolls back; earlier migrations in the same
batch remain applied. Retrying skips them. Rollback without `--step` reverts the
latest batch; a positive step reverts that many most-recent migrations.

History is in `public.graft_migrations` with version, name, batch, executed_at,
execution_time (nanoseconds) and checksum. Applied files must not be renamed,
edited, removed, or have their line endings changed. Add a new migration instead.
Missing files, checksum changes, duplicate versions and out-of-order insertion
produce errors before changes are made.

A database advisory lock protects each entire operation, including status.
Concurrent commands fail fast. Use a direct PostgreSQL connection or session-mode
pooler; transaction-pooling proxies cannot preserve session locks. The database
user needs permission to create/use the history table and execute migration SQL.
One database has one Graft migration stream.

Migration file generation uses an exclusive `.graft-create.lock` in its directory.
If a generator is force-killed, remove this lock only after ensuring no generator
is running, then retry. This is separate from the database advisory lock.

Do not put BEGIN/COMMIT/ROLLBACK in migration files: Graft owns transactions.
Nontransactional SQL (such as CREATE INDEX CONCURRENTLY), psql meta-commands,
and COPY FROM STDIN are not supported in v0.1. Migration files are trusted code.
The framework does not run migrations on application startup.

For programmatic use, `migration.Load(fs, dir)` accepts disk or embedded files;
`migration.Runner{Store: postgres.New(db)}` supplies Up, Status and Rollback.
The caller owns `*sql.DB` and its driver. HTTP apps can use database/sql, pgx,
sqlc or any other data-access approach independently of Graft.

## OpenAPI and Swagger

Call `app.Docs("API name", "0.1.0")` to enable docs. Swagger UI assets are embedded
and need no CDN, Node runtime, or separate server. There is no remote validator.
Describe schemas explicitly using `github.com/spidermeaow/graft-framework/openapi`:

```go
schema := openapi.Schema{
    Type: "object",
    Properties: map[string]openapi.Schema{"name": {Type: "string"}},
    Required: []string{"name"},
}
app.POST("/users", createUser,
    graft.Summary("Create user"), graft.Tag("Users"),
    graft.RequestBody(schema), graft.Response(201, "Created", schema),
)
```

Additional metadata: Description, QueryParameter and PathParameter. Path
parameters are discovered as required strings unless overridden. Schemas document
the API; they do not validate request bodies. `app.OpenAPI(title, version)` exports
JSON without serving it. Catch-all `{path...}` routes work for HTTP but are rejected
by OpenAPI generation, since OpenAPI cannot describe them faithfully.
Documentation endpoints currently assume the app is mounted at the origin root.

## Build and publish

```sh
graft build
graft publish --target linux-x64
```

Build writes `bin/<project>` (or `.exe` on Windows). Publish writes
`publish/<project>` with CGO_ENABLED=0, GOOS and GOARCH selected from:

| Target | GOOS | GOARCH |
| --- | --- | --- |
| linux-x64 | linux | amd64 |
| linux-arm64 | linux | arm64 |
| windows-x64 | windows | amd64 |
| windows-arm64 | windows | arm64 |
| darwin-x64 | darwin | amd64 |
| darwin-arm64 | darwin | arm64 |

Use `--output path` to keep builds for multiple targets and `--package` to select a
main package. Default package is ./cmd/api when it exists, otherwise the current
directory. `graft dev` delegates to `go run`; no hot reload in v0.1.
Copy the published binary to the target server, set environment variables and run.
No Graft runtime or Docker is required. Apps using cgo can use normal Go tooling.

See [Machines API](examples/production-api/README.md) for PostgreSQL-backed routes,
migrations, request validation, error handling and full route schemas.

## Quality and design

```sh
go test ./...
go vet ./...
go test -race ./...
go test -run '^$' -bench . -benchmem .
```

PostgreSQL integration tests are enabled with GRAFT_TEST_DATABASE_URL, a URL to a
test server whose user can CREATE DATABASE. Each test creates and drops its own
uniquely named database; it never migrates the database named by the URL.
CI runs these tests against a PostgreSQL service. Without the variable they skip.

See [ADRs](docs/adr), [dependency rationale and upstream licenses](docs/DEPENDENCIES.md),
and [contributing conventions](CONTRIBUTING.md). The core HTTP package has no
external Go dependencies. The CLI/example use one PostgreSQL driver.
