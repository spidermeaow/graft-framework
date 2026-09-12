package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spidermeaow/graft-framework/internal/frameworkbundle"
)

// A local module proxy exercises the actual public-module flow before the first
// tag exists. No local replace or workspace is supplied to the generated app.
func moduleProxy(t *testing.T) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "source")
	if err := frameworkbundle.Extract(root); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		w, err := zw.Create("github.com/spidermeaow/graft-framework@v0.1.0/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/spidermeaow/graft-framework/@v/v0.1.0.zip":
			w.Write(archive.Bytes())
		case "/github.com/spidermeaow/graft-framework/@v/v0.1.0.mod":
			w.Write(mod)
		case "/github.com/spidermeaow/graft-framework/@v/v0.1.0.info":
			w.Write([]byte(`{"Version":"v0.1.0","Time":"2026-01-01T00:00:00Z"}`))
		case "/github.com/spidermeaow/graft-framework/@v/list":
			w.Write([]byte("v0.1.0\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("GOPROXY", server.URL)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOWORK", "off")
}

func TestStandardProjectAndLegacyUpgrade(t *testing.T) {
	moduleProxy(t)
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"new", "app"}, &out, &out); err != nil {
		t.Fatal(err, &out)
	}
	t.Chdir("app")
	for _, name := range []string{"go.work", ".graft"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("unexpected %s: %v", name, err)
		}
	}
	mod, _ := os.ReadFile("go.mod")
	if strings.Contains(string(mod), "replace") || strings.Contains(string(mod), "embedded-framework") {
		t.Fatal(string(mod))
	}
	if _, err := os.Stat("go.sum"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./...")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("direct Go tooling: %v: %s", err, output)
	}
	// Convert the fixture into a legacy project without modifying its app code.
	legacy := string(mod) + "\n// graft: embedded-framework\nreplace github.com/spidermeaow/graft-framework => ./.graft/framework\n"
	if err := os.WriteFile("go.mod", []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("go.work", []byte("go 1.26.6\n// graft: embedded-framework\nuse .\n"), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"upgrade-project", "--version", "v0.1.0"}
	if err := Run(context.Background(), args, &out, &out); err != nil {
		t.Fatal(err, &out)
	}
	unchanged, _ := os.ReadFile("go.mod")
	if string(unchanged) != legacy {
		t.Fatal("preview modified go.mod")
	}
	if err := Run(context.Background(), []string{"upgrade-project", "--version", "v9.9.9", "--apply"}, &out, &out); err == nil {
		t.Fatal("accepted missing release")
	}
	unchanged, _ = os.ReadFile("go.mod")
	if string(unchanged) != legacy {
		t.Fatal("failed upgrade modified go.mod")
	}
	if err := Run(context.Background(), append(args, "--apply"), &out, &out); err != nil {
		t.Fatal(err, &out)
	}
	if _, err := os.Stat("go.work"); !os.IsNotExist(err) {
		t.Fatal("managed workspace retained", err)
	}
	backups, _ := filepath.Glob(".graft-upgrade-backup-*/go.mod")
	if len(backups) != 1 {
		t.Fatal("missing backup", backups)
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != legacy {
		t.Fatal("incorrect backup")
	}
	cmd = exec.Command("go", "test", "./...")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("upgraded Go tooling: %v: %s", err, output)
	}
}

func TestLegacyWorkspaceAndVersionSelection(t *testing.T) {
	t.Chdir(t.TempDir())
	original := Version
	Version = "v0.2.0-rc.1"
	t.Cleanup(func() { Version = original })
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"new", "--download=false", "app"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	mod, _ := os.ReadFile("app/go.mod")
	if !strings.Contains(string(mod), "v0.2.0-rc.1") {
		t.Fatal(string(mod))
	}
	t.Chdir("app")
	if err := os.WriteFile("go.mod", append(mod, []byte("\n// graft: embedded-framework\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureEmbeddedWorkspace(); err != nil {
		t.Fatal(err)
	}
	work, _ := os.ReadFile("go.work")
	if !strings.Contains(string(work), "replace github.com/spidermeaow/graft-framework") {
		t.Fatal(string(work))
	}
	custom := []byte("go 1.26.6\nuse .\n")
	if err := os.WriteFile("go.work", custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureEmbeddedWorkspace(); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"upgrade-project", "--version", "v0.1.0", "--apply"}, &out, &out); err == nil {
		t.Fatal("overwrote user-managed workspace")
	}
	work, _ = os.ReadFile("go.work")
	if !bytes.Equal(work, custom) {
		t.Fatal("changed user workspace")
	}
}
