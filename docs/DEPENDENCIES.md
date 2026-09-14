# Dependency decisions

The root HTTP package, OpenAPI types, migration runner and database adapters use
only Go's standard library. `go list -deps .` can verify this. The module also
contains the CLI and database examples, which use these external modules:

| Dependency | Scope | Reason | License |
| --- | --- | --- | --- |
| github.com/lib/pq | CLI, example, integration tests | Mature pure-Go database/sql PostgreSQL driver; implementing the wire protocol is outside Graft's scope | MIT |
| github.com/go-sql-driver/mysql v1.10.1 | CLI, MySQL integration tests | Pure-Go database/sql MySQL protocol with context cancellation and multiple SQL statements | MPL-2.0 |
| filippo.io/edwards25519 v1.2.0 | MySQL driver (indirect) | Upstream driver's authentication dependency | BSD-3-Clause |
| swagger-ui-dist 5.32.15 | Embedded JS/CSS assets | Real Swagger UI with no browser CDN or server-side framework dependency | Apache-2.0 plus bundled notices |

The PostgreSQL adapter accepts any compatible database/sql driver. pq is chosen
for its focused SQL interface and pure-Go cross compilation. Versions and hashes
are pinned in go.mod/go.sum; upgrade via normal Go module tooling.

MySQL driver sources are unmodified. Its MPL-2.0 license does not change Graft's
MIT license. Driver licenses and source URLs are in `licenses/`, embedded in
`graft licenses`, and installed as THIRD_PARTY_NOTICES.txt by Windows Setup.
Release archives also contain the license files.

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

## Automated checks

`go run ./internal/cmd/check-swagger` verifies the vendored file SHA-256 hashes
against internal/swaggerui/manifest.json and queries OSV for both swagger-ui-dist
and swagger-ui at the pinned version. Network or advisory-query failures fail the
gate. It does not prove that every bundled transitive JavaScript dependency is
free from unpublished/unmapped issues; review upstream release/security notes.
Refresh the manifest only after verifying the official archive integrity.

CI and release run govulncheck v1.8.0; scheduled checks cover Linux, Windows and
macOS. Actions are pinned by SHA and maintained with Dependabot. No JavaScript
package installation is needed to build or run Graft.
