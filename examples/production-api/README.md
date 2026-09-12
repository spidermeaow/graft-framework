# Machines API

A bounded SQL-first service example with authentication, query deadlines, health
checks and optional private monitoring. It shares the repository's Go module.

```sh
go build -o bin/graft ./cmd/graft
cd examples/production-api
# Copy .env.example to .env and configure DATABASE_URL and two random tokens.
../../bin/graft migrate
../../bin/graft dev
```

Use `bin/graft.exe` on Windows. Go and PostgreSQL are required for development.
Create the database first and use a migration role with DDL privileges. The running
application should use a separate restricted role. Generate distinct random
APP_READ_TOKEN and APP_WRITE_TOKEN values of at least 32 characters; startup fails
without them. Supply credentials through your secret manager in production.

```sh
curl localhost:8080/api/machines -H "Authorization: Bearer $APP_READ_TOKEN"
curl -X POST localhost:8080/api/machines -H "Authorization: Bearer $APP_WRITE_TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"press-01"}'
curl 'localhost:8080/api/machines?limit=100&after_id=0' -H "Authorization: Bearer $APP_READ_TOKEN"
curl localhost:8080/livez
curl localhost:8080/readyz
```

Read credentials cannot write. For pagination, pass the last ID of a page as
`after_id`; each response contains at most 100 machines. IDs and names are bounded,
SQL uses parameters, duplicates return 409 and unknown machines return 404.
This service-token example does not implement multi-user ownership or an IdP.

APP_DOCS=false disables Swagger and OpenAPI. If enabled, all docs/assets require
the same Bearer authentication. A browser UI needs a session-aware auth gateway
or your own browser-compatible middleware; never put a token into a URL.

APP_ADMIN_ADDR=127.0.0.1:9090 optionally exposes `/metrics` privately, including Go
heap/goroutines and DB pool stats. APP_PPROF=true adds private profiling handlers.
The public app never mounts these routes. Liveness ignores DB status; readiness
checks the DB with a 1s deadline and becomes unavailable during drain. `/health`
is retained as a legacy DB health check.

```sh
../../bin/graft publish --target linux-x64
```

Deploy the resulting `bin/production-api` binary with external configuration.
Bind explicitly to an appropriate interface and use a TLS reverse proxy. See
[Production guide](../../docs/PRODUCTION.md) for limits, role permissions,
statement timeouts, monitoring, rate limiting, migrations and rollback.

The defaults are starting points, not measured production capacity. Run the real
service on staging under expected traffic and fault conditions before rollout.
