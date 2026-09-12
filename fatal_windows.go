//go:build windows

package graft

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Fatal prints an error and exits non-zero. A console owned only by this process
// also shows a dialog, so double-click launches remain readable. Existing shells
// and redirected/service execution are never blocked by a dialog.
func Fatal(err error) {
	fmt.Fprintln(os.Stderr, "Application error:", err)
	var mode uint32
	console := syscall.GetConsoleMode(syscall.Handle(os.Stderr.Fd()), &mode) == nil
	var processes [2]uint32
	count, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&processes[0])), uintptr(len(processes)))
	if console && count == 1 {
		caption, _ := syscall.UTF16PtrFromString("Application error")
		body, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(err.Error(), "\x00", ""))
		_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(caption)), 0x10)
	}
	os.Exit(1)
}
