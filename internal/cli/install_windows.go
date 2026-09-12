package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const userPathScript = `
$ErrorActionPreference = 'Stop'
$destination = $env:GRAFT_INSTALL_DIRECTORY
$old = [Environment]::GetEnvironmentVariable('Path', 'User')
$found = $false
foreach ($entry in ($old -split ';')) {
    if ([string]::IsNullOrWhiteSpace($entry)) { continue }
    $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
    if ([string]::Equals($expanded, $destination.TrimEnd('\', '/'), [StringComparison]::OrdinalIgnoreCase)) { $found = $true }
}
if (-not $found) {
    $updated = $destination
    if (-not [string]::IsNullOrEmpty($old)) { $updated += ';' + $old }
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
}
`

func installUserPath(ctx context.Context, directory string) error {
	// Windows includes PowerShell. Data is passed through the environment, never
	// interpolated into script text, and no execution policy is changed.
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", userPathScript)
	cmd.Env = replaceEnv(os.Environ(), "GRAFT_INSTALL_DIRECTORY", directory)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}
