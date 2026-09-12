# v0.2.0-rc.1 local validation

Date: 2026-09-12. Windows/amd64, Go 1.26.6, Intel Core i7-1165G7 (4 cores,
8 logical CPUs), approximately 16 GiB physical RAM. PostgreSQL 18 ran in a newly
initialized isolated cluster on 127.0.0.1:19487. No production database was used.

## Verified

- `go test -race ./...` passed with GRAFT_TEST_DATABASE_URL set, including actual
  PostgreSQL migrations, lock contention, pool waiting and recovery.
- `go vet ./...`, `go mod verify`, and govulncheck v1.8.0 passed; no Go vulnerability
  findings were reported for the local build target.
- `go run ./internal/cmd/check-swagger` verified pinned asset hashes and found no
  OSV advisories for the checked swagger-ui-dist/swagger-ui 5.32.15 package versions.
- Generated v0.2 module-proxy app and local-source app compiled. Legacy scaffold
  selection remains available for explicitly requested v0.1 framework versions.
- Tests cover full/unknown-length oversized bodies, rejected overload, panic slot
  release, expired handlers retaining admission slots, earlier parent deadlines,
  client disconnect, slow request bodies, protected docs/assets, rate rejection,
  readiness withdrawal, ordered cleanup and forced-shutdown cancellation.
- Machines tests cover read/write authorization, keyset pagination, DB lock/pool
  deadline failures followed by successful requests, additive schema compatibility,
  legacy DB health, readiness failure and independent liveness.
- A compiled Machines binary was started on isolated local ports: readiness 200;
  anonymous API access 401; read-only credential POST 403; write credential POST
  succeeded; public Swagger and metrics 404; private metrics 200 including DB stats.
- pg_dump custom-format backup of that canary database was restored using pg_restore
  into a separate database; the inserted `restore-check` record was verified.
  This is a functional restore drill, not a measured production RPO/RTO exercise.

## Three-minute regression soak

Command: `loadtest -duration 60s` (three phases), concurrency limit 8, 1 KiB body,
one bounded JSON response item, simulated cooperative work 5ms, deadline 1s.

| Phase | Workers | HTTP 200 | HTTP 503 | p95 / p99 (ms, upper bounds) | Peak heap (MiB) | Post-GC heap (MiB) | Goroutines after |
| --- | ---: | ---: | ---: | --- | ---: | ---: | ---: |
| steady | 4 | 42686 | 0 | 7 / 7 | 3.18 | 1.21 | 3 |
| overload | 32 | 81381 | 1292842 | 7 / 9 | 4.94 | 1.72 | 3 |
| recovery | 4 | 43255 | 0 | 7 / 7 | 3.14 | 1.46 | 3 |

Total 1,460,164 requests, zero transport errors, zero remaining in-flight requests.
Steady/recovery requests all succeeded; overload included intentional 503 rejection.
Do not interpret the overload request rate (which includes fast rejections) as
successful business throughput. Post-GC memory returned toward baseline and
observed goroutines returned to 3; this short run does not prove absence of leaks.

Additional 3s-per-phase runs passed with a 1 MiB body and 100 response items,
a body one byte above the limit (413), and 2s work under a 100ms deadline (504).
Their exact counts are in the JSON files beside this report.

The harness uses a client and server in the same process. Heap measurements include
both; they are not process RSS or server-only allocations. Its large body path
streams to io.Discard rather than measuring arbitrary application JSON decoding.
The workload has no database or TLS. Some development/validation activity also
occurred on the host. Measurements describe a regression experiment, not isolated
benchmark results or a production sizing promise.

## Still required for stable deployment

Run a longer staging soak with the real traffic mix, external load generation,
TLS/proxy configuration and target hardware. Measure service-specific SLOs, RSS,
DB pool behavior, alerts, replica shutdown, backup RPO/RTO and a controlled canary
rollout. A Windows installer has not been exercised on a separate clean machine
in this run. Cross-platform builds and tagged-module install are release-workflow
gates; consult the actual workflow outcome rather than inferring it from local tests.
