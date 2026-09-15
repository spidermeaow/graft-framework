// Package validate checks request DTOs using small, explicit struct tags.
package validate

import (
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/spidermeaow/graft-framework"
	"github.com/spidermeaow/graft-framework/internal/validationrules"
)

// FieldError describes a public validation failure without echoing input values.
type FieldError struct {
	Field, Rule string `json:"-"`
}

func (e FieldError) Error() string { return e.Field + " failed " + e.Rule }

// Errors is a collection of validation failures.
type Errors []FieldError

func (e Errors) Error() string { return fmt.Sprintf("%d validation error(s)", len(e)) }

// Check supports required, email, min=N and max=N tags. Length applies to
// strings, slices and maps; min/max apply numerically to numeric fields.
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
		rules, err := validationrules.Parse(field.Tag.Get("validate"))
		if err != nil {
			*failures = append(*failures, FieldError{name, "unsupported rule"})
			continue
		}
		for _, rule := range rules {
			if rule.Name == "required" {
				if value.IsZero() {
					*failures = append(*failures, FieldError{name, rule.Raw})
				}
				continue
			}
			if rule.Name == "email" {
				actual, ok := indirectValue(value)
				if !ok {
					continue
				}
				if actual.Kind() != reflect.String {
					*failures = append(*failures, FieldError{name, "unsupported rule"})
					continue
				}
				raw := actual.String()
				address, err := mail.ParseAddress(raw)
				if err != nil || address.Address != raw || !strings.Contains(raw, "@") {
					*failures = append(*failures, FieldError{name, rule.Raw})
				}
				continue
			}
			actual, ok := indirectValue(value)
			if !ok {
				continue
			}
			failed := false
			switch actual.Kind() {
			case reflect.String:
				size := int64(utf8.RuneCountInString(actual.String()))
				failed = rule.Name == "min" && size < rule.Value || rule.Name == "max" && size > rule.Value
			case reflect.Slice, reflect.Map, reflect.Array:
				size := int64(actual.Len())
				failed = rule.Name == "min" && size < rule.Value || rule.Name == "max" && size > rule.Value
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				number := actual.Int()
				failed = rule.Name == "min" && number < rule.Value || rule.Name == "max" && number > rule.Value
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				number := actual.Uint()
				failed = rule.Name == "min" && number < uint64(rule.Value) || rule.Name == "max" && number > uint64(rule.Value)
			case reflect.Float32, reflect.Float64:
				number := actual.Float()
				failed = rule.Name == "min" && number < float64(rule.Value) || rule.Name == "max" && number > float64(rule.Value)
			default:
				*failures = append(*failures, FieldError{name, "unsupported rule"})
				continue
			}
			if failed {
				*failures = append(*failures, FieldError{name, rule.Raw})
			}
		}
	}
}

func indirectValue(value reflect.Value) (reflect.Value, bool) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, true
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
