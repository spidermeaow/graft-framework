package graft_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/openapi"
	"github.com/spidermeaow/graft-framework/toolkit/auth"
)

type externalProvider struct{ metadata graft.OpenAPIMetadata }

func (p externalProvider) OpenAPIMetadata() graft.OpenAPIMetadata { return p.metadata }

func spec(t *testing.T, a *graft.App) map[string]any {
	t.Helper()
	data, err := a.OpenAPI("test", "1")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func operation(t *testing.T, doc map[string]any, path, method string) map[string]any {
	t.Helper()
	return doc["paths"].(map[string]any)[path].(map[string]any)[method].(map[string]any)
}

func TestContextAwareMiddlewareAndLegacyRoutes(t *testing.T) {
	a := graft.New()
	public := func(c *graft.Context) error { return c.JSON(200, map[string]bool{"ok": true}) }
	a.GET("/public", public, graft.Summary("Public"))
	api := a.Group("/api")
	bearer := auth.JWT(auth.JWTConfig{Issuer: "https://issuer.example", Audience: "api", HMACSecret: []byte("0123456789abcdef0123456789abcdef")})
	api.UseDocumented(auth.Required(bearer), graft.DocumentedRateLimit(1, 1))
	metadata := externalProvider{graft.OpenAPIMetadata{
		Tags: []string{"Users", "Users"}, OperationID: "readMe",
		Parameters: []graft.OpenAPIParameter{{Name: "X-Tenant", In: "header", Required: true, Schema: openapi.Schema{Type: "string"}}},
		Responses:  map[int]graft.OpenAPIResponse{401: {Description: "Unauthorized"}, 200: {Description: "User", Content: map[string]openapi.Schema{"application/json": {Type: "object"}}}},
		Schemas:    map[string]openapi.Schema{"User": {Type: "object"}},
	}}
	api.GET("/me", public, graft.Metadata(metadata), graft.Tag("Users"), graft.Deprecated())
	api.GET("/other", public)
	doc := spec(t, a)
	pub := operation(t, doc, "/public", "get")
	if pub["summary"] != "Public" {
		t.Fatal(pub)
	}
	if _, exists := pub["security"]; exists {
		t.Fatal("public route protected", pub)
	}
	me := operation(t, doc, "/api/me", "get")
	if me["operationId"] != "readMe" || me["deprecated"] != true {
		t.Fatal(me)
	}
	if len(me["tags"].([]any)) != 1 || len(me["parameters"].([]any)) != 1 {
		t.Fatal(me)
	}
	responses := me["responses"].(map[string]any)
	if len(responses) != 3 || responses["401"] == nil || responses["429"] == nil || responses["200"] == nil {
		t.Fatal(responses)
	}
	security := me["security"].([]any)
	if len(security) != 1 || security[0].(map[string]any)["BearerAuth"] == nil {
		t.Fatal(security)
	}
	components := doc["components"].(map[string]any)
	if len(components["securitySchemes"].(map[string]any)) != 1 || components["schemas"].(map[string]any)["User"] == nil {
		t.Fatal(components)
	}
	other := operation(t, doc, "/api/other", "get")
	if other["security"] == nil || other["responses"].(map[string]any)["401"] == nil {
		t.Fatal(other)
	}
	request := httptest.NewRequest("GET", "/api/me", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, request)
	if w.Code != 401 {
		t.Fatal("runtime auth missing", w.Code)
	}
}

func TestRouteScopedAPIKeyAndCustomCookie(t *testing.T) {
	a := graft.New()
	h := func(c *graft.Context) error { return c.String(200, "ok") }
	key := auth.APIKey("0123456789abcdef", auth.Principal{Subject: "service"})
	a.GET("/key", h, graft.With(auth.Required(key)))
	cookie := externalProvider{graft.OpenAPIMetadata{
		Security:        []map[string][]string{{"SessionCookie": {}}},
		SecuritySchemes: map[string]map[string]any{"SessionCookie": {"type": "apiKey", "in": "cookie", "name": "session"}},
		Responses:       map[int]graft.OpenAPIResponse{401: {Description: "Unauthorized"}},
	}}
	cookieMiddleware := graft.Document(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie("session"); err != nil {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, cookie)
	a.GET("/cookie", h, graft.With(cookieMiddleware))
	doc := spec(t, a)
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	if len(schemes) != 2 || schemes["ApiKeyAuth"] == nil || schemes["SessionCookie"] == nil {
		t.Fatal(schemes)
	}
	if operation(t, doc, "/key", "get")["security"] == nil || operation(t, doc, "/cookie", "get")["security"] == nil {
		t.Fatal(doc)
	}
	r := httptest.NewRequest("GET", "/key", nil)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r.Header.Set("X-API-Key", "0123456789abcdef")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestMetadataMergeComponentsAndConflicts(t *testing.T) {
	a := graft.New()
	h := func(*graft.Context) error { return nil }
	provider := externalProvider{graft.OpenAPIMetadata{
		RequestBody:         &graft.OpenAPIRequestBody{Required: true, Content: map[string]openapi.Schema{"multipart/form-data": {Type: "object"}}},
		Responses:           map[int]graft.OpenAPIResponse{400: {Description: "Bad Request"}},
		ComponentResponses:  map[string]graft.OpenAPIResponse{"BadRequest": {Description: "Bad Request"}},
		ComponentParameters: map[string]graft.OpenAPIParameter{"Tenant": {Name: "X-Tenant", In: "header", Schema: openapi.Schema{Type: "string"}}},
		Schemas:             map[string]openapi.Schema{"Upload": {Type: "object"}},
	}}
	a.POST("/upload", h, graft.Metadata(provider), graft.Metadata(provider), graft.ParameterReference("Tenant"), graft.ResponseReference(422, "BadRequest"), graft.Response(201, "Created", openapi.Schema{Ref: "#/components/schemas/Upload"}))
	doc := spec(t, a)
	op := operation(t, doc, "/upload", "post")
	if op["requestBody"].(map[string]any)["content"].(map[string]any)["multipart/form-data"] == nil {
		t.Fatal(op)
	}
	if len(op["responses"].(map[string]any)) != 3 || op["responses"].(map[string]any)["422"].(map[string]any)["$ref"] != "#/components/responses/BadRequest" {
		t.Fatal(op)
	}
	if op["parameters"].([]any)[0].(map[string]any)["$ref"] != "#/components/parameters/Tenant" {
		t.Fatal(op)
	}
	created := op["responses"].(map[string]any)["201"].(map[string]any)
	schema := created["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["$ref"] != "#/components/schemas/Upload" {
		t.Fatal(created)
	}
	components := doc["components"].(map[string]any)
	if components["responses"].(map[string]any)["BadRequest"] == nil || components["parameters"].(map[string]any)["Tenant"] == nil {
		t.Fatal(components)
	}

	b := graft.New()
	b.GET("/one", h, graft.SecurityScheme("Custom", map[string]any{"type": "apiKey", "in": "header", "name": "X-One"}), graft.Security("Custom"))
	b.GET("/two", h, graft.SecurityScheme("Custom", map[string]any{"type": "apiKey", "in": "header", "name": "X-Two"}), graft.Security("Custom"))
	if _, err := b.OpenAPI("test", "1"); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatal(err)
	}
}

func TestMultipleSecurityRequirementsUseAND(t *testing.T) {
	a := graft.New()
	h := func(*graft.Context) error { return nil }
	jwt := auth.JWT(auth.JWTConfig{Issuer: "issuer", Audience: "api", HMACSecret: []byte("0123456789abcdef0123456789abcdef")})
	key := auth.APIKey("0123456789abcdef", auth.Principal{Subject: "service"})
	g := a.Group("/private")
	g.UseDocumented(auth.Required(jwt))
	g.GET("/both", h, graft.With(auth.Required(key)), graft.Response(200, "OK", openapi.Schema{}))
	doc := spec(t, a)
	security := operation(t, doc, "/private/both", "get")["security"].([]any)
	if len(security) != 1 {
		t.Fatal(security)
	}
	required := security[0].(map[string]any)
	if required["BearerAuth"] == nil || required["ApiKeyAuth"] == nil {
		t.Fatal(required)
	}
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	if len(schemes) != 2 {
		t.Fatal(schemes)
	}
}

func TestAppWideMetadataAndSnapshot(t *testing.T) {
	a := graft.New()
	meta := graft.OpenAPIMetadata{
		Tags:            []string{"Global"},
		Security:        []map[string][]string{{"Custom": {}}},
		SecuritySchemes: map[string]map[string]any{"Custom": {"type": "apiKey", "in": "header", "name": "X-Custom"}},
	}
	component := graft.Document(func(next http.Handler) http.Handler { return next }, externalProvider{meta})
	a.UseDocumented(component)
	a.GET("/first", func(*graft.Context) error { return nil })
	meta.Tags[0] = "Changed"
	meta.SecuritySchemes["Custom"]["name"] = "X-Changed"
	child := a.Group("/nested")
	child.GET("/second", func(*graft.Context) error { return nil })
	doc := spec(t, a)
	first := operation(t, doc, "/first", "get")
	second := operation(t, doc, "/nested/second", "get")
	if first["tags"].([]any)[0] != "Global" || second["tags"].([]any)[0] != "Global" {
		t.Fatal(first, second)
	}
	scheme := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)["Custom"].(map[string]any)
	if scheme["name"] != "X-Custom" {
		t.Fatal(scheme)
	}
}
