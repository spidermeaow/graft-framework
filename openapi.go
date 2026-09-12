package graft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spidermeaow/graft-framework/internal/swaggerui"
	"github.com/spidermeaow/graft-framework/openapi"
)

// OpenAPI generates an OpenAPI 3.0.3 JSON document from registered route metadata.
// It returns an error for ServeMux patterns that cannot be represented faithfully.
func (a *App) OpenAPI(title, version string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	paths := map[string]map[string]routeDoc{}
	for _, original := range a.routes {
		r := original
		r.Parameters = append([]parameterDoc(nil), original.Parameters...)
		path := strings.TrimSuffix(r.path, "{$}")
		if strings.Contains(path, "...") {
			return nil, fmt.Errorf("OpenAPI does not support multi-segment wildcard route %s", r.path)
		}
		method := strings.ToLower(r.method)
		switch method {
		case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		default:
			return nil, fmt.Errorf("OpenAPI does not support method %s", r.method)
		}
		params := map[string]bool{}
		for _, segment := range strings.Split(path, "/") {
			if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
				params[segment[1:len(segment)-1]] = true
			}
		}
		seen := map[string]bool{}
		for _, p := range r.Parameters {
			key := p.In + ":" + p.Name
			if seen[key] {
				return nil, fmt.Errorf("duplicate parameter %s on %s", p.Name, path)
			}
			seen[key] = true
			if p.Name == "" {
				return nil, fmt.Errorf("empty parameter on %s", path)
			}
			if p.In == "path" && !params[p.Name] {
				return nil, fmt.Errorf("path parameter %s does not exist on %s", p.Name, path)
			}
		}
		for _, segment := range strings.Split(path, "/") {
			if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
				continue
			}
			name := segment[1 : len(segment)-1]
			if !seen["path:"+name] {
				r.Parameters = append(r.Parameters, parameterDoc{Name: name, In: "path", Required: true, Schema: openapi.Schema{Type: "string"}})
			}
		}
		if paths[path] == nil {
			paths[path] = map[string]routeDoc{}
		}
		if _, exists := paths[path][method]; exists {
			return nil, fmt.Errorf("duplicate OpenAPI operation %s %s", method, path)
		}
		paths[path][method] = r
	}
	document := struct {
		OpenAPI string                         `json:"openapi"`
		Info    map[string]string              `json:"info"`
		Paths   map[string]map[string]routeDoc `json:"paths"`
	}{"3.0.3", map[string]string{"title": title, "version": version}, paths}
	return json.MarshalIndent(document, "", "  ")
}

// Docs enables /openapi.json and an embedded, offline-capable Swagger UI at /swagger.
// It must be called before serving. Documentation routes are not included in the spec.
func (a *App) Docs(title, version string) {
	a.configure(func() {
		a.mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
			b, err := a.OpenAPI(title, version)
			if err != nil {
				handleError(w, r, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(b)
		})
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
		a.mux.Handle("GET /swagger", serve)
		a.mux.Handle("GET /swagger/", serve)
	})
}
