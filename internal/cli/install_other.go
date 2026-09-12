//go:build !windows

package cli

import (
	"context"
	"fmt"
)

func installUserPath(context.Context, string) error { return fmt.Errorf("Windows required") }
