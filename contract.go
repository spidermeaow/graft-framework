package graft

import (
	"net/http"
)

// Body documents a required JSON request body inferred from T.
func Body[T any]() RouteOption { return RequestBody(SchemaOf[T]()) }

// Response documents a JSON response inferred from T.
func Response[T any](status int) RouteOption {
	return ResponseDescription[T](status, defaultResponseDescription(status))
}

// ResponseDescription documents a JSON response inferred from T with a custom description.
func ResponseDescription[T any](status int, description string) RouteOption {
	return ResponseSchema(status, description, SchemaOf[T]())
}

// Path documents a required path parameter inferred from T.
func Path[T any](name string) RouteOption { return PathParameter(name, SchemaOf[T]()) }

// Query documents a required query parameter inferred from T.
func Query[T any](name string) RouteOption { return QueryParameter(name, true, SchemaOf[T]()) }

// QueryOptional documents an optional query parameter inferred from T.
func QueryOptional[T any](name string) RouteOption {
	return QueryParameter(name, false, SchemaOf[T]())
}

func defaultResponseDescription(status int) string {
	if description := http.StatusText(status); description != "" {
		return description
	}
	return "Response"
}
