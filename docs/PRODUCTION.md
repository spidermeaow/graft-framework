# Production deployment and migration guide

The v0.2 APIs add resource controls; they do not certify an application's capacity.
The core remains standard-library-only. Run the workload on deployment hardware,
define a latency/error budget, and tune limits from the results before rollout.

## Upgrade existing applications

Existing `Run`, `RunContext`, `Serve`, `Docs`, `PrintStartup` and `Bind` calls remain
available. Updating the CLI or module alone does not retrofit application source.
Generated applications targeting v0.2+ use the new configuration. Explicitly
targeting v0.1.x retains the legacy scaffold and its binding/docs behavior.
Local framework development should use `graft new --framework <checkout> app`.

For a v0.2 application:

```go
cfg, err := graft.ConfigFromEnv() // call LoadEnv first
if err != nil { return err }
metrics := &graft.Metrics{}
app.Use(graft.RequestID(), metrics.Middleware(), graft.Logger())
app.Use(graft.ConcurrencyLimit(cfg.MaxConcurrent),
    graft.BodyLimit(cfg.MaxBodyBytes), graft.RequestDeadline(cfg.RequestTimeout))
if cfg.Docs { app.DocsWithMiddleware("API", "1", yourAuthenticationMiddleware) }
return app.RunWithConfig(cfg.Address(), graft.ServerConfig{
    WriteTimeout: cfg.RequestTimeout + 5*time.Second,
})
```

Global limits also cover health endpoints. For orchestrated services, follow the
Machines example: put business limits on a group and keep health checks separate.
Set an ingress rate/concurrency limit for health endpoints as well; dependency
checks consume resources. Use a short deadline on readiness checks.

| Environment variable | Starter default | Meaning |
| --- | --- | --- |
| APP_HOST | 127.0.0.1 | Bind IP or localhost; set 0.0.0.0 explicitly for a container |
| APP_PORT | 8080 | TCP port |
| APP_DOCS | false; true under graft dev | Register Swagger/spec routes |
| APP_MAX_CONCURRENT | 64 | Active requests per middleware instance; no waiting queue |
| APP_MAX_BODY_BYTES | 1048576 | Maximum bytes read from a request body |
| APP_REQUEST_TIMEOUT | 10s | Cooperative request deadline |

`graft dev --host 0.0.0.0` explicitly exposes a compatible app to the network.
The flag overrides APP_HOST; APP_HOST otherwise overrides the loopback default.
Older applications with hard-coded `Run(":" + port)` ignore these environment
settings until their source is migrated. APP_DOCS=false overrides the dev default.
Compiled apps do not infer a production mode from APP_ENV.

## Memory and overload

Place ConcurrencyLimit outside RequestDeadline. Expiration never releases a slot
until the actual handler returns. On overload, requests get 503 and Retry-After;
there is no unlimited queue. This bounds admitted handlers, not TCP connections,
kernel backlog, rejected requests, or memory allocated by application code. Put a
connection limit and timeouts on the ingress and set container/OS memory limits.

BodyLimit also covers direct body reads. Propagate read errors so unknown-length
oversized uploads become 413. Bind still has its own 1 MiB default; use BindLimit
when a route intentionally has a different limit. JSON serializes the whole value
before writing: use bounded pages, bounded field sizes, and streaming for large
exports. A body byte limit is not a total decoded-object memory budget.

Do not choose a universal concurrency setting from CPU count. Begin with the
starter values on staging, measure peak per-request allocation, latency and DB
waits, then set a limit that leaves memory headroom. Account for every replica's
DB pool plus migrations/administration. GOMEMLIMIT is a soft Go runtime budget,
not a guarantee against OOM and not a replacement for bounded work. Profile first;
do not tune GC to conceal an ever-growing queue or cache.

RateLimit is a constant-memory, global token bucket per process: it returns 429.
It does not allocate a map per client IP, infer a user from a header, or coordinate
replicas. For per-user/tenant limits use authenticated identities and a gateway
or bounded shared store. Clients should back off with jitter, not retry in a loop.

## Deadlines and database permissions

RequestDeadline does not spawn an unbounded handler goroutine or kill a goroutine.
Pass the request context to all database/network calls and check cancellation in
CPU loops. Code that ignores cancellation still occupies its slot. A response
already started cannot be replaced with 504. Connection Read/WriteTimeout is
separate from a handler deadline. Custom long-lived streams should use their own
explicit policy rather than a short global request deadline.

The Machines example adds a 2s query deadline, including connection-pool waiting,
and sets open/idle/lifetime limits. Configure PostgreSQL as defense in depth:

```sql
-- Run as a database administrator; adapt names to the deployment.
ALTER ROLE graft_app SET statement_timeout = '2s';
ALTER ROLE graft_app SET lock_timeout = '1s';
ALTER ROLE graft_app SET idle_in_transaction_session_timeout = '10s';
```

Use a restricted application role with only required SELECT/INSERT/UPDATE/DELETE
grants and a separate migration role with DDL permissions. Do not run the app as
postgres/superuser. Use TLS certificate validation (`sslmode=verify-full` with the
proper CA) for non-local database connections. Set connection establishment
timeouts as well. Do not put live DSNs or tokens into source, images or logs.

## Authentication, proxy and docs

