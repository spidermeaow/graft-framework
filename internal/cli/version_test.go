package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	for _, command := range []string{"version", "--version", "-v"} {
		var output bytes.Buffer
		if err := Run(context.Background(), []string{command}, &output, io.Discard); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(output.String(), "Graft 0.2.5-dev (go") {
			t.Fatalf("unexpected version: %q", output.String())
		}
	}
	for _, args := range [][]string{{"version", "extra"}, {"version", "--unknown"}} {
		if err := Run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestPublishedVersion(t *testing.T) {
	for _, value := range []string{"v0.1.0", "v0.2.0-rc.1"} {
		if !publishedVersion(value) {
			t.Errorf("release not detected: %s", value)
		}
	}
	for _, value := range []string{"", "(devel)", "v0.0.0-20260912053937-efab56689388", "v0.1.0+dirty", "v0.0.0-20260912053937-efab56689388+dirty"} {
		if publishedVersion(value) {
			t.Errorf("development build treated as release: %s", value)
		}
	}
}
