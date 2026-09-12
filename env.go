package graft

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadEnv loads a dotenv file without overwriting existing environment variables,
// including explicitly empty values. With no filename it reads .env alongside
// the running executable and then .env in the working directory; working-directory
// values take precedence when both files exist. A missing file is allowed. Syntax
// errors expose only line numbers, never values. No variable interpolation or
// shell execution occurs. Call it at startup before reading configuration or
// starting goroutines.
func LoadEnv(filenames ...string) error {
	if len(filenames) == 0 {
		filenames = defaultEnvFiles()
	}
	values := map[string]string{}
	for _, filename := range filenames {
		file, err := os.Open(filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			if line == 1 {
				text = strings.TrimPrefix(text, "\ufeff")
			}
			text = strings.TrimSpace(text)
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			if strings.HasPrefix(text, "export ") || strings.HasPrefix(text, "export\t") {
				text = strings.TrimSpace(text[6:])
			}
			key, value, ok := strings.Cut(text, "=")
			key = strings.TrimSpace(key)
			if !ok || !envName.MatchString(key) {
				file.Close()
				return fmt.Errorf("%s:%d: invalid environment assignment", filename, line)
			}
			parsed, err := envValue(strings.TrimSpace(value))
			if err != nil {
				file.Close()
				return fmt.Errorf("%s:%d: invalid environment value", filename, line)
			}
			values[key] = parsed
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return fmt.Errorf("%s: cannot read environment file", filename)
		}
	}
	// Parse every file before changing process state.
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("cannot set environment variable %s", key)
			}
		}
	}
	return nil
}

func defaultEnvFiles() []string {
	files := make([]string, 0, 2)
	if executable, err := os.Executable(); err == nil {
		files = append(files, filepath.Join(filepath.Dir(executable), ".env"))
	}
	files = append(files, ".env")
	return files
}

func envValue(value string) (string, error) {
	invalid := fmt.Errorf("invalid dotenv value")
	if strings.ContainsRune(value, 0) {
		return "", invalid
	}
	if value == "" {
		return "", nil
	}
	if value[0] == '\'' || value[0] == '"' {
		quote := value[0]
		for i := 1; i < len(value); i++ {
			if quote == '"' && value[i] == '\\' {
				i++
				continue
			}
			if value[i] != quote {
				continue
			}
			tail := strings.TrimSpace(value[i+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return "", invalid
			}
			if quote == '\'' {
				return value[1:i], nil
			}
			decoded, err := strconv.Unquote(value[:i+1])
			if err != nil || strings.ContainsRune(decoded, 0) {
				return "", invalid
			}
			return decoded, nil
		}
		return "", invalid
	}
	for i := 0; i < len(value); i++ {
		if value[i] == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
			return strings.TrimSpace(value[:i]), nil
		}
	}
	return value, nil
}
