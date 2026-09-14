package graft

import "reflect"

func mergeSecurity(existing, incoming []map[string][]string) []map[string][]string {
	// Security alternatives within one provider are OR; requirements contributed
	// by different middleware are AND, represented by a Cartesian product.
	if len(incoming) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return uniqueSecurity(incoming)
	}
	var merged []map[string][]string
	for _, left := range existing {
		for _, right := range incoming {
			combined := map[string][]string{}
			for name, scopes := range left {
				combined[name] = append([]string{}, scopes...)
			}
			for name, scopes := range right {
				for _, scope := range scopes {
					if !hasString(combined[name], scope) {
						combined[name] = append(combined[name], scope)
					}
				}
				if _, exists := combined[name]; !exists {
					combined[name] = []string{}
				}
			}
			merged = append(merged, combined)
		}
	}
	return uniqueSecurity(merged)
}

func uniqueSecurity(requirements []map[string][]string) []map[string][]string {
	unique := make([]map[string][]string, 0, len(requirements))
	for _, requirement := range requirements {
		normalized := map[string][]string{}
		for name, scopes := range requirement {
			normalized[name] = append([]string{}, scopes...)
		}
		seen := false
		for _, previous := range unique {
			if reflect.DeepEqual(previous, normalized) {
				seen = true
				break
			}
		}
		if !seen {
			unique = append(unique, normalized)
		}
	}
	return unique
}

func legacySecurityScheme(name string) map[string]any {
	switch name {
	case "BearerAuth":
		return map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}
	case "ApiKeyAuth":
		return map[string]any{"type": "apiKey", "in": "header", "name": "X-API-Key"}
	default:
		return nil
	}
}
