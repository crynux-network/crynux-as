package llmadapter

import (
	"regexp"
	"strings"
	"unicode"
)

var huggingFaceModelIDPattern = regexp.MustCompile(
	`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?(/[a-z0-9]([a-z0-9._-]*[a-z0-9])?)*$`,
)

// NormalizeModelName lowercases a Hugging Face model repository name so that
// names differing only by letter case identify the same model. URL-based names
// are kept unchanged because URL paths are case sensitive.
func NormalizeModelName(name string) string {
	if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") {
		return name
	}
	return strings.ToLower(name)
}

// NormalizeModelID returns the canonical model ID used after request parsing:
// surrounding whitespace trimmed, then NormalizeModelName.
func NormalizeModelID(model string) string {
	return NormalizeModelName(strings.TrimSpace(model))
}

// ValidateHuggingFaceModelID checks that model is a Hugging Face repo id after normalization.
// Absolute URLs and other non-repo forms are rejected.
func ValidateHuggingFaceModelID(model string) error {
	normalized := NormalizeModelID(model)
	if normalized == "" {
		return newValidationError("model", "model is required")
	}
	if strings.Contains(normalized, "://") ||
		strings.HasPrefix(normalized, "http://") ||
		strings.HasPrefix(normalized, "https://") {
		return newValidationError("model", "must be a Hugging Face model id, not a URL")
	}
	for _, r := range normalized {
		if unicode.IsSpace(r) {
			return newValidationError("model", "must be a Hugging Face model id")
		}
	}
	if !huggingFaceModelIDPattern.MatchString(normalized) {
		return newValidationError("model", "must be a Hugging Face model id")
	}
	return nil
}
