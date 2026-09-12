package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMigrationLoadsDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")
	os.Unsetenv("DATABASE_URL")
	if err := os.Mkdir("migrations", 0755); err != nil {
		t.Fatal(err)
	}
	// Invalid connection configuration proves .env was read, without connecting
	// to an actual database or depending on a locally running server.
	if err := os.WriteFile(".env", []byte("DATABASE_URL='not a valid DSN'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), []string{"migrate", "--timeout", "1s"}, io.Discard, io.Discard)
	if os.Getenv("DATABASE_URL") != "not a valid DSN" || err == nil || strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatal("dotenv not consumed:", err)
	}
}

func TestDevelopmentEnvironment(t *testing.T) {
	for _, tt := range []struct {
		env        []string
		host, want string
	}{
		{nil, "", "APP_HOST=127.0.0.1"},
		{[]string{"APP_HOST=0.0.0.0"}, "", "APP_HOST=0.0.0.0"},
		{[]string{"APP_HOST=0.0.0.0"}, "::1", "APP_HOST=::1"},
	} {
		got := strings.Join(developmentEnv(tt.env, tt.host), ";")
		if !strings.Contains(got, tt.want) || !strings.Contains(got, "GRAFT_DEV=1") {
			t.Fatal(got)
		}
	}
}
