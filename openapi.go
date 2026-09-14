package graft

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/spidermeaow/graft-framework/openapi"
)

// OpenAPI generates an OpenAPI 3.0.3 JSON document from registered route metadata.
// It returns an error for ServeMux patterns that cannot be represented faithfully.
func (a *App) OpenAPI(title, version string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	paths := map[string]map[string]routeDoc{}
	components := map[string]map[string]any{}
	operationIDs := map[string]bool{}
	// Register components first so routes may reference definitions contributed
	// by any route, regardless of registration order.
	for _, original := range a.routes {
		if original.metadataErr != nil {
			return nil, fmt.Errorf("OpenAPI %s %s: %w", original.method, original.path, original.metadataErr)
		}
		for name, scheme := range original.components.SecuritySchemes {
			if err := addComponent(components, "securitySchemes", name, scheme); err != nil {
				return nil, err
			}
		}
		for name, schema := range original.components.Schemas {
			if err := addComponent(components, "schemas", name, schema); err != nil {
				return nil, err
			}
		}
		for name, response := range original.components.ComponentResponses {
			if err := addComponent(components, "responses", name, responseDocument(response)); err != nil {
				return nil, err
			}
		}
		for name, parameter := range original.components.ComponentParameters {
			if err := addComponent(components, "parameters", name, parameter); err != nil {
				return nil, err
			}
		}
	}
	for _, original := range a.routes {
		r := original
		if r.OperationID != "" {
			if operationIDs[r.OperationID] {
				return nil, fmt.Errorf("duplicate OpenAPI operationId %q", r.OperationID)
			}
			operationIDs[r.OperationID] = true
		}
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
		seen := map[string]parameterDoc{}
		unique := make([]parameterDoc, 0, len(r.Parameters))
		for _, p := range r.Parameters {
			if p.Ref != "" {
				name := strings.TrimPrefix(p.Ref, "#/components/parameters/")
				if name == p.Ref {
					return nil, fmt.Errorf("invalid parameter reference %q", p.Ref)
				}
				definition, exists := components["parameters"][name]
				if !exists {
					return nil, fmt.Errorf("unknown parameter component %q", name)
				}
				registered := definition.(OpenAPIParameter)
				p.Name, p.In, p.Required = registered.Name, registered.In, registered.Required
			}
			key := p.In + ":" + p.Name
			if previous, exists := seen[key]; exists {
				if !reflect.DeepEqual(previous, p) {
					return nil, fmt.Errorf("conflicting parameter %s on %s", p.Name, path)
				}
				continue
			}
			seen[key] = p
			unique = append(unique, p)
			if p.Name == "" {
				return nil, fmt.Errorf("empty parameter on %s", path)
			}
			if p.In == "path" && !params[p.Name] {
				return nil, fmt.Errorf("path parameter %s does not exist on %s", p.Name, path)
			}
			if p.In == "path" && !p.Required {
				return nil, fmt.Errorf("path parameter %s must be required", p.Name)
			}
		}
		for _, segment := range strings.Split(path, "/") {
			if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
				continue
			}
			name := segment[1 : len(segment)-1]
			if _, exists := seen["path:"+name]; !exists {
				unique = append(unique, parameterDoc{Name: name, In: "path", Required: true, Schema: openapi.Schema{Type: "string"}})
			}
		}
		r.Parameters = unique
		for _, response := range r.Responses {
			if response.Ref == "" {
				continue
			}
			name := strings.TrimPrefix(response.Ref, "#/components/responses/")
			if name == response.Ref {
				return nil, fmt.Errorf("invalid response reference %q", response.Ref)
			}
			if _, exists := components["responses"][name]; !exists {
				return nil, fmt.Errorf("unknown response component %q", name)
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
	for path, methods := range paths {
		for _, route := range methods {
			for _, requirement := range route.Security {
				for name := range requirement {
					if _, exists := components["securitySchemes"][name]; !exists {
						return nil, fmt.Errorf("security scheme %q is not registered for %s", name, path)
					}
				}
			}
		}
	}
	document := struct {
		OpenAPI    string                         `json:"openapi"`
		Info       map[string]string              `json:"info"`
		Paths      map[string]map[string]routeDoc `json:"paths"`
		Components map[string]any                 `json:"components,omitempty"`
	}{OpenAPI: "3.0.3", Info: map[string]string{"title": title, "version": version}, Paths: paths}
	if len(components) > 0 {
		document.Components = map[string]any{}
		for category, entries := range components {
			document.Components[category] = entries
		}
	}
	return json.MarshalIndent(document, "", "  ")
}

func addComponent(all map[string]map[string]any, category, name string, value any) error {
	if name == "" {
		return fmt.Errorf("empty OpenAPI %s component name", category)
	}
	if all[category] == nil {
		all[category] = map[string]any{}
	}
	if previous, exists := all[category][name]; exists {
		if !reflect.DeepEqual(previous, value) {
			return fmt.Errorf("conflicting OpenAPI %s component %q", category, name)
		}
		return nil
	}
	all[category][name] = value
	return nil
}

func responseDocument(response OpenAPIResponse) responseDoc {
	item := responseDoc{Description: response.Description}
	if len(response.Content) > 0 {
		item.Content = map[string]mediaDoc{}
		for media, schema := range response.Content {
			item.Content[media] = mediaDoc{Schema: schema}
		}
	}
	return item
}
