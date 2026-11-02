package provider

// MapAnthropicEffort converts a validated OpenAI-compatible effort value into
// Anthropic thinking and output_config.effort fields. omitted is true when the
// client did not provide effort or requested "none", so neither Anthropic field
// should be injected.
// Other values use thinkingType "adaptive" and a mapped effortOut.
func MapAnthropicEffort(effort string) (thinkingType string, effortOut string, omitted bool) {
	if effort == "" || effort == "none" {
		return "", "", true
	}
	switch effort {
	case "minimal", "low":
		return "adaptive", "low", false
	default:
		return "adaptive", effort, false
	}
}

// MapGoogleThinkingLevel converts a validated OpenAI-compatible effort value
// into a Google thinkingLevel. omitted is true when the client did not provide
// effort or requested "none", so thinkingConfig must not be injected.
func MapGoogleThinkingLevel(effort string) (level string, omitted bool) {
	switch effort {
	case "":
		return "", true
	case "none":
		return "", true
	case "minimal", "low":
		return "low", false
	case "medium":
		return "medium", false
	case "high", "xhigh", "max":
		return "high", false
	default:
		return "", true
	}
}
