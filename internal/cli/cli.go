// Package cli implements Graft's standard-library command line interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/internal/frameworkbundle"
	"github.com/spidermeaow/graft-framework/licenses"
)

const usage = `Graft — Write Routes. Migrate. Document. Ship.

Usage:
  graft version
  graft licenses
  graft install [--dir path] [--no-path]
  graft new [--database postgres|mysql] [--module name] [--framework local-path] [--version v0.2.6] project-name
  graft upgrade-project --version v0.2.6 [--apply]
  graft doctor [--timeout 5s]
  graft dev [--package ./cmd/api] [--host 127.0.0.1]
  graft build [--package ./cmd/api] [--output path]
  graft publish --target linux-x64 [--package ./cmd/api] [--output path]
  graft make:migration [--dir migrations] migration_name
  graft migrate [--dir migrations] [--timeout 2m]
  graft migrate:status [--dir migrations] [--timeout 2m]
  graft migrate:doctor [--dir migrations] [--timeout 2m]
  graft migrate:repair --mark-pending VERSION|--mark-applied VERSION --plan
  graft migrate:repair --mark-pending VERSION|--mark-applied VERSION --note REASON --confirm
  graft migrate:rollback [--dir migrations] [--step N] [--timeout 2m]

Migration commands use DATABASE_DRIVER (postgres or mysql) and DATABASE_URL.
PostgreSQL is the default. Flags must precede positional arguments.
Publish targets: linux-x64, linux-arm64, windows-x64, windows-arm64, darwin-x64, darwin-arm64.
`

// Run executes a CLI command. It returns errors instead of exiting the process.
func Run(ctx context.Context, args []string, out, errOut io.Writer) error {
	return run(ctx, args, nil, out, errOut)
}

// RunInteractive executes a command with a reader for prompts. Command binaries
// should use this; Run stays deterministic for callers that embed the CLI.
func RunInteractive(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	return run(ctx, args, in, out, errOut)
}

func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(out, usage)
		return err
	}
	var err error
	switch args[0] {
	case "licenses":
		if len(args) != 1 {
			return errors.New("licenses takes no arguments")
		}
		_, err = io.WriteString(out, licenses.Text())
	case "install":
		err = installCommand(ctx, args[1:], out, errOut)
	case "version", "--version", "-v":
		err = versionCommand(args[1:], out, errOut)
	case "new":
		err = newProject(ctx, args[1:], in, out, errOut)
	case "upgrade-project":
		err = upgradeProject(ctx, args[1:], out, errOut)
	case "doctor":
		err = doctorCommand(ctx, args[1:], out, errOut)
	case "dev", "build", "publish":
		err = goCommand(ctx, args[0], args[1:], out, errOut)
	case "make:migration", "migrate", "migrate:status", "migrate:doctor", "migrate:repair", "migrate:rollback":
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
	var host string
	if command == "dev" {
		f.StringVar(&host, "host", "", "bind IP or localhost (default APP_HOST or 127.0.0.1; requires current app template)")
	}
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
	if command == "dev" {
		if err := graft.LoadEnv(); err != nil {
			return err
		}
	}
	env := os.Environ()
	if err := ensureEmbeddedWorkspace(); err != nil {
		return err
	}
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
		if host != "" && host != "localhost" && net.ParseIP(host) == nil {
			return errors.New("host must be an IP address or localhost")
		}
		env = developmentEnv(env, host)
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
		output = filepath.Join("bin", name)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err := runGo(ctx, env, out, errOut, "build", "-trimpath", "-o", output, *pkg); err != nil {
		return err
	}
	if err := copyBuildCompanions(output); err != nil {
		return err
	}
	fmt.Fprintln(out, "Built", output)
	return nil
}

func developmentEnv(env []string, host string) []string {
	if host == "" {
		for _, entry := range env {
			key, value, _ := strings.Cut(entry, "=")
			if strings.EqualFold(key, "APP_HOST") {
				host = value
			}
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	env = replaceEnv(env, "APP_HOST", host)
	return replaceEnv(env, "GRAFT_DEV", "1")
}

func copyBuildCompanions(output string) error {
	for _, name := range []string{".env.example", "README.md"} {
		data, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(output), name), data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func ensureEmbeddedWorkspace() error {
	data, err := os.ReadFile("go.mod")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), "// graft: embedded-framework") {
		return nil
	}
	framework, err := frameworkbundle.Cache()
	if err != nil {
		return fmt.Errorf("prepare embedded framework: %w", err)
	}
	path := "go.work"
	current, err := os.ReadFile(path)
	if err == nil && !strings.Contains(string(current), "// graft: embedded-framework") {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(embeddedWorkspace(framework)), 0644)
}

func embeddedWorkspace(framework string) string {
	return "go 1.26.6\n\n// graft: embedded-framework\nuse .\n\nreplace github.com/spidermeaow/graft-framework => " + strconv.Quote(filepath.ToSlash(framework)) + "\n"
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
