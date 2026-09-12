# Changelog

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
