package graft_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/openapi"
)

type createUserRequest struct {
	Name  string `json:"name" validate:"required,min=3,max=80"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"min=18,max=100"`
}

type userResponse struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

type errorResponse struct {
	Error string `json:"error" validate:"required"`
}

func TestGenericContractGeneratesOpenAPIFromStructTags(t *testing.T) {
	app := graft.New()
	app.PUT(
		"/api/users/{id}",
		func(c *graft.Context) error { return c.JSON(200, userResponse{}) },
		graft.Summary("Update user"),
		graft.Tag("Users"),
		graft.Path[int]("id"),
		graft.Query[string]("locale"),
		graft.QueryOptional[bool]("sendEmail"),
		graft.Body[createUserRequest](),
		graft.Response[userResponse](200),
		graft.Response[errorResponse](400),
		graft.Response[errorResponse](404),
	)

	document := openAPIDocument(t, app)
	operation := document["paths"].(map[string]any)["/api/users/{id}"].(map[string]any)["put"].(map[string]any)
	bodySchema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	properties := bodySchema["properties"].(map[string]any)
	name := properties["name"].(map[string]any)
	email := properties["email"].(map[string]any)
	age := properties["age"].(map[string]any)
	if name["type"] != "string" || name["minLength"] != float64(3) || name["maxLength"] != float64(80) {
		t.Fatalf("unexpected name schema: %#v", name)
	}
	if email["format"] != "email" || age["minimum"] != float64(18) || age["maximum"] != float64(100) {
		t.Fatalf("unexpected validation schemas: email=%#v age=%#v", email, age)
	}
	required := bodySchema["required"].([]any)
	if len(required) != 2 || required[0] != "name" || required[1] != "email" {
		t.Fatalf("unexpected required fields: %#v", required)
	}

	parameters := operation["parameters"].([]any)
	if len(parameters) != 3 {
		t.Fatalf("generic path parameter must replace automatic fallback: %#v", parameters)
	}
	assertParameter(t, parameters, "path", "id", true, "integer")
	assertParameter(t, parameters, "query", "locale", true, "string")
	assertParameter(t, parameters, "query", "sendEmail", false, "boolean")

	responses := operation["responses"].(map[string]any)
	if responses["200"].(map[string]any)["description"] != "OK" || responses["404"].(map[string]any)["description"] != "Not Found" {
		t.Fatalf("unexpected response descriptions: %#v", responses)
	}
	responseSchema := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	responseProperties := responseSchema["properties"].(map[string]any)
	if responseProperties["id"].(map[string]any)["format"] != "int64" || responseProperties["createdAt"].(map[string]any)["format"] != "date-time" {
		t.Fatalf("unexpected response schema: %#v", responseSchema)
	}
}

func TestManualSchemaAPIRemainsAvailable(t *testing.T) {
	app := graft.New()
	app.POST("/manual", func(*graft.Context) error { return nil },
		graft.RequestBody(openapi.Schema{Type: "string", Format: "binary"}),
		graft.ResponseSchema(202, "Queued", openapi.Schema{Type: "object"}),
	)
	document := openAPIDocument(t, app)
	operation := document["paths"].(map[string]any)["/manual"].(map[string]any)["post"].(map[string]any)
	if operation["responses"].(map[string]any)["202"].(map[string]any)["description"] != "Queued" {
		t.Fatal("manual response schema was not retained")
	}
}

func TestSchemaOfRejectsUnsupportedTypes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected unsupported type to panic during route setup")
		}
	}()
	_ = graft.SchemaOf[func()]()
}

func openAPIDocument(t *testing.T, app *graft.App) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	app.Docs("Contracts", "1.0")
	app.ServeHTTP(response, httptest.NewRequest("GET", "/openapi.json", nil))
	if response.Code != 200 {
		t.Fatalf("OpenAPI returned %d: %s", response.Code, response.Body)
	}
	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func assertParameter(t *testing.T, parameters []any, in, name string, required bool, schemaType string) {
	t.Helper()
	for _, raw := range parameters {
		parameter := raw.(map[string]any)
		if parameter["in"] == in && parameter["name"] == name {
			if parameter["required"] != required || parameter["schema"].(map[string]any)["type"] != schemaType {
				t.Fatalf("unexpected parameter: %#v", parameter)
			}
			return
		}
	}
	t.Fatalf("missing %s parameter %s", in, name)
}