The example fails startup without separate randomly generated read/write service
tokens of at least 32 characters. A read token cannot POST; a write token can.
This is a service-to-service example, not a user-account system. Multi-user apps
must validate tokens with their identity provider, including issuer/audience/
expiry, and enforce object-level access. Cookie-authenticated writes also need
CSRF protection. CORS is not authentication; allow only intended browser origins.

DocsWithMiddleware protects `/swagger`, its assets and `/openapi.json` equally.
For an interactive browser, put documentation behind your session-aware gateway
or authentication middleware; the example's Bearer header gate is primarily for
programmatic access. Do not put tokens in query strings to make browser docs work.

Use a TLS-terminating reverse proxy. For example, the following nginx server must
be completed with the deployment's certificate paths and domain:

```nginx
server {
    listen 443 ssl;
    server_name api.example.com;
    ssl_certificate /etc/tls/fullchain.pem;
    ssl_certificate_key /etc/tls/privkey.pem;
    client_max_body_size 1m;
    client_header_timeout 5s;
    client_body_timeout 30s;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_connect_timeout 2s;
        proxy_read_timeout 15s;
        proxy_send_timeout 15s;
    }
}
```

Configure an HTTP-to-HTTPS redirect separately. Keep the backend unreachable from
untrusted networks. Graft does not trust X-Forwarded-* for identity or client IP.
If an app needs these headers, accept them only from an allowlisted proxy which
overwrites incoming values. Rate/connection limits must account for the actual
proxy topology; avoid treating every request as the proxy user.

## Monitoring and lifecycle

Metrics.Handler serves Prometheus text on an explicitly mounted handler. Counters
have no user-supplied labels, so paths/IDs cannot grow a metrics map. Histogram
buckets are fixed. Heap metrics report Go heap, not total process RSS; collect
RSS/container memory and CPU from the host monitoring agent too.

The Machines example optionally listens on APP_ADMIN_ADDR (loopback IP only) for
`/metrics`, including DB pool usage/waits. APP_PPROF=true additionally enables
profiling only on that listener. Both are off unless configured; never forward
the admin listener through the public proxy. In containers, use a local agent or
port-forwarding with access control. Profiling may contain private data.

Alert on sustained rejection/error rates, p95/p99 latency, rising DB wait time,
low memory headroom and goroutine growth that does not subside after traffic drops.
Tune alerts to an agreed SLO; do not page on every isolated 429. Default internal
error logs may include driver errors: handlers must avoid embedding secrets in
errors. Panic logs include type, request ID and stack, not the panic payload.

Health starts unready. ServeWithConfig marks it ready, then withdraws readiness
before DrainDelay when stopping. After that delay, HTTP stops accepting connections
and drains for ShutdownTimeout (10s by default). On expiry, request contexts are
cancelled and connections closed. Total planned stop time includes both periods;
set the orchestrator's grace period longer than their sum plus cleanup margin.

ShutdownHooks run in order after HTTP drain with the remaining shutdown context.
Hooks and worker cleanup must honor that context. Stop workers and await them
before closing shared DB/queues. Hijacked connections require explicit tracking.
The example drains both public/admin listeners before closing its DB pool.
Liveness stays independent of DB health; readiness uses a bounded DB check.

## Load, soak and fault validation

```sh
go test -race ./...
go run ./internal/cmd/loadtest -duration 20s
go run ./internal/cmd/loadtest -duration 10m
go run ./internal/cmd/loadtest -duration 5s -body-bytes 1048576 -response-items 100
go run ./internal/cmd/loadtest -duration 5s -body-bytes 1048577
go run ./internal/cmd/loadtest -duration 5s -delay 2s -timeout 100ms
```

Each harness run executes steady, overload and recovery phases. JSON output
records status counts, transport failures, latency histogram upper bounds, sampled
peak heap and post-GC heap. Client and server share a process; these are regression
experiments, not per-server RSS or customer capacity claims. Run an external load
generator against the real app on deployment hardware to establish an SLO.

Set GRAFT_TEST_DATABASE_URL to an isolated PostgreSQL admin URL to include actual
lock/pool timeout, migration, readiness failure and compatibility tests. Tests
create and remove uniquely named databases. Never use production for fault tests.

Release acceptance: expected steady requests succeed within the chosen SLO;
overload rejects promptly; recovery resumes without restart; memory/goroutines
stabilize in a long soak; DB faults time out; shutdown drains; dependency scans and
generated-app tests pass. Soak duration, traffic mix and failure budget must match
the target service. A short local result does not satisfy an unmeasured service SLO.

## Migration, rollback and recovery

1. Back up using pg_dump (custom format) or the managed database's backup/PITR
   service. Encrypt backups and restrict access; verify retention and RPO/RTO.
2. Restore into a separate database with pg_restore and exercise application reads
   and writes. A backup that has never been restored is not a recovery drill.
3. Use expand/contract migrations: add compatible nullable columns/tables first,
   deploy code supporting old/new data, backfill in bounded batches, then remove
   old schema only after all old binaries are retired.
4. Run migrations once using the migration role. Keep old/new app compatibility
   tests and test restoration/rollback on staging. Advisory locks require a direct
   database connection or session-mode pooling.
5. Roll back the application to the previous artifact while compatible schema
   remains. Destructive Down SQL is not a generic production rollback strategy;
   use a forward repair or restore/PITR when data would otherwise be lost.

Publish a prerelease first and canary it behind controlled traffic. Keep the last
known-good binary/config and checksums. Promote a fresh stable tag only after the
target application's rollout, monitoring and recovery checks pass. Never move an
already published tag.
