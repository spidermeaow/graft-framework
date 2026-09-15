package graft

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spidermeaow/graft-framework/openapi"
)

func TestOpenAPI(t *testing.T) {
	a := New()
	schema := openapi.Schema{Type: "object", Properties: map[string]openapi.Schema{"name": {Type: "string"}}, Required: []string{"name"}}
	a.Group("/api").POST("/users/{id}", func(c *Context) error { return c.JSON(201, nil) }, Summary("Create user"), Tag("Users"), RequestBody(schema), ResponseSchema(201, "Created", schema), PathParameter("id", openapi.Schema{Type: "integer"}), QueryParameter("notify", false, openapi.Schema{Type: "boolean"}))
	schema.Properties["name"] = openapi.Schema{Type: "number"}
	a.GET("/{$}", func(*Context) error { return nil })
	a.Docs("Test API", "1.0")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc struct {
		OpenAPI string                         `json:"openapi"`
		Paths   map[string]map[string]routeDoc `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	r := doc.Paths["/api/users/{id}"]["post"]
	if w.Code != 200 || doc.OpenAPI != "3.0.3" || r.Summary != "Create user" || len(r.Tags) != 1 || len(r.Parameters) != 2 || r.Responses["201"].Description != "Created" {
		t.Fatalf("%s", w.Body)
	}
	if r.RequestBody.Content["application/json"].Schema.Properties["name"].Type != "string" {
		t.Fatal("caller mutated registered schema")
	}
	if _, ok := doc.Paths["/"]; !ok {
		t.Fatal("exact-root pattern not normalized")
	}
	if _, ok := doc.Paths["/swagger"]; ok {
		t.Fatal("docs included in paths")
	}
	first, _ := a.OpenAPI("Test API", "1.0")
	second, _ := a.OpenAPI("Test API", "1.0")
	if string(first) != string(second) {
		t.Fatal("nondeterministic JSON")
	}
}

func TestSwaggerAssets(t *testing.T) {
	a := New()
	a.Docs("Test", "1")
	for _, path := range []string{"/swagger", "/swagger/", "/swagger/swagger-ui.css", "/swagger/swagger-ui-bundle.js", "/swagger/initializer.js"} {
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if path == "/swagger" && (!strings.Contains(w.Body.String(), "swagger-ui-bundle.js") || strings.Contains(w.Body.String(), "https://")) {
			t.Fatal("missing or external assets")
		}
	}
}

func TestOpenAPIParametersAndUnsupportedPatterns(t *testing.T) {
	a := New()
	a.GET("/things/{id}", func(*Context) error { return nil })
	b, err := a.OpenAPI("t", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"required": true`) {
		t.Fatal(string(b))
	}
	a = New()
	a.GET("/files/{path...}", func(*Context) error { return nil })
	if _, err = a.OpenAPI("t", "1"); err == nil {
		t.Fatal("catchall silently misrepresented")
	}
}
