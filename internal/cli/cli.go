// Package cli implements Graft's standard-library command line interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const usage = `Graft — Write Routes. Migrate. Document. Ship.

Usage:
  graft new [--module name] [--framework local-path] [--version v0.1.0] project-name
  graft dev [--package ./cmd/api]
  graft build [--package ./cmd/api] [--output path]
  graft publish --target linux-x64 [--package ./cmd/api] [--output path]
  graft make:migration [--dir migrations] migration_name
  graft migrate [--dir migrations] [--timeout 2m]
  graft migrate:status [--dir migrations] [--timeout 2m]
  graft migrate:rollback [--dir migrations] [--step N] [--timeout 2m]

Migration commands use DATABASE_URL. Flags must precede positional arguments.
Publish targets: linux-x64, linux-arm64, windows-x64, windows-arm64, darwin-x64, darwin-arm64.
`

// Run executes a CLI command. It returns errors instead of exiting the process.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(out, usage)
		return err
	}
	var err error
	switch args[0] {
	case "new":
		err = newProject(args[1:], out, errOut)
	case "dev", "build", "publish":
		err = goCommand(ctx, args[0], args[1:], out, errOut)
	case "make:migration", "migrate", "migrate:status", "migrate:rollback":
		err = migrationCommand(ctx, args[0], args[1:], out, errOut)
	default:
		return fmt.Errorf("unknown command %q; run graft help", args[0])
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func flags(name string, out io.Writer) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(out)
	return f
}

var targets = map[string][2]string{
	"linux-x64": {"linux", "amd64"}, "linux-arm64": {"linux", "arm64"},
	"windows-x64": {"windows", "amd64"}, "windows-arm64": {"windows", "arm64"},
	"darwin-x64": {"darwin", "amd64"}, "darwin-arm64": {"darwin", "arm64"},
}

func goCommand(ctx context.Context, command string, args []string, out, errOut io.Writer) error {
	f := flags(command, errOut)
	pkg := f.String("package", "", "main package (defaults to ./cmd/api if present, otherwise .)")
	var output, target string
	if command != "dev" {
		f.StringVar(&output, "output", "", "binary output path")
	}
	if command == "publish" {
		f.StringVar(&target, "target", "", "required deployment target")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("%s takes no positional arguments", command)
	}
	if *pkg == "" {
		*pkg = "."
		if info, err := os.Stat(filepath.Join("cmd", "api")); err == nil && info.IsDir() {
			*pkg = "./cmd/api"
		}
	}
	if strings.HasPrefix(*pkg, "-") {
		return errors.New("invalid package")
	}
	env := os.Environ()
	goos := runtime.GOOS
	if command == "publish" {
		pair, ok := targets[target]
		if !ok {
			return fmt.Errorf("unsupported target %q; run graft help", target)
		}
		goos = pair[0]
		env = replaceEnv(env, "GOOS", pair[0])
		env = replaceEnv(env, "GOARCH", pair[1])
		env = replaceEnv(env, "CGO_ENABLED", "0")
	}
	if command == "dev" {
		return runGo(ctx, env, out, errOut, "run", *pkg)
	}
	if output == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		name := filepath.Base(cwd)
		if goos == "windows" {
			name += ".exe"
		}
		dir := "bin"
		if command == "publish" {
			dir = "publish"
		}
		output = filepath.Join(dir, name)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err := runGo(ctx, env, out, errOut, "build", "-trimpath", "-o", output, *pkg); err != nil {
		return err
	}
	fmt.Fprintln(out, "Built", output)
	return nil
}

func replaceEnv(env []string, key, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if k, _, ok := strings.Cut(entry, "="); ok && !strings.EqualFold(k, key) {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

func runGo(ctx context.Context, env []string, out, errOut io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
