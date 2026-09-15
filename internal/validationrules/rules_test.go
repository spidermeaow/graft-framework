package validationrules

import "testing"

func TestParse(t *testing.T) {
	rules, err := Parse("required,email,min=3,max=80")
	if err != nil || len(rules) != 4 || rules[2].Name != "min" || rules[2].Value != 3 {
		t.Fatalf("unexpected rules: %#v, %v", rules, err)
	}
	for _, tag := range []string{"unknown", "min", "min=-1", "max=word"} {
		if _, err := Parse(tag); err == nil {
			t.Fatalf("expected %q to fail", tag)
		}
	}
}
