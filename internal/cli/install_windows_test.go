package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserPathScript(t *testing.T) {
	// Exercise the actual Windows PowerShell script while substituting only its
	// registry read/write boundary. Tests must not change the developer's User PATH.
	script := strings.ReplaceAll(userPathScript, "[Environment]::GetEnvironmentVariable('Path', 'User')", "$env:GRAFT_TEST_OLD_PATH")
	script = strings.ReplaceAll(script, "[Environment]::SetEnvironmentVariable('Path', $updated, 'User')", "[Console]::Write($updated)")
	for _, tt := range []struct{ name, old, directory, want string }{
		{"empty", "", `C:\Apps\Graft`, `C:\Apps\Graft`},
		{"preserve", `C:\Other;%SOME_VAR%\bin`, `C:\Apps\Graft`, `C:\Apps\Graft;C:\Other;%SOME_VAR%\bin`},
		{"duplicate", `C:\Other;c:\apps\graft\`, `C:\Apps\Graft`, ""},
		{"quoted", `"C:\Apps\Graft";C:\Other`, `C:\Apps\Graft`, ""},
		{"special", `C:\Other`, `C:\Apps\it's $data`, `C:\Apps\it's $data;C:\Other`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", script)
			cmd.Env = replaceEnv(replaceEnv(os.Environ(), "GRAFT_TEST_OLD_PATH", tt.old), "GRAFT_INSTALL_DIRECTORY", tt.directory)
			output, err := cmd.CombinedOutput()
			if err != nil || string(output) != tt.want {
				t.Fatalf("%v: got %q, want %q", err, output, tt.want)
			}
		})
	}
}
