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
