package provider

import (
	"math"
	"regexp"
)

const (
	// googleGemini3ImageTokens is the default media resolution token count per
	// image for Gemini 3 and Gemini 4.
	googleGemini3ImageTokens uint64 = 1120
	// googleTileTokens is the token count per image tile for earlier Gemini models.
	googleTileTokens     uint64 = 258
	googleSmallImageEdge        = 384
	googleMinCropUnit           = 256
	googleMaxCropUnit           = 768
)

var (
	googleDefaultResolutionModel = regexp.MustCompile(`^gemini-[34](?:[.-]|$)`)
	googleTiledModel             = regexp.MustCompile(`^gemini-[12](?:[.-]|$)`)
)

// resolveGoogleImageRule returns the fixed default media resolution rule for
// Gemini 3 and Gemini 4, and the 258-token tile rule for Gemini 1.x and 2.x.
func resolveGoogleImageRule(model string) (func(CanonicalImageInfo) (uint64, error), error) {
	switch {
	case googleDefaultResolutionModel.MatchString(model):
		return func(CanonicalImageInfo) (uint64, error) { return googleGemini3ImageTokens, nil }, nil
	case googleTiledModel.MatchString(model):
		return func(image CanonicalImageInfo) (uint64, error) {
			return googleTileTokens * googleTileCount(image.Width, image.Height), nil
		}, nil
	default:
		return nil, unsupportedImageModelError(model)
	}
}

// googleTileCount returns 1 when both edges are at most 384px. Larger images are
// cropped into square units of floor(min edge / 1.5), clamped to 256..768px.
func googleTileCount(width, height int) uint64 {
	if width <= googleSmallImageEdge && height <= googleSmallImageEdge {
		return 1
	}
	unit := math.Floor(math.Min(float64(width), float64(height)) / 1.5)
	unit = math.Max(googleMinCropUnit, math.Min(googleMaxCropUnit, unit))
	return ceilDiv(float64(width), unit) * ceilDiv(float64(height), unit)
}
