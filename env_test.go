package graft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvValues(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"plain # comment", "plain"}, {"postgres://user:p#ss@host/db", "postgres://user:p#ss@host/db"},
		{`"hello\nworld" # comment`, "hello\nworld"}, {`'C:\work\$HOME'`, `C:\work\$HOME`}, {"", ""}, {"# comment", ""}, {"a=b", "a=b"},
	} {
		got, err := envValue(tt.input)
		if err != nil || got != tt.want {
			t.Fatalf("%q: %q %v", tt.input, got, err)
		}
	}
	for _, value := range []string{`"unclosed`, `'unclosed`, `"ok" garbage`, `"\q"`, `"\x00"`} {
		if _, err := envValue(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestLoadEnv(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".env")
	keys := []string{"GRAFT_ENV_TEST_VALUE", "GRAFT_ENV_TEST_EXISTING", "GRAFT_ENV_TEST_EMPTY"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	os.Unsetenv(keys[0])
	t.Setenv(keys[1], "shell")
	data := "\ufeff# comment\r\nexport " + keys[0] + " = 'from file'\r\n" + keys[1] + "=file\n" + keys[2] + "=file\n"
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnv(file); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(keys[0]) != "from file" || os.Getenv(keys[1]) != "shell" || os.Getenv(keys[2]) != "" {
		t.Fatal("precedence incorrect")
	}
	os.Unsetenv(keys[0])
	os.WriteFile(file, []byte(keys[0]+"=new\nSECRET invalid-password\n"), 0600)
	err := LoadEnv(file)
	if err == nil || strings.Contains(err.Error(), "password") {
		t.Fatalf("unsafe error: %v", err)
	}
	if _, exists := os.LookupEnv(keys[0]); exists {
		t.Fatal("syntax error partially changed environment")
	}
	if err := LoadEnv(filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultEnvFilesIncludesExecutableDirectory(t *testing.T) {
	files := defaultEnvFiles()
	if len(files) == 0 || files[len(files)-1] != ".env" {
		t.Fatalf("working-directory .env missing: %q", files)
	}
	if executable, err := os.Executable(); err == nil {
		want := filepath.Join(filepath.Dir(executable), ".env")
		if files[0] != want {
			t.Fatalf("executable .env = %q, want %q", files[0], want)
		}
	}
}
