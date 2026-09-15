package validate

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spidermeaow/graft-framework"
)

func TestCheckAndBind(t *testing.T) {
	type input struct {
		Name  string `json:"name" validate:"required,min=3,max=10"`
		Email string `json:"email" validate:"required,email"`
		Count int    `json:"count" validate:"min=1"`
	}
	var fields Errors
	if err := Check(input{Name: "x", Email: "not-an-email"}); !errors.As(err, &fields) || len(fields) != 3 {
		t.Fatal(err)
	}
	if err := Check(input{Name: "กขค", Email: "dev@example.com", Count: 1}); err != nil {
		t.Fatal("string bounds must count Unicode characters:", err)
	}
	a := graft.New()
	a.POST("/", func(c *graft.Context) error {
		var body input
		if err := Bind(c, &body); err != nil {
			return err
		}
		return c.JSON(200, body)
	})
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"abc","email":"dev@example.com","count":2}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","email":"bad","count":0}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
