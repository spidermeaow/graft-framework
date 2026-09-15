// Package validationrules parses the validation tag subset shared by runtime
// validation and OpenAPI schema generation.
package validationrules

import (
	"fmt"
	"strconv"
	"strings"
)

// Rule is one normalized validate tag rule.
type Rule struct {
	Name  string
	Value int64
	Raw   string
}

// Parse supports required, email, min=N and max=N.
func Parse(tag string) ([]Rule, error) {
	if strings.TrimSpace(tag) == "" {
		return nil, nil
	}
	parts := strings.Split(tag, ",")
	rules := make([]Rule, 0, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "required" || raw == "email" {
			rules = append(rules, Rule{Name: raw, Raw: raw})
			continue
		}
		name, value, ok := strings.Cut(raw, "=")
		if !ok || (name != "min" && name != "max") {
			return nil, fmt.Errorf("unsupported validation rule %q", raw)
		}
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil || number < 0 {
			return nil, fmt.Errorf("invalid validation rule %q", raw)
		}
		rules = append(rules, Rule{Name: name, Value: number, Raw: raw})
	}
	return rules, nil
}
