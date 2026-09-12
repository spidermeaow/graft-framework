# Machines API

A SQL-first example using database/sql, PostgreSQL and Graft. No ORM or enforced
business architecture. Routes live in cmd/api/main.go. This example shares the
repository's Go module and uses the locally built Graft CLI.

Build the CLI from the repository root:

```sh
go build -o bin/graft ./cmd/graft
cd examples/production-api
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/machines?sslmode=disable'
../../bin/graft migrate
../../bin/graft dev
```

Create the `machines` database first. The user needs DDL privileges for migrations.
On Windows build `bin/graft.exe`, then use `../../bin/graft.exe` and set the variable
with `$env:DATABASE_URL='...'`. Alternatively copy `.env.example` to `.env` and edit
it. The application and migration CLI load `.env` from the working directory;
existing shell environment variables take precedence.

```sh
curl -X POST localhost:8080/api/machines \
  -H 'Content-Type: application/json' -d '{"name":"press-01"}'
curl localhost:8080/api/machines
curl localhost:8080/api/machines/1
curl localhost:8080/health
```

`/swagger` includes an interactive API reference with request/response schemas.
List is bounded to 100 results (optional `?limit=1..100`), IDs are validated,
duplicate names return 409, missing machines return 404. SQL parameters are bound.

```sh
../../bin/graft publish --target linux-x64
```

Copy `publish/production-api` to the server, configure DATABASE_URL and APP_PORT,
and run it. Run migrations separately as a deployment step. The binary does not
migrate automatically. Use a TLS-terminating reverse proxy or your own http.Server
for TLS. Authentication and authorization belong to the application; this sample
is intended for local evaluation and should not be exposed as a public write API.
