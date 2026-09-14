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
		Count int    `json:"count" validate:"min=1"`
	}
	var fields Errors
	if err := Check(input{Name: "x"}); !errors.As(err, &fields) || len(fields) != 2 {
		t.Fatal(err)
	}
	a := graft.New()
	a.POST("/", func(c *graft.Context) error {
		var body input
		if err := Bind(c, &body); err != nil {
			return err
		}
		return c.JSON(200, body)
	})
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"abc","count":2}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","count":0}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
