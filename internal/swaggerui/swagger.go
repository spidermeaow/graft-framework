// Package swaggerui serves the pinned, embedded Swagger UI distribution.
package swaggerui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

// Handler serves assets relative to the request path.
func Handler() http.Handler {
	files, _ := fs.Sub(assets, "assets")
	return http.FileServer(http.FS(files))
}
