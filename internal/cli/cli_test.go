package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArguments(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"new"}, {"new", "../unsafe"}, {"new", "--bad"}, {"publish", "--target", "wrong"}, {"build", "extra"}, {"make:migration", "Bad Name"}, {"migrate", "--timeout", "0"}, {"migrate:rollback", "--step", "-1"}} {
		var out bytes.Buffer
		if err := Run(context.Background(), args, &out, &out); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"help"}, &out, &out); err != nil || !strings.Contains(out.String(), "graft publish") {
		t.Fatal(err, &out)
	}
}

func TestExplicitVersionUsesPublishedModule(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"new", "--download=false", "--version", "v0.2.0", "remote-api"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("remote-api/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "replace") || !strings.Contains(string(data), "v0.2.0") {
		t.Fatal(string(data))
	}
}

func TestProjectGenerationAndBuild(t *testing.T) {
	source, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	args := []string{"new", "--framework", source, "--module", "example.com/hello", "hello-api"}
	if err := Run(context.Background(), args, &out, &out); err != nil {
		t.Fatal(err, &out)
	}
	for _, path := range []string{"go.mod", "cmd/api/main.go", "README.md", ".gitignore", ".env.example", "internal", "migrations"} {
		if _, err := os.Stat(filepath.Join("hello-api", path)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile("hello-api/go.mod")
	if err != nil || !strings.Contains(string(b), "module example.com/hello") || strings.Contains(string(b), "// graft: embedded-framework") || !strings.Contains(string(b), "replace github.com/spidermeaow/graft-framework") {
		t.Fatal(string(b), err)
	}
	if _, err := os.Stat("hello-api/.graft"); !os.IsNotExist(err) {
		t.Fatalf("generated .graft directory: %v", err)
	}
	if err := Run(context.Background(), args, &out, &out); err == nil {
		t.Fatal("overwrote existing project")
	}
	t.Chdir("hello-api")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	output := filepath.Join("bin", "hello.exe")
	if err := Run(ctx, []string{"build", "--output", output}, &out, &out); err != nil {
		t.Fatal(err, &out)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatal(info, err)
	}
	for _, name := range []string{".env.example", "README.md"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(output), name)); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	binary, err := filepath.Abs(output)
	if err != nil {
		t.Fatal(err)
	}
	process := exec.CommandContext(ctx, binary)
	if err := os.WriteFile(".env", []byte(fmt.Sprintf("APP_PORT=%d\n", port)), 0600); err != nil {
		t.Fatal(err)
	}
	process.Env = []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "APP_PORT") {
			process.Env = append(process.Env, entry)
		}
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	client := http.Client{Timeout: time.Second}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get(base + "/")
		if err == nil {
			var body map[string]string
			err = json.NewDecoder(response.Body).Decode(&body)
			response.Body.Close()
			if err != nil || response.StatusCode != 200 || body["message"] != "Hello Graft" {
				t.Fatalf("generated API: %v %v", body, err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("generated binary did not start:", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, path := range []string{"/swagger", "/openapi.json"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s: %d", path, response.StatusCode)
		}
	}
}

func TestTargets(t *testing.T) {
	want := map[string][2]string{"linux-x64": {"linux", "amd64"}, "linux-arm64": {"linux", "arm64"}, "windows-x64": {"windows", "amd64"}, "windows-arm64": {"windows", "arm64"}, "darwin-x64": {"darwin", "amd64"}, "darwin-arm64": {"darwin", "arm64"}}
	if len(targets) != len(want) {
		t.Fatal(targets)
	}
	for target, pair := range want {
		if targets[target] != pair {
			t.Fatal(target)
		}
	}
	env := replaceEnv([]string{"GOOS=wrong", "Path=kept", "goos=also_wrong"}, "GOOS", "linux")
	if strings.Join(env, ";") != "Path=kept;GOOS=linux" {
		t.Fatal(env)
	}
}

func TestDevURLs(t *testing.T) {
	for _, tt := range []struct {
		port, app, swagger string
	}{
		{"", "http://localhost:8080", "http://localhost:8080/swagger"},
		{"4567", "http://localhost:4567", "http://localhost:4567/swagger"},
	} {
		app, swagger := devURLs(tt.port)
		if app != tt.app || swagger != tt.swagger {
			t.Fatalf("devURLs(%q) = %q, %q", tt.port, app, swagger)
		}
	}
}

func TestMakeMigration(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 12, 1, 2, 3, 0, time.UTC)
	a, err := makeMigration(dir, "first", now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := makeMigration(dir, "second", now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(a) != "20260912010203_first.sql" || filepath.Base(b) != "20260912010204_second.sql" {
		t.Fatal(a, b)
	}
	data, err := os.ReadFile(a)
	if err != nil || !strings.Contains(string(data), "-- +graft Up") || !strings.Contains(string(data), "-- +graft Down") {
		t.Fatal(string(data), err)
	}
}
