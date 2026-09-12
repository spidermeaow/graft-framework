# 004: Small CLI and native publication

Use os.Args, flag and os/exec. The CLI delegates development and compilation to
the installed Go toolchain. Flags precede positional arguments. A generated app
has cmd/api, empty internal and migrations directories, and environment examples;
there is no generated application architecture or YAML parser.

`new` refuses an existing directory. Development CLI binaries infer their source
checkout and write an explicit go.mod replace, so onboarding works before a
public release. `--framework` overrides this, while an empty value uses the
published version. Tagged installations do not infer local source paths.
github.com/spidermeaow/graft-framework is the canonical module identity chosen for v0.1;
the repository is hosted under that path. Version tags are a separate release step.

Build uses the host toolchain settings; publish sets GOOS, GOARCH and CGO_ENABLED=0
for six explicit targets. Output has no Graft runtime requirement. Applications
that require cgo should use normal Go tooling and an appropriate cross compiler.
