//go:build !windows

package graft

import (
	"fmt"
	"os"
)

// Fatal reports a startup error and exits with a non-zero status.
func Fatal(err error) {
	fmt.Fprintln(os.Stderr, "Application error:", err)
	os.Exit(1)
}
