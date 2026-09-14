# Changelog

## v0.2.6

- Added context-aware OpenAPI metadata for documented middleware, routes and
  handlers, with generic component registration and deduplicated merge rules.
- Added automatic Bearer/API Key metadata in the auth toolkit and documented
  rate-limit responses, while preserving existing route options and middleware.

## v0.2.5

- Added optional `toolkit/auth`, `toolkit/validate`, `toolkit/api`, and
  `toolkit/testkit` packages, plus group middleware and OpenAPI security metadata.

## v0.2.4

- Added `migrate:doctor` and guarded `migrate:repair --plan` / `--confirm` for
  MySQL dirty migrations, with name/checksum checks and operator audit notes.
- Recorded migration start, SQL completion and failure events; added a tested
  recovery path for failed Up and Down operations.
- Added `graft doctor` for read-only checks of Go, dotenv/runtime configuration,
  local migrations, and an optional PostgreSQL/MySQL connection ping.

## v0.2.3

- Added a Windows application manifest to Setup so Program Compatibility Assistant
  recognizes it as a current per-user installer instead of applying its legacy
  compatibility heuristic.

## v0.2.2

- `graft new` now asks users to select PostgreSQL or MySQL and creates a matching
  `.env.example`; scripts can choose with `--database postgres|mysql`.
- Generated Swagger metadata now uses the selected Graft framework version.

## v0.2.0

- Added MySQL 8.0+ migration support to migrate, status and rollback commands,
  including a MySQL driver selector in `.env`.
- Added session-scoped locking and dirty-state protection for MySQL's
  nontransactional DDL, with recovery documentation and integration checks.

## v0.2.0-rc.1

- Added bounded concurrency and request body middleware, cooperative request
  deadlines, a process-local token bucket, and bounded Prometheus metrics.
- Added configurable server timeouts, readiness drain and ordered shutdown hooks.
- Added protected documentation routes and loopback/default-off docs configuration
  for v0.2 starter applications, while retaining older framework scaffolds.
- Hardened the Machines example with separate read/write credentials, query/pool
  limits, keyset pagination, health checks and optional private metrics/profiling.
- Added overload/soak tooling, fault tests, Go/Swagger vulnerability gates, pinned
  Actions, scheduled security scans and a production/rollback guide.
- Prerelease: target-service capacity, longer staging soak and canary acceptance
  are required before a stable production rollout.

## v0.1.1

- Fixed duplicate startup banners in `graft dev`; the application now reports
  its App and Swagger URLs once, using its configured port.

## v0.1.0

- Standard Go-module projects from both Windows installers and Go-installed CLI.
- Dependency setup during `graft new`, with an offline scaffolding option.
- Release-matched framework versions and explicit local-source development.
- Preview/apply migration from legacy embedded or local-source projects.
- GitHub release automation with tagged-module installation checks and checksums.
- Retained legacy project compatibility, Swagger, PostgreSQL migrations and
  deployment bundles with external configuration.
