# v0.1 implementation validation

Validated locally on 2026-09-12 with Go 1.26.6, Windows/amd64 and an isolated
PostgreSQL 18 cluster. This records observed checks, not a guarantee for every
deployment environment.

Passed:

- `go test ./...`, including live PostgreSQL integration tests using
  GRAFT_TEST_DATABASE_URL.
- `go test -race ./...`, including PostgreSQL and generated-project tests.
- `go vet ./...`, `go mod verify`, and gofmt checks.
- `govulncheck ./...` (golang.org/x/vuln v1.8.0): no vulnerabilities found.
- Generated a new project, built its executable, started it, and checked `/`,
  `/swagger` and `/openapi.json` over HTTP.
- CLI status → migrate → repeated migrate → status → step rollback → status
  against a fresh test database. Repeated migration did not execute SQL twice.
- PostgreSQL tests cover multi-statement migrations, batch/step rollback, failed
  Up and Down transactions, checksum mismatch, concurrent locking, cancellation
  and lock cleanup after a panic.
- Machines API end-to-end test covers migration, create/list/get, validation,
  duplicate names, missing records, readiness, OpenAPI and rollback.
- Published the PostgreSQL example for linux-x64, linux-arm64, windows-x64,
  windows-arm64, darwin-x64 and darwin-arm64. All compilation commands succeeded.
- Started the published Windows/x64 binary and opened the embedded Swagger UI in
  a browser. Routes and schemas rendered; Try it out on GET /api/machines returned
  HTTP 200 and `[]` from PostgreSQL.
- `go list -deps .` confirmed the HTTP package's Go dependencies are only the
  standard library and Graft's own packages.

Cross-compiled Linux/macOS/ARM binaries were not executed on those operating
systems or architectures. GitHub Actions configuration was authored but has not
run on a remote repository. Public module installation needs an actual published
repository and version tag; local-checkout onboarding was tested instead.

Initial benchmarks (Core Ultra 5 325; Windows/amd64):

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Router | 634.8 | 1448 | 15 |
| Middleware (RequestID) | 872.1 | 1528 | 18 |
| JSON response | 1446 | 1872 | 20 |

These benchmarks include httptest recorder allocation and are a regression
baseline, not network throughput or a comparison with other frameworks.
