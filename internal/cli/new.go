package cli

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

var projectName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var moduleName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._~/-]*$`)
var versionName = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`)

type databaseChoice struct {
	Name, Driver, URL string
}

var databases = map[string]databaseChoice{
	"postgres": {"PostgreSQL", "postgres", "postgres://postgres:postgres@localhost:5432/%s?sslmode=disable"},
	"mysql":    {"MySQL", "mysql", "user:password@tcp(localhost:3306)/%s"},
}

func newProject(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	f := flags("new", errOut)
	database := f.String("database", "", "database for migration configuration: postgres or mysql")
	module := f.String("module", "", "Go module path")
	framework := f.String("framework", "", "local framework checkout")
	version := f.String("version", frameworkVersion(), "framework module version")
	download := f.Bool("download", true, "resolve dependencies with go mod tidy (false generates files only)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 || !projectName.MatchString(f.Arg(0)) {
		return errors.New("usage: graft new [flags] project-name (letters, digits, - or _)")
	}
	name := f.Arg(0)
	if *module == "" {
		*module = name
	}
	if !moduleName.MatchString(*module) || strings.Contains(*module, "//") || strings.Contains(*module, "..") || strings.HasSuffix(*module, "/") {
		return errors.New("invalid module path")
	}
	if !versionName.MatchString(*version) {
		return errors.New("invalid framework version")
	}
	choice, err := selectDatabase(*database, in, out)
	if err != nil {
		return err
	}
	replace := ""
	if *framework != "" {
		path, err := filepath.Abs(*framework)
		if err != nil {
			return err
		}
		if !isFramework(path) {
			return fmt.Errorf("%s is not a Graft checkout", path)
		}
		replace = "\nreplace github.com/spidermeaow/graft-framework => " + strconv.Quote(filepath.ToSlash(path)) + "\n"
	}
	data := struct {
		Name, Module, Version, Replace string
		Database                       databaseChoice
		Modern                         bool
	}{name, *module, *version, replace, choice, *framework != "" || modernFramework(*version)}
	files := map[string]string{"main.go.tmpl": "cmd/api/main.go", "go.mod.tmpl": "go.mod", "README.md.tmpl": "README.md", "env.tmpl": ".env.example", "gitignore.tmpl": ".gitignore"}
	// Render before making any filesystem changes; a pre-existing target is never overwritten.
	rendered := map[string][]byte{}
	for source, target := range files {
		b, err := templates.ReadFile("templates/" + source)
		if err != nil {
			return err
		}
		t, err := template.New(source).Parse(string(b))
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err = t.Execute(&buf, data); err != nil {
			return err
		}
		rendered[target] = buf.Bytes()
	}
	if err := os.Mkdir(name, 0755); err != nil {
		return err
	}
	for _, dir := range []string{"cmd/api", "internal", "migrations"} {
		if err := os.MkdirAll(filepath.Join(name, dir), 0755); err != nil {
			return err
		}
	}
	for target, b := range rendered {
		if err := os.WriteFile(filepath.Join(name, target), b, 0644); err != nil {
			return err
		}
	}
	if *download {
		cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
		cmd.Dir = name
		cmd.Env = replaceEnv(os.Environ(), "GOWORK", "off")
		cmd.Stdout, cmd.Stderr = out, errOut
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("project created in %s, but dependency setup failed: %w; ensure Go is installed and framework %s is published, then run go mod tidy inside the project", name, err, *version)
		}
	}
	fmt.Fprintf(out, "Created %s (%s)\n\n  cd %s\n  graft dev\n", name, choice.Name, name)
	if replace != "" {
		fmt.Fprintln(out, "Using local Graft checkout:", *framework)
	}
	return nil
}

func selectDatabase(value string, in io.Reader, out io.Writer) (databaseChoice, error) {
	if value != "" {
		choice, ok := databases[strings.ToLower(value)]
		if !ok {
			return databaseChoice{}, errors.New("invalid --database; use postgres or mysql")
		}
		return choice, nil
	}
	// Library callers use the historical PostgreSQL default. The command binary
	// supplies stdin and therefore always asks before it creates project files.
	if in == nil {
		return databases["postgres"], nil
	}
	reader := bufio.NewReader(in)
	for {
		fmt.Fprintln(out, "Choose a database for migrations:")
		fmt.Fprintln(out, "  1) PostgreSQL (default)")
		fmt.Fprintln(out, "  2) MySQL")
		fmt.Fprint(out, "Select [1]: ")
		answer, err := reader.ReadString('\n')
		if err != nil && len(answer) == 0 {
			return databaseChoice{}, errors.New("database selection was not provided; use --database postgres or --database mysql")
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "1", "postgres", "postgresql":
			return databases["postgres"], nil
		case "2", "mysql":
			return databases["mysql"], nil
		default:
			fmt.Fprintln(out, "Please enter 1 for PostgreSQL or 2 for MySQL.")
		}
	}
}

func isFramework(path string) bool {
	b, err := os.ReadFile(filepath.Join(path, "go.mod"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(b))
	return len(fields) >= 2 && fields[0] == "module" && fields[1] == "github.com/spidermeaow/graft-framework"
}

// Older explicitly selected framework releases must keep a compatible scaffold.
func modernFramework(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) < 2 {
		return false
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return major > 0 || minor >= 2
}
