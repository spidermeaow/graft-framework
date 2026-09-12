# 005: Standalone Windows CLI distribution

The embedded new-project design below is historical and superseded by
[ADR 007](007-standard-modules.md). It is retained for legacy compatibility only.

Ship a pure-Go, trimpath-built Windows executable. An explicit `graft install`
copies itself to the current user's LOCALAPPDATA/Graft/bin and updates User PATH
using the Windows-provided PowerShell. No Go, repository checkout, admin rights,
external installer, or PowerShell execution-policy change is needed to install.
Installation does not occur merely by invoking the executable without arguments.
Existing parent shells must be restarted to receive PATH changes.

The public framework release is not assumed to exist. A deterministic embedded
archive contains the library source (including migration packages and Swagger UI
notices), excluding CLI, tests, examples and build outputs. The packaging command
regenerates the archive before compiling. Generated standalone projects mark their
go.mod for Graft's embedded framework; each `graft dev`, `build` or `publish`
command extracts it only into a temporary directory and supplies an alternate
go.mod to Go. Projects therefore contain no `.graft` directory or framework source
copy, while still moving between machines without a developer-specific path.

An explicit `new --version` selects a published module, and `--framework` selects
local source. Embedded projects must use Graft's dev/build/publish commands;
direct Go builds require one of those explicit source options.
Go remains required for development, build and publish. Application binaries,
CLI version/help, project generation and CLI migration execution need no Go runtime.

Installer tests copy/update executables in temporary directories. Windows
PowerShell tests exercise PATH handling with only registry reads/writes replaced
by test inputs/outputs; they never modify the developer's permanent User PATH.
