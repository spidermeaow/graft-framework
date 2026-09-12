package graft

import (
	"fmt"
	"net"
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

// PrintStartupAddress reports the configured HTTP bind address. Wildcard binds
// remain visible instead of being presented as localhost-only access.
func PrintStartupAddress(address string, docs bool) {
	fmt.Fprintln(os.Stdout, "Starting server")
	fmt.Fprintln(os.Stdout, "  Listen: ", address)
	host, _, err := net.SplitHostPort(address)
	if err == nil && host != "" && host != "0.0.0.0" && host != "::" {
		fmt.Fprintln(os.Stdout, "  App:    ", "http://"+address)
		if docs {
			fmt.Fprintln(os.Stdout, "  Swagger:", "http://"+address+"/swagger")
		}
	} else {
		fmt.Fprintln(os.Stdout, "  Accepting connections on all interfaces.")
		if docs {
			fmt.Fprintln(os.Stdout, "  Swagger: /swagger")
		}
	}
	fmt.Fprintln(os.Stdout, "Press Ctrl+C to stop.")
}
