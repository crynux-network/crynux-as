package llmadapter

import "fmt"

// ValidationError indicates an OpenAI-compatible invalid request error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func newValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
