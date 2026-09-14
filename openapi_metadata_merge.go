package graft

import (
	"fmt"
	"github.com/spidermeaow/graft-framework/openapi"
	"reflect"
	"strconv"
)

func (r *routeDoc) apply(m OpenAPIMetadata) {
	if m.Summary != "" {
		r.Summary = m.Summary
	}
	if m.Description != "" {
		r.Description = m.Description
	}
	if m.OperationID != "" {
		r.OperationID = m.OperationID
	}
	if m.Deprecated {
		r.Deprecated = true
	}
	for _, tag := range m.Tags {
		if !hasString(r.Tags, tag) {
			r.Tags = append(r.Tags, tag)
		}
	}
	for _, p := range m.Parameters {
		newParam := parameterDoc{Name: p.Name, In: p.In, Required: p.Required, Schema: p.Schema}
		found := false
		for i := range r.Parameters {
			if r.Parameters[i].Name == p.Name && r.Parameters[i].In == p.In {
				if !reflect.DeepEqual(r.Parameters[i], newParam) {
					r.metadataErr = fmt.Errorf("conflicting parameter %s in %s", p.Name, p.In)
				}
				found = true
				break
			}
		}
		if !found {
			r.Parameters = append(r.Parameters, newParam)
		}
	}
	if m.RequestBody != nil {
		if r.RequestBody == nil {
			r.RequestBody = &bodyDoc{Content: map[string]mediaDoc{}}
		}
		r.RequestBody.Required = r.RequestBody.Required || m.RequestBody.Required
		for media, schema := range m.RequestBody.Content {
			if previous, ok := r.RequestBody.Content[media]; ok && !reflect.DeepEqual(previous.Schema, schema) {
				r.metadataErr = fmt.Errorf("conflicting request body for %s", media)
			}
			r.RequestBody.Content[media] = mediaDoc{Schema: schema}
		}
	}
	if r.Responses == nil {
		r.Responses = map[string]responseDoc{}
	}
	for status, response := range m.Responses {
		if status < 100 || status > 599 {
			r.metadataErr = fmt.Errorf("invalid response status %d", status)
			continue
		}
		item := responseDoc{Description: response.Description}
		if len(response.Content) > 0 {
			item.Content = map[string]mediaDoc{}
			for media, schema := range response.Content {
				item.Content[media] = mediaDoc{Schema: schema}
			}
		}
		key := strconv.Itoa(status)
		if previous, ok := r.Responses[key]; ok {
			if previous.Description == item.Description && reflect.DeepEqual(previous.Content, item.Content) {
				continue
			}
			// Explicit route options may override middleware response descriptions.
			continue
		}
		r.Responses[key] = item
	}
	r.Security = mergeSecurity(r.Security, m.Security)
	if r.components.SecuritySchemes == nil {
		r.components.SecuritySchemes = map[string]map[string]any{}
	}
	for name, scheme := range m.SecuritySchemes {
		if previous, exists := r.components.SecuritySchemes[name]; exists && !reflect.DeepEqual(previous, scheme) {
			r.metadataErr = fmt.Errorf("conflicting security scheme %q", name)
		}
		r.components.SecuritySchemes[name] = scheme
	}
	if r.components.Schemas == nil {
		r.components.Schemas = map[string]openapi.Schema{}
	}
	for name, schema := range m.Schemas {
		if previous, exists := r.components.Schemas[name]; exists && !reflect.DeepEqual(previous, schema) {
			r.metadataErr = fmt.Errorf("conflicting schema %q", name)
		}
		r.components.Schemas[name] = schema
	}
	if r.components.ComponentResponses == nil {
		r.components.ComponentResponses = map[string]OpenAPIResponse{}
	}
	for name, response := range m.ComponentResponses {
		if previous, exists := r.components.ComponentResponses[name]; exists && !reflect.DeepEqual(previous, response) {
			r.metadataErr = fmt.Errorf("conflicting response component %q", name)
		}
		r.components.ComponentResponses[name] = response
	}
	if r.components.ComponentParameters == nil {
		r.components.ComponentParameters = map[string]OpenAPIParameter{}
	}
	for name, parameter := range m.ComponentParameters {
		if previous, exists := r.components.ComponentParameters[name]; exists && !reflect.DeepEqual(previous, parameter) {
			r.metadataErr = fmt.Errorf("conflicting parameter component %q", name)
		}
		r.components.ComponentParameters[name] = parameter
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
