package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spidermeaow/graft-framework/licenses"
)

func installCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	f := flags("install", errOut)
	dir := f.String("dir", "", "installation directory (default LOCALAPPDATA/Graft/bin)")
	noPath := f.Bool("no-path", false, "copy executable without updating User PATH")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("install takes no positional arguments")
	}
	if runtime.GOOS != "windows" {
		return fmt.Errorf("self-install currently supports Windows only; place graft on PATH manually")
	}
	if *dir == "" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return fmt.Errorf("LOCALAPPDATA is unset; use --dir")
		}
		*dir = filepath.Join(base, "Graft", "bin")
	}
	destination, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	if strings.ContainsAny(destination, ";\r\n\x00") {
		return fmt.Errorf("invalid installation path")
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	target := filepath.Join(destination, "graft.exe")
	if err := copyExecutable(source, target); err != nil {
		return err
	}
	fmt.Fprintln(out, "Installed:", target)
	if err := os.WriteFile(filepath.Join(destination, "THIRD_PARTY_NOTICES.txt"), []byte(licenses.Text()), 0644); err != nil {
		return fmt.Errorf("write third-party notices: %w", err)
	}
	if !*noPath {
		if err := installUserPath(ctx, destination); err != nil {
			return fmt.Errorf("executable installed, but User PATH update failed: %w", err)
		}
		fmt.Fprintln(out, "User PATH updated. Restart terminal applications, then run: graft version")
		fmt.Fprintln(out, "For this CMD session: set \"PATH="+destination+";%PATH%\"")
	}
	return nil
}

func copyExecutable(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(target)
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".graft-install-*.exe")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace executable (close other running graft processes first): %w", err)
	}
	return nil
}
