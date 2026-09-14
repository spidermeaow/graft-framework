package migration

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var filename = regexp.MustCompile(`^([0-9]+)_([a-z][a-z0-9_]*)\.sql$`)

// Load reads SQL files in dir, checks versions/markers, and orders them numerically.
// The checksum covers exact file bytes, including comments and line endings.
func Load(files fs.FS, dir string) ([]Migration, error) {
	return LoadDialect(files, dir, "postgres")
}

// LoadDialect loads PostgreSQL or MySQL migration SQL without translating it.
func LoadDialect(files fs.FS, dir, dialect string) ([]Migration, error) {
	if dialect != "postgres" && dialect != "mysql" {
		return nil, fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, err
	}
	var result []Migration
	versions := map[int64]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := filename.FindStringSubmatch(entry.Name())
		if parts == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if versions[version] {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		versions[version] = true
		path := entry.Name()
		if dir != "." {
			path = dir + "/" + path
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return nil, err
		}
		up, down, err := parse(string(data), dialect)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		result = append(result, Migration{Dialect: dialect, Version: version, Name: parts[2], Up: up, Down: down, Checksum: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func parse(source string, dialect ...string) (string, string, error) {
	var up, down strings.Builder
	section := 0
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		switch strings.TrimSpace(line) {
		case "-- +graft Up":
			if section != 0 {
				return "", "", fmt.Errorf("Up marker must occur once, before Down")
			}
			section = 1
		case "-- +graft Down":
			if section != 1 {
				return "", "", fmt.Errorf("Down marker must occur once, after Up")
			}
			section = 2
		default:
			if strings.Contains(line, "-- +graft ") {
				return "", "", fmt.Errorf("unknown or misplaced graft marker")
			}
			switch section {
			case 0:
				if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "--") {
					return "", "", fmt.Errorf("SQL before Up marker")
				}
			case 1:
				up.WriteString(line + "\n")
			case 2:
				down.WriteString(line + "\n")
			}
		}
	}
	if section != 2 {
		return "", "", fmt.Errorf("both Up and Down markers are required")
	}
	for _, sql := range []string{up.String(), down.String()} {
		if err := validateSQL(sql, dialect...); err != nil {
			return "", "", err
		}
	}
	return strings.TrimSpace(up.String()), strings.TrimSpace(down.String()), nil
}
