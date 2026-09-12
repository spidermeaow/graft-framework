# Dependency decisions

The root HTTP package, OpenAPI types, migration runner and PostgreSQL adapter use
only Go's standard library. `go list -deps .` can verify this. The module also
contains the CLI and PostgreSQL example, which use one external Go module:

| Dependency | Scope | Reason | License |
| --- | --- | --- | --- |
| github.com/lib/pq | CLI, example, integration tests | Mature pure-Go database/sql PostgreSQL driver; implementing the wire protocol is outside Graft's scope | MIT |
| swagger-ui-dist 5.32.15 | Embedded JS/CSS assets | Real Swagger UI with no browser CDN or server-side framework dependency | Apache-2.0 plus bundled notices |

The PostgreSQL adapter accepts any compatible database/sql driver. pq is chosen
for its focused SQL interface and pure-Go cross compilation. Versions and hashes
are pinned in go.mod/go.sum; upgrade via normal Go module tooling.

Swagger UI was obtained from the official npm distribution:
https://registry.npmjs.org/swagger-ui-dist/-/swagger-ui-dist-5.32.15.tgz

Verified archive SHA-512 (npm integrity):

```
sha512-TSFER+rFQlf1nzk6WvKkMaHTxAPQ3eAAxigFThnxQedSREanfZgSbJFayZVs/ULnSbNdrJOb99vLD6xpb3R3eg==
```

Only swagger-ui-bundle.js, swagger-ui.css and the upstream license/notice files
are vendored under internal/swaggerui/assets. The index and initializer are local
Graft files. No npm install is needed to build or run Graft. Upgrades should verify
the archive integrity, preserve LICENSE, NOTICE and bundle license notices, then
run the documentation endpoint tests and inspect the UI.

Upstream references: https://github.com/lib/pq and https://github.com/swagger-api/swagger-ui.
