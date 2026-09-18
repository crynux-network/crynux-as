package llmadapter

import "strings"

// NormalizeModelID returns the canonical lowercase model ID used after request parsing.
func NormalizeModelID(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}
