package graft

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/spidermeaow/graft-framework/internal/validationrules"
	"github.com/spidermeaow/graft-framework/openapi"
)

var timeType = reflect.TypeFor[time.Time]()

// SchemaOf builds an OpenAPI schema from T and its json and validate tags.
// It panics during route registration when T cannot be represented. Use the
// manual schema APIs for types that need a custom representation.
func SchemaOf[T any]() openapi.Schema {
	schema, err := schemaFromType(reflect.TypeFor[T](), map[reflect.Type]bool{})
	if err != nil {
		panic("graft: infer OpenAPI schema: " + err.Error())
	}
	return schema
}

func schemaFromType(t reflect.Type, visiting map[reflect.Type]bool) (openapi.Schema, error) {
	if t == nil {
		return openapi.Schema{}, fmt.Errorf("type is nil")
	}
	if t.Kind() == reflect.Pointer {
		schema, err := schemaFromType(t.Elem(), visiting)
		schema.Nullable = true
		return schema, err
	}
	if t == timeType {
		return openapi.Schema{Type: "string", Format: "date-time"}, nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return openapi.Schema{Type: "boolean"}, nil
	case reflect.String:
		return openapi.Schema{Type: "string"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16:
		return openapi.Schema{Type: "integer"}, nil
	case reflect.Int32:
		return openapi.Schema{Type: "integer", Format: "int32"}, nil
	case reflect.Int64:
		return openapi.Schema{Type: "integer", Format: "int64"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16:
		minimum := float64(0)
		return openapi.Schema{Type: "integer", Minimum: &minimum}, nil
	case reflect.Uint32:
		minimum := float64(0)
		return openapi.Schema{Type: "integer", Format: "int32", Minimum: &minimum}, nil
	case reflect.Uint64, reflect.Uintptr:
		minimum := float64(0)
		return openapi.Schema{Type: "integer", Format: "int64", Minimum: &minimum}, nil
	case reflect.Float32:
		return openapi.Schema{Type: "number", Format: "float"}, nil
	case reflect.Float64:
		return openapi.Schema{Type: "number", Format: "double"}, nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return openapi.Schema{Type: "string", Format: "byte"}, nil
		}
		item, err := schemaFromType(t.Elem(), visiting)
		return openapi.Schema{Type: "array", Items: &item}, err
	case reflect.Array:
		item, err := schemaFromType(t.Elem(), visiting)
		length := t.Len()
		return openapi.Schema{Type: "array", Items: &item, MinItems: &length, MaxItems: &length}, err
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return openapi.Schema{}, fmt.Errorf("map %s must have string keys", t)
		}
		additional := true
		return openapi.Schema{Type: "object", AdditionalProperties: &additional}, nil
	case reflect.Struct:
		if visiting[t] {
			return openapi.Schema{}, fmt.Errorf("recursive type %s requires a manual schema", t)
		}
		visiting[t] = true
		defer delete(visiting, t)
		return structSchema(t, visiting)
	default:
		return openapi.Schema{}, fmt.Errorf("unsupported type %s", t)
	}
}

func structSchema(t reflect.Type, visiting map[reflect.Type]bool) (openapi.Schema, error) {
	schema := openapi.Schema{Type: "object", Properties: map[string]openapi.Schema{}}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		jsonParts := strings.Split(field.Tag.Get("json"), ",")
		name := jsonParts[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		property, err := schemaFromType(field.Type, visiting)
		if err != nil {
			return openapi.Schema{}, fmt.Errorf("field %s: %w", field.Name, err)
		}
		property.Description = field.Tag.Get("description")
		if property.Description == "" {
			property.Description = field.Tag.Get("doc")
		}
		rules, err := validationrules.Parse(field.Tag.Get("validate"))
		if err != nil {
			return openapi.Schema{}, fmt.Errorf("field %s: %w", field.Name, err)
		}
		for _, rule := range rules {
			switch rule.Name {
			case "required":
				if !containsString(schema.Required, name) {
					schema.Required = append(schema.Required, name)
				}
			case "email":
				if indirectKind(field.Type) != reflect.String {
					return openapi.Schema{}, fmt.Errorf("field %s: email requires a string", field.Name)
				}
				property.Format = "email"
			case "min", "max":
				if err := applyBound(&property, field.Type, rule); err != nil {
					return openapi.Schema{}, fmt.Errorf("field %s: %w", field.Name, err)
				}
			}
		}
		schema.Properties[name] = property
	}
	return schema, nil
}

func applyBound(schema *openapi.Schema, t reflect.Type, rule validationrules.Rule) error {
	value := int(rule.Value)
	switch indirectKind(t) {
	case reflect.String:
		if rule.Name == "min" {
			schema.MinLength = &value
		} else {
			schema.MaxLength = &value
		}
	case reflect.Slice, reflect.Array:
		if rule.Name == "min" {
			schema.MinItems = &value
		} else {
			schema.MaxItems = &value
		}
	case reflect.Map:
		if rule.Name == "min" {
			schema.MinProperties = &value
		} else {
			schema.MaxProperties = &value
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		number := float64(rule.Value)
		if rule.Name == "min" {
			schema.Minimum = &number
		} else {
			schema.Maximum = &number
		}
	default:
		return fmt.Errorf("%s is not supported for %s", rule.Raw, t)
	}
	return nil
}

func indirectKind(t reflect.Type) reflect.Kind {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind()
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
