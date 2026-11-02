package provider

import (
	"math"
	"regexp"
	"strings"
)

// openAIImageRule is one OpenAI image token rule. Patch-based models use
// patchMultiplier; tile-based models use baseTokens and tileTokens.
type openAIImageRule struct {
	patchMultiplier float64
	baseTokens      uint64
	tileTokens      uint64
}

const (
	openAIPatchSize     = 32
	openAIMaxPatches    = 1536
	openAITileSize      = 512
	openAITileMaxEdge   = 2048
	openAITileShortEdge = 768
)

var openAIGPT5Family = regexp.MustCompile(`^gpt-5(?:\.\d+)?(-.*)?$`)

var openAIPatchModels = []struct {
	prefix     string
	multiplier float64
}{
	{"gpt-4.1-mini", 1.62},
	{"gpt-4.1-nano", 2.46},
	{"o4-mini", 1.72},
}

var openAINonVisionModels = []string{"o1-mini", "o3-mini"}

var openAITileModels = []struct {
	prefix string
	base   uint64
	tile   uint64
}{
	{"gpt-4o-mini", 2833, 5667},
	{"gpt-4o", 85, 170},
	{"chatgpt-4o", 85, 170},
	{"gpt-4.1", 85, 170},
	{"gpt-4.5", 85, 170},
	{"gpt-4-turbo", 85, 170},
	{"o1", 75, 150},
	{"o3", 75, 150},
}

func resolveOpenAIImageRule(model string) (openAIImageRule, error) {
	for _, nonVision := range openAINonVisionModels {
		if hasModelPrefix(model, nonVision) {
			return openAIImageRule{}, unsupportedImageModelError(model)
		}
	}
	for _, m := range openAIPatchModels {
		if hasModelPrefix(model, m.prefix) {
			return openAIImageRule{patchMultiplier: m.multiplier}, nil
		}
	}
	if match := openAIGPT5Family.FindStringSubmatch(model); match != nil {
		suffix := match[1]
		switch {
		case strings.HasPrefix(suffix, "-mini"):
			return openAIImageRule{patchMultiplier: 1.62}, nil
		case strings.HasPrefix(suffix, "-nano"):
			return openAIImageRule{patchMultiplier: 2.46}, nil
		default:
			return openAIImageRule{baseTokens: 70, tileTokens: 140}, nil
		}
	}
	for _, m := range openAITileModels {
		if hasModelPrefix(model, m.prefix) {
			return openAIImageRule{baseTokens: m.base, tileTokens: m.tile}, nil
		}
	}
	return openAIImageRule{}, unsupportedImageModelError(model)
}

func (r openAIImageRule) estimate(image CanonicalImageInfo) (uint64, error) {
	if r.patchMultiplier > 0 {
		patches := openAIPatchCount(image.Width, image.Height)
		return uint64(math.Ceil(float64(patches) * r.patchMultiplier)), nil
	}
	if image.Detail == "low" {
		return r.baseTokens, nil
	}
	return r.baseTokens + r.tileTokens*openAITileCount(image.Width, image.Height), nil
}

// openAIPatchCount covers the image with 32px patches. Images above the patch
// budget are downscaled so that the patch count stays within the budget.
func openAIPatchCount(width, height int) uint64 {
	w, h := float64(width), float64(height)
	patches := ceilDiv(w, openAIPatchSize) * ceilDiv(h, openAIPatchSize)
	if patches <= openAIMaxPatches {
		return patches
	}
	r := math.Sqrt(openAIPatchSize * openAIPatchSize * openAIMaxPatches / (w * h))
	scaledW, scaledH := w*r/openAIPatchSize, h*r/openAIPatchSize
	r *= math.Min(math.Floor(scaledW)/scaledW, math.Floor(scaledH)/scaledH)
	patches = ceilDiv(w*r, openAIPatchSize) * ceilDiv(h*r, openAIPatchSize)
	if patches > openAIMaxPatches {
		patches = openAIMaxPatches
	}
	return patches
}

// openAITileCount fits the image within 2048x2048, downscales the shortest side
// to 768px when larger, and counts 512px tiles.
func openAITileCount(width, height int) uint64 {
	w, h := scaleToFit(width, height, openAITileMaxEdge, openAITileMaxEdge)
	if w <= h && w > openAITileShortEdge {
		w, h = openAITileShortEdge, h*openAITileShortEdge/w
	} else if h < w && h > openAITileShortEdge {
		w, h = w*openAITileShortEdge/h, openAITileShortEdge
	}
	return ceilDiv(w, openAITileSize) * ceilDiv(h, openAITileSize)
}
