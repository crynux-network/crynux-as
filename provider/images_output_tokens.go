package provider

import (
	"strings"
)

// EstimateOpenAIImageOutputTokens returns an admission upper bound for one
// GPT Image output image from quality and pixel size.
func EstimateOpenAIImageOutputTokens(model, quality string, width, height int) uint64 {
	_ = model
	if width <= 0 {
		width = imagesDefaultWidth
	}
	if height <= 0 {
		height = imagesDefaultHeight
	}
	q := strings.TrimSpace(strings.ToLower(quality))
	if q == "" || q == "auto" || q == "standard" {
		q = "medium"
	}
	if q == "hd" {
		q = "high"
	}
	// Square 1024 baseline table from OpenAI GPT Image pricing guidance.
	base := map[string]uint64{
		"low":    272,
		"medium": 1056,
		"high":   4160,
		"xhigh":  6240,
		"max":    7024,
	}
	tokens, ok := base[q]
	if !ok {
		tokens = base["medium"]
	}
	area := uint64(width) * uint64(height)
	ref := uint64(1024 * 1024)
	if area <= ref {
		return tokens
	}
	// Scale up roughly with area for larger canvases; admission is an upper bound.
	scaled := tokens * area / ref
	if scaled < tokens {
		return tokens
	}
	return scaled
}

// EstimateProviderImageOutputTokens estimates total image output tokens for one
// Images request (already multiplied by n).
func EstimateProviderImageOutputTokens(serviceProvider string, model string, quality string, width, height, n int) uint64 {
	if n <= 0 {
		n = 1
	}
	switch serviceProvider {
	case "google":
		_, imageSize := mapSizeToGoogleImageConfig(width, height)
		return EstimateGoogleImageOutputTokens(imageSize) * uint64(n)
	default:
		return EstimateOpenAIImageOutputTokens(model, quality, width, height) * uint64(n)
	}
}
