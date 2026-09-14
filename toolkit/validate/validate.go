// Package validate checks request DTOs using small, explicit struct tags.
package validate

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/spidermeaow/graft-framework"
)

// FieldError describes a public validation failure without echoing input values.
type FieldError struct {
	Field, Rule string `json:"-"`
}

func (e FieldError) Error() string { return e.Field + " failed " + e.Rule }

// Errors is a collection of validation failures.
type Errors []FieldError

func (e Errors) Error() string { return fmt.Sprintf("%d validation error(s)", len(e)) }

// Check supports required, min=N and max=N tags. Length applies to strings,
// slices and maps; min/max apply numerically to integer fields.
func Check(value any) error {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return errors.New("validate: nil value")
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return errors.New("validate: expected struct")
	}
	var failures Errors
	check(v, "", &failures)
	if len(failures) > 0 {
		return failures
	}
	return nil
}

func check(v reflect.Value, prefix string, failures *Errors) {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if prefix != "" {
			name = prefix + "." + name
		}
		value := v.Field(i)
		for _, rule := range strings.Split(field.Tag.Get("validate"), ",") {
			if rule == "" {
				continue
			}
			if rule == "required" {
				if value.IsZero() {
					*failures = append(*failures, FieldError{name, rule})
				}
				continue
			}
			operator, raw, ok := strings.Cut(rule, "=")
			if !ok || (operator != "min" && operator != "max") {
				*failures = append(*failures, FieldError{name, "unsupported rule"})
				continue
			}
			limit, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || limit < 0 {
				*failures = append(*failures, FieldError{name, "unsupported rule"})
				continue
			}
			var size int64
			switch value.Kind() {
			case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
				size = int64(value.Len())
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				size = value.Int()
			default:
				*failures = append(*failures, FieldError{name, "unsupported rule"})
				continue
			}
			if operator == "min" && size < limit || operator == "max" && size > limit {
				*failures = append(*failures, FieldError{name, rule})
			}
		}
	}
}

// Bind checks one JSON body and returns 400 for validation failures.
func Bind(c *graft.Context, dst any) error {
	if err := c.Bind(dst); err != nil {
		return err
	}
	if err := Check(dst); err != nil {
		return graft.NewHTTPError(400, err.Error())
	}
	return nil
}
