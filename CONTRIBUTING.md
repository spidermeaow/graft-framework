# Contributing

Use Go 1.26.6+, gofmt, testing and httptest. Keep exported APIs small and documented.
Prefer standard library primitives, explicit composition and errors over panic for
runtime failures. Configuration mistakes may panic like net/http registration does.

Do not add an ORM, DI container or application architecture. Document the reason
for every external dependency in docs/DEPENDENCIES.md and design decisions in ADRs.
SQL stays visible. Runtime migration execution must preserve lock/transaction
semantics, including cancellation and rollback failures.

Before submitting:

```sh
gofmt -w .
go test ./...
go vet ./...
go test -race ./...
```

Set GRAFT_TEST_DATABASE_URL to enable the PostgreSQL integration tests. CI includes
this environment. Run benchmarks when changing request-path behavior, and test
generated projects when changing the CLI. Do not commit bin/, publish/ or .tmp/.

Release preparation includes deciding the repository's license, confirming the
canonical remote/module path, tagging a version, checking generated projects with
that published version, and retaining bundled third-party license notices.
