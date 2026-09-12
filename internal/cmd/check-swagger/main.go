// Command check-swagger verifies pinned browser assets and checks published OSV
// advisories. Go vulnerability scanners do not analyze embedded JavaScript.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	var manifest struct {
		Package string
		Version string
		SHA256  map[string]string `json:"sha256"`
	}
	data, err := os.ReadFile("internal/swaggerui/manifest.json")
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.Package != "swagger-ui-dist" || manifest.Version == "" || len(manifest.SHA256) < 5 {
		return fmt.Errorf("incomplete Swagger manifest")
	}
	for name, want := range manifest.SHA256 {
		if filepath.Base(name) != name {
			return fmt.Errorf("invalid asset name")
		}
		data, err := os.ReadFile(filepath.Join("internal/swaggerui/assets", name))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			return fmt.Errorf("asset checksum mismatch: %s", name)
		}
	}
	client := http.Client{Timeout: 30 * time.Second}
	// Both package names are checked: advisories may name the source package.
	for _, name := range []string{manifest.Package, "swagger-ui"} {
		body, _ := json.Marshal(map[string]any{"package": map[string]string{"name": name, "ecosystem": "npm"}, "version": manifest.Version})
		response, err := client.Post("https://api.osv.dev/v1/query", "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			return fmt.Errorf("OSV query failed: HTTP %d", response.StatusCode)
		}
		var result struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		}
		if err = json.Unmarshal(data, &result); err != nil {
			return err
		}
		if len(result.Vulns) > 0 {
			return fmt.Errorf("%s %s has advisory findings: %v", name, manifest.Version, result.Vulns)
		}
	}
	fmt.Printf("Swagger UI %s: asset checksums verified; no OSV advisories found for checked package versions.\n", manifest.Version)
	return nil
}
