//go:build windows && graftsetup

// Command setup embeds the CLI and installs it without a source checkout or Go.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

//go:embed payload/graft.bin
var executable []byte

func message(text string, style uintptr) uintptr {
	caption, _ := syscall.UTF16PtrFromString("Graft Setup")
	body, _ := syscall.UTF16PtrFromString(text)
	result, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(caption)), style)
	return result
}

func main() {
	silent := flag.Bool("silent", false, "install without dialogs")
	dir := flag.String("dir", "", "custom installation directory")
	noPath := flag.Bool("no-path", false, "do not update User PATH")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Setup takes no positional arguments")
		os.Exit(1)
	}
	if !*silent {
		text := "Install Graft for the current Windows user?\n\nThe executable will be installed in LOCALAPPDATA\\Graft\\bin and added to User PATH. No administrator rights or Go installation are required."
		if *dir != "" {
			text = "Install Graft in " + *dir + "?\nUser PATH will be updated unless --no-path was specified."
		}
		if *noPath {
			text = "Install Graft for the current user without changing PATH?"
		}
		if message(text, 0x24) != 6 {
			return
		}
	}
	err := install(*dir, *noPath)
	if err != nil {
		if *silent {
			fmt.Fprintln(os.Stderr, err)
		} else {
			message("Installation failed:\n\n"+err.Error(), 0x10)
		}
		os.Exit(1)
	}
	if !*silent {
		message("Graft installed successfully.\n\nRestart terminal applications, then run:\ngraft version\ngraft new my-api\n\nGo 1.26.6+ is required for dev/build/publish.", 0x40)
	}
}

func install(dir string, noPath bool) error {
	tmp, err := os.MkdirTemp("", "graft-setup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "graft.exe")
	if err := os.WriteFile(path, executable, 0700); err != nil {
		return err
	}
	args := []string{"install"}
	if dir != "" {
		args = append(args, "--dir", dir)
	}
	if noPath {
		args = append(args, "--no-path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}
