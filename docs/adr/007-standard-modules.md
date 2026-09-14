# 007: One Go module, multiple CLI installation methods

Windows Setup, portable executables and `go install` distribute the same CLI.
New projects require the CLI's release version of the public framework module.
Development builds default to v0.2.0; `new --version` overrides it and
`new --framework` explicitly selects a local checkout.

Generation runs `go mod tidy` to prepare go.sum and editor tooling. Failure keeps
the generated source and explains how to retry. `--download=false` supports
offline scaffolding. No workspace or embedded marker is generated.

Existing embedded markers still select the compatibility cache/workspace path.
Local source replacements keep working. `upgrade-project --version vX.Y.Z` is a
preview; `--apply` resolves and builds a staged module before backing up and
updating dependency files. Only Graft-managed go.work files can be removed.
Source snapshots and user caches are retained. An interrupted save can be
recovered from the printed backup directory.

This supersedes the new-project distribution choices in ADRs 004 and 005.
The compatibility archive must be included in the tagged source, because
`go install module/cmd/graft@version` does not execute source generators.

Release automation builds from a tag and publishes after tests and a public-module
smoke check pass. Publishing the Git tag already makes the module version
discoverable, even before release assets are ready. No private credentials enter assets.
