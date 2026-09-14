package graft

import (
	"github.com/spidermeaow/graft-framework/internal/swaggerui"
	"net/http"
)

// Docs enables /openapi.json and an embedded, offline-capable Swagger UI at /swagger.
// It must be called before serving. Documentation routes are not included in the spec.
func (a *App) Docs(title, version string) {
	a.DocsWithMiddleware(title, version)
}

// DocsWithMiddleware protects the spec, UI and every asset with the same middleware.
// Omit the call entirely to disable documentation. App middleware still applies.
func (a *App) DocsWithMiddleware(title, version string, middleware ...Middleware) {
	a.configure(func() {
		a.mux.Handle("GET /openapi.json", chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, err := a.OpenAPI(title, version)
			if err != nil {
				handleError(w, r, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(b)
		}), middleware))
		assets := http.StripPrefix("/swagger/", swaggerui.Handler())
		serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
			if r.URL.Path == "/swagger" {
				clone := r.Clone(r.Context())
				url := *r.URL
				url.Path = "/swagger/"
				clone.URL = &url
				assets.ServeHTTP(w, clone)
				return
			}
			assets.ServeHTTP(w, r)
		})
		a.mux.Handle("GET /swagger", chain(serve, middleware))
		a.mux.Handle("GET /swagger/", chain(serve, middleware))
	})
}
