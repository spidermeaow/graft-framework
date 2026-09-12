package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// upgradeProject leaves source snapshots and caches intact. Only an explicitly
// applied, successfully resolved migration changes project dependency files.
func upgradeProject(ctx context.Context, args []string, out, errOut io.Writer) (resultErr error) {
	f := flags("upgrade-project", errOut)
	version := f.String("version", "", "required published framework version")
	apply := f.Bool("apply", false, "verify dependencies, back up files and apply migration")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || !versionName.MatchString(*version) {
		return fmt.Errorf("usage: graft upgrade-project --version vX.Y.Z [--apply]")
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		return err
	}
	work, err := os.ReadFile("go.work")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(work) > 0 && !strings.Contains(string(work), "// graft: embedded-framework") {
		return fmt.Errorf("go.work is user-managed; migrate its framework replacement manually")
	}
	fmt.Fprintf(out, "Use framework %s from Go modules; remove its local replacement and Graft-managed workspace. Keep source snapshots and caches.\n", *version)
	if !*apply {
		fmt.Fprintln(out, "Preview only. Add --apply to verify and save this change.")
		return nil
	}
	stage, err := os.MkdirTemp("", "graft-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	modFile := filepath.Join(stage, "candidate.mod")
	clean := strings.ReplaceAll(string(mod), "// graft: embedded-framework", "")
	if err := os.WriteFile(modFile, []byte(clean), 0600); err != nil {
		return err
	}
	sum, sumErr := os.ReadFile("go.sum")
	if sumErr != nil && !os.IsNotExist(sumErr) {
		return sumErr
	}
	if sumErr == nil {
		if err := os.WriteFile(filepath.Join(stage, "candidate.sum"), sum, 0600); err != nil {
			return err
		}
	}
	env := replaceEnv(os.Environ(), "GOWORK", "off")
	const module = "github.com/spidermeaow/graft-framework"
	if err := runGo(ctx, env, out, errOut, "mod", "edit", "-modfile="+modFile, "-dropreplace="+module, "-require="+module+"@"+*version); err != nil {
		return err
	}
	if err := runGo(ctx, env, out, errOut, "mod", "tidy", "-modfile="+modFile); err != nil {
		return err
	}
	if err := runGo(ctx, env, out, errOut, "build", "-modfile="+modFile, "-o", filepath.Join(stage, "build")+string(os.PathSeparator), "./..."); err != nil {
		return err
	}
	updated, err := os.ReadFile(modFile)
	if err != nil {
		return err
	}
	updatedSum, err := os.ReadFile(filepath.Join(stage, "candidate.sum"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	backup, err := os.MkdirTemp(".", ".graft-upgrade-backup-")
	if err != nil {
		return err
	}
	originals := map[string][]byte{"go.mod": mod}
	if sumErr == nil {
		originals["go.sum"] = sum
	}
	if len(work) > 0 {
		originals["go.work"] = work
	}
	for name, data := range originals {
		if err := os.WriteFile(filepath.Join(backup, name), data, 0600); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Backup:", backup)
	committed := false
	defer func() {
		if committed {
			return
		}
		for name, data := range originals {
			if err := os.WriteFile(name, data, 0644); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("restore %s from %s: %w", name, backup, err))
			}
		}
		if sumErr != nil {
			if err := os.Remove("go.sum"); err != nil && !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if err := os.WriteFile("go.mod", updated, 0644); err != nil {
		return err
	}
	if err := os.WriteFile("go.sum", updatedSum, 0644); err != nil {
		return err
	}
	if len(work) > 0 {
		if err := os.Remove("go.work"); err != nil {
			return err
		}
	}
	committed = true
	fmt.Fprintln(out, "Project upgraded. Restart your editor's Go language server. Keep the backup until verified.")
	return nil
}
