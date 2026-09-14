package api

import (
	"net/http/httptest"
	"testing"
)

func TestParsePage(t *testing.T) {
	r := httptest.NewRequest("GET", "/?limit=20&offset=5", nil)
	p, err := ParsePage(r, 10, 100)
	if err != nil || p.Limit != 20 || p.Offset != 5 {
		t.Fatal(p, err)
	}
	for _, url := range []string{"/?limit=101", "/?limit=-1", "/?offset=-1", "/?offset=bad"} {
		if _, err := ParsePage(httptest.NewRequest("GET", url, nil), 10, 100); err == nil {
			t.Fatal(url)
		}
	}
}
