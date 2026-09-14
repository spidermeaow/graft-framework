package frameworkbundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractAndBuild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "framework")
	if err := Extract(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"go.mod", "app.go", "migration/postgres/postgres.go", "toolkit/auth/auth.go", "toolkit/validate/validate.go", "toolkit/api/api.go", "toolkit/testkit/testkit.go", "internal/swaggerui/assets/LICENSE", "internal/swaggerui/assets/swagger-ui-bundle.js"} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if err := Extract(dir); err == nil {
		t.Fatal("overwrote existing bundle")
	}
}
