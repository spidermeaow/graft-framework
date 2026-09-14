package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func makeMigration(dir, name string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	lockPath := filepath.Join(dir, ".graft-create.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("lock migration directory (another generator may be running): %w", err)
	}
	defer os.Remove(lockPath)
	if err := lock.Close(); err != nil {
		return "", err
	}
	version, _ := strconv.ParseInt(now.UTC().Format("20060102150405"), 10, 64)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		v, e := strconv.ParseInt(prefix, 10, 64)
		if e == nil && v >= version {
			if v == 1<<63-1 {
				return "", errors.New("migration version overflow")
			}
			version = v + 1
		}
	}
	// O_EXCL also prevents overwrites if two generators choose the same name/version.
	path := filepath.Join(dir, fmt.Sprintf("%d_%s.sql", version, name))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString("-- +graft Up\n\n\n-- +graft Down\n\n")
	closeErr := file.Close()
	return path, errors.Join(writeErr, closeErr)
}
