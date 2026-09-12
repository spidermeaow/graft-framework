package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyExecutable(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.exe")
	target := filepath.Join(dir, "installed", "graft.exe")
	if err := os.WriteFile(source, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := copyExecutable(source, target); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(source, []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, target); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "second" {
		t.Fatal(string(b), err)
	}
	if err := copyExecutable(target, target); err != nil {
		t.Fatal(err)
	}
}
