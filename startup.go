package graft

import (
	"fmt"
	"os"
)

// PrintStartup reports the local application and Swagger URLs for a generated app.
func PrintStartup(port string) {
	if port == "" {
		port = "8080"
	}
	base := "http://localhost:" + port
	fmt.Fprintln(os.Stdout, "Starting server")
	fmt.Fprintln(os.Stdout, "  App:    ", base)
	fmt.Fprintln(os.Stdout, "  Swagger:", base+"/swagger")
	fmt.Fprintln(os.Stdout, "Press Ctrl+C to stop.")
}
