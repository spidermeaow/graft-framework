// Package licenses embeds notices for the CLI's Go database drivers.
package licenses

import (
	"embed"
	"strings"
)

//go:embed *-LICENSE SOURCES.txt
var files embed.FS

// Text includes unmodified upstream license texts and source locations.
func Text() string {
	var text strings.Builder
	for _, name := range []string{"SOURCES.txt", "pq-LICENSE", "mysql-LICENSE", "edwards25519-LICENSE"} {
		data, _ := files.ReadFile(name)
		text.WriteString(name + "\n\n")
		text.Write(data)
		text.WriteString("\n\n")
	}
	return text.String()
}
