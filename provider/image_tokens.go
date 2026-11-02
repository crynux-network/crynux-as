package provider

import (
	"crynux_as/models"
	"fmt"
	"math"
	"strings"
)

// EstimateImageInputTokens returns the upper bound of input tokens consumed by
// images for one Service Provider model. Requests without images always return
// zero. A model whose image token rule is unknown returns an error.
func EstimateImageInputTokens(serviceProvider models.ServiceProvider, model string, images []CanonicalImageInfo) (uint64, error) {
	if len(images) == 0 {
		return 0, nil
	}
	name := strings.ToLower(strings.TrimSpace(model))
	var estimate func(CanonicalImageInfo) (uint64, error)
	switch serviceProvider {
	case models.ServiceProviderOpenAI:
		rule, err := resolveOpenAIImageRule(name)
		if err != nil {
			return 0, err
		}
		estimate = rule.estimate
	case models.ServiceProviderAnthropic:
		tier, err := resolveAnthropicImageTier(name)
		if err != nil {
			return 0, err
		}
		estimate = tier.estimate
	case models.ServiceProviderGoogle:
		rule, err := resolveGoogleImageRule(name)
		if err != nil {
			return 0, err
		}
		estimate = rule
	default:
		return 0, fmt.Errorf("image input is not supported for service provider %q", serviceProvider)
	}
	var total uint64
	for _, image := range images {
		tokens, err := estimate(image)
		if err != nil {
			return 0, err
		}
		total += tokens
	}
	return total, nil
}

func unsupportedImageModelError(model string) error {
	return fmt.Errorf("image input is not supported for model %q", model)
}

// scaleToFit downscales width and height so both fit within maxWidth x maxHeight,
// preserving aspect ratio. Images already inside the box are unchanged.
func scaleToFit(width, height int, maxWidth, maxHeight float64) (float64, float64) {
	w, h := float64(width), float64(height)
	if w <= maxWidth && h <= maxHeight {
		return w, h
	}
	if w*maxHeight >= h*maxWidth {
		return maxWidth, h * maxWidth / w
	}
	return w * maxHeight / h, maxHeight
}

func ceilDiv(value, unit float64) uint64 {
	return uint64(math.Ceil(value / unit))
}

// hasModelPrefix reports whether model equals prefix or continues it with a
// version or variant suffix separated by "-".
func hasModelPrefix(model, prefix string) bool {
	return model == prefix || strings.HasPrefix(model, prefix+"-")
}
