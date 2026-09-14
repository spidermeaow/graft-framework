# Releasing Graft

## Before the first tag

- Include the owner-approved MIT LICENSE and retain third-party notices.
- Confirm the public repository is github.com/spidermeaow/graft-framework.
- Commit all source, including internal/frameworkbundle/framework.zip, after
  running `go run ./internal/cmd/bundle`. This archive supports legacy projects
  and is needed to compile the CLI through `go install`.
- Run CI and the generated-project integration tests. Keep Go's required version
  available on the GitHub runners and document it in Quick Start.

## Release

Create and push a version tag, for example v0.2.3, only after reviewing the
source changes. The release workflow validates the tag, checks the committed
compatibility bundle, tests the source and builds Windows x64/ARM64 installers
and portable CLI archives for Windows, Linux and macOS.

After installing the tagged CLI through Go modules and testing a generated app's
Swagger endpoint, it publishes a GitHub Release containing archives, installers
and SHA256SUMS.txt. The tag itself is public immediately, even if an asset build
later fails. Never move or reuse a published version tag.

Smoke-test from a clean directory after the tag is accessible:

```sh
go install github.com/spidermeaow/graft-framework/cmd/graft@v0.2.3
graft version
graft new release-check
cd release-check
go test ./...
go build ./cmd/api
graft build
graft dev
```

Also install the Windows Setup on a clean machine and run the same sequence.
Confirm App and Swagger respond. Test a published app on a server without Go.
All releases must identify the real version, not 0.2.3-dev.

## Upgrade old projects

From the project root, with the new CLI installed:

```sh
graft upgrade-project --version v0.2.3
graft upgrade-project --version v0.2.3 --apply
```

The command preserves .env and application code. It verifies the new dependency
and builds before saving, backs up go.mod/go.sum/managed go.work, and retains old
.graft source directories. Review the diff and test before removing backups.
User-managed workspaces must be migrated manually. Commit go.mod and go.sum.

## Adoption

Keep English Quick Start, CHANGELOG and examples current. Record opt-in projects
in BUILT_WITH_GRAFT.md, including a public link and permission to list them.
GitHub Release asset download counts measure downloads, not unique users or
active deployments. Never present stars or download counts as verified usage.

## v0.2 release acceptance

Run the production tests with an isolated PostgreSQL URL, race detection, the
load/soak harness and both security scanners. Regenerate the embedded bundle.
Release candidates use a fresh `v0.2.0-rc.N` tag; the workflow marks hyphenated tags
as prereleases so they do not replace the latest stable release. The tagged-module
smoke test explicitly enables docs on its generated app.

The security workflow checks Go dependencies on Linux/Windows/macOS weekly, plus
pinned Swagger assets/advisories. Dependency automation covers Go and Actions;
review Swagger's manifest and upstream bundle notices during JS upgrades.

Do not promote stable until the deployment workload meets the acceptance criteria
in PRODUCTION.md. Archive the load output, hardware/configuration, failure tests,
restore drill and canary observations. Compare downloaded assets with SHA256SUMS;
checksums detect corruption but are not independent publisher signatures.
