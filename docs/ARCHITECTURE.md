# Codebase map

Graft is one Go module. The root package is the HTTP framework; optional
toolkits and migration stores live in separate packages. Applications import
only the parts they use.

| Area | Main files | Responsibility |
| --- | --- | --- |
| HTTP core | `app.go`, `context.go`, `route.go`, `server.go` | Routes, request/response helpers, and server lifecycle |
| Runtime middleware | `middleware.go`, `limits.go`, `metrics.go` | Request protection, limits, and metrics |
| OpenAPI | `openapi.go`, `openapi_metadata.go`, `openapi_metadata_merge.go`, `openapi_security.go`, `swagger.go` | Document generation, metadata API and merge rules, embedded UI |
| SQL migrations | `migration/`, `migration/postgres/`, `migration/mysql/` | SQL parsing, execution, history, and recovery |
| CLI | `internal/cli/` | Project generation, build/publish, doctor, and migration commands |
| Optional toolkits | `toolkit/` | Auth, validation, API helpers, and test helpers |

OpenAPI route metadata is frozen when a route is registered. The generator in
`openapi.go` first gathers shared components, then constructs operations and
validates references. The merge policy is in `openapi_metadata_merge.go` and
security combination is in `openapi_security.go`; runtime middleware remains
independent of the generator. See [OpenAPI metadata](OPENAPI_METADATA.md) for the
public extension API.

The migration CLI keeps command coordination in `internal/cli/migration.go`.
Connection selection, local file generation, and dirty-state inspection are in
`migration_connection.go`, `migration_create.go`, and `migration_doctor.go`.
Database execution belongs to the migration stores, not the CLI.

The Windows standalone CLI embeds `internal/frameworkbundle/framework.zip`.
After changing root framework or toolkit source, regenerate it with
`go run ./internal/cmd/bundle` before committing. The release workflow verifies
that the archive matches the checked-in source.
