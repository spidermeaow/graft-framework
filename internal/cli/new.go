package cli

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

var projectName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var moduleName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._~/-]*$`)
var versionName = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`)

func newProject(args []string, out, errOut io.Writer) error {
	f := flags("new", errOut)
	module := f.String("module", "", "Go module path")
	framework := f.String("framework", localFramework(), "local framework checkout; empty uses published module")
	version := f.String("version", "v0.1.0", "framework module version")
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
	data := struct{ Name, Module, Version, Replace string }{name, *module, *version, replace}
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
	fmt.Fprintf(out, "Created %s\n\n  cd %s\n  graft dev\n", name, name)
	if replace != "" {
		fmt.Fprintln(out, "Using local Graft checkout:", *framework)
	}
	return nil
}

func isFramework(path string) bool {
	b, err := os.ReadFile(filepath.Join(path, "go.mod"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(b))
	return len(fields) >= 2 && fields[0] == "module" && fields[1] == "github.com/spidermeaow/graft-framework"
}

func localFramework() string {
	// Only development binaries infer a local replacement. Tagged installs use the module proxy.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return ""
	}
	if _, source, _, ok := runtime.Caller(0); ok {
		path := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
		if isFramework(path) {
			return path
		}
	}
	return ""
}
