package config

import "fmt"

// validationError describes configuration validation failures.
type validationError struct {
	field   string
	details string
}

// Error returns a human-readable validation error message.
func (e validationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.field, e.details)
}

// newValidationError creates structured validation errors.
func newValidationError(field string, details string) error {
	return validationError{field: field, details: details}
}
