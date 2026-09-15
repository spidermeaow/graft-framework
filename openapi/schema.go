// Package openapi defines a deliberately small, explicit OpenAPI 3.0 schema model.
// Schemas describe the API; they do not perform runtime validation or reflection.
package openapi

// Schema describes JSON values using the common subset of OpenAPI 3.0 schemas.
type Schema struct {
	Ref                  string            `json:"$ref,omitempty"`
	Type                 string            `json:"type,omitempty"`
	Format               string            `json:"format,omitempty"`
	Description          string            `json:"description,omitempty"`
	Minimum              *float64          `json:"minimum,omitempty"`
	Maximum              *float64          `json:"maximum,omitempty"`
	MinLength            *int              `json:"minLength,omitempty"`
	MaxLength            *int              `json:"maxLength,omitempty"`
	MinItems             *int              `json:"minItems,omitempty"`
	MaxItems             *int              `json:"maxItems,omitempty"`
	MinProperties        *int              `json:"minProperties,omitempty"`
	MaxProperties        *int              `json:"maxProperties,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	Enum                 []string          `json:"enum,omitempty"`
	Nullable             bool              `json:"nullable,omitempty"`
	AdditionalProperties *bool             `json:"additionalProperties,omitempty"`
}
