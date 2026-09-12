# 006: Dotenv convenience and standalone Windows setup

Load `.env` at startup explicitly using `graft.LoadEnv()` in generated apps and
before configuration reads in CLI dev/migration commands. The default lookup reads
the executable directory first, then the working directory (which wins on conflicts),
so a deployed binary can keep its editable `.env` beside itself. A missing file is
fine; parse errors fail early without displaying secret values. Existing process
values win (including explicit empties). No upward directory search, interpolation,
shell execution, or new dependency. Parsing completes before applying values. This is
a startup operation, not a configuration reload service.

The supported single-line dotenv subset handles export, comments, quotes, BOM and
CRLF. Quoted multiline strings are outside scope. `.env.example` remains a template
and must be copied by the user. Generated .gitignore excludes `.env`.

Setup.exe is a small Go Windows GUI launcher embedding the exact packaged CLI.
Double-click displays a native install confirmation, invokes the embedded CLI's
existing installer, then displays success/failure. Silent mode supports automation.
The transient payload is removed after installation. Build it with the Windows
packaging script; the graftsetup build tag excludes its generated payload from
normal source-checkout builds and tests. Neither installer requires Go on the
destination. Development/compilation still require the Go toolchain.
