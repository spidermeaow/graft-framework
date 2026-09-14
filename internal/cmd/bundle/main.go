// Command bundle generates the framework source archive for standalone CLI builds.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := filepath.ToSlash(path)
		if entry.IsDir() {
			if name == "." || name == "openapi" || name == "migration" || strings.HasPrefix(name, "migration/") || name == "toolkit" || strings.HasPrefix(name, "toolkit/") || name == "internal" || name == "internal/swaggerui" || name == "internal/swaggerui/assets" {
				return nil
			}
			return filepath.SkipDir
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" && name != "LICENSE" && !strings.HasPrefix(name, "internal/swaggerui/assets/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Text checkouts may use CRLF on Windows and LF on release runners.
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		file, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return os.WriteFile("internal/frameworkbundle/framework.zip", buffer.Bytes(), 0644)
}
