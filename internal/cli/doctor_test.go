package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestDoctorWithoutDatabaseConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"DATABASE_DRIVER", "DATABASE_URL", "APP_HOST", "APP_PORT", "APP_DOCS", "APP_MAX_CONCURRENT", "APP_MAX_BODY_BYTES", "APP_REQUEST_TIMEOUT"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	if err := os.WriteFile("go.mod", []byte("module example.com/api\n\ngo 1.26.6\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"doctor"}, &out, &out); err != nil {
		t.Fatal(err, out.String())
	}
	text := out.String()
	for _, want := range []string{"Graft doctor", "[ok] Project: module example.com/api", "[warn] Database:", "Doctor found no blocking issues."} {
		if !strings.Contains(text, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, text)
		}
	}
}

func TestDoctorReportsInvalidRuntimeConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"DATABASE_DRIVER", "DATABASE_URL", "APP_HOST", "APP_PORT", "APP_DOCS", "APP_MAX_CONCURRENT", "APP_MAX_BODY_BYTES", "APP_REQUEST_TIMEOUT"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	if err := os.WriteFile(".env", []byte("APP_PORT=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Run(context.Background(), []string{"doctor"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "APP_PORT") || !strings.Contains(out.String(), "[fail] App configuration") {
		t.Fatalf("doctor error = %v, output:\n%s", err, out.String())
	}
}
