package llmadapter

import "strings"

// AllowedReasoningEffortValues are the OpenAI-compatible public effort values.
var AllowedReasoningEffortValues = []string{
	"none",
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
	"max",
}

var allowedReasoningEffort = map[string]struct{}{
	"none":    {},
	"minimal": {},
	"low":     {},
	"medium":  {},
	"high":    {},
	"xhigh":   {},
	"max":     {},
}

// NormalizeReasoningEffort trims the public effort value. An empty result means
// the client omitted the field.
func NormalizeReasoningEffort(value string) string {
	return strings.TrimSpace(value)
}

// ValidateReasoningEffort accepts an empty value or one of the OpenAI-compatible
// effort enums. field names the public JSON path for error messages.
func ValidateReasoningEffort(field, value string) (string, error) {
	normalized := NormalizeReasoningEffort(value)
	if normalized == "" {
		return "", nil
	}
	if _, ok := allowedReasoningEffort[normalized]; !ok {
		return "", newValidationError(field, "must be one of none, minimal, low, medium, high, xhigh, max")
	}
	return normalized, nil
}
