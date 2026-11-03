package provider

import (
	"crynux_as/models"
	"fmt"
	"math"
	"strings"
)

const (
	openAIImagesEditBaseTokens    uint64 = 65
	openAIImagesEditTileTokens    uint64 = 129
	openAIImagesEditShortEdge            = 512
	openAIImagesEditTileEdge             = 512
	openAIImagesEditHighSquare           = 4160
	openAIImagesEditHighNonSquare        = 6240
)

// EstimateProviderImagesEditInputTokens returns the admission upper bound for
// input images on Provider Images edits. Chat Completions / Responses vision
// rules MUST NOT be used here.
func EstimateProviderImagesEditInputTokens(serviceProvider models.ServiceProvider, model string, images []CanonicalImageInfo, inputFidelity string) (uint64, error) {
	if len(images) == 0 {
		return 0, nil
	}
	name := strings.ToLower(strings.TrimSpace(model))
	switch serviceProvider {
	case models.ServiceProviderOpenAI:
		return estimateOpenAIImagesEditInputTokens(name, images, inputFidelity)
	case models.ServiceProviderGoogle:
		return estimateGoogleImagesEditInputTokens(name, images)
	default:
		return 0, fmt.Errorf("image edit input is not supported for service provider %q", serviceProvider)
	}
}

func estimateOpenAIImagesEditInputTokens(model string, images []CanonicalImageInfo, inputFidelity string) (uint64, error) {
	if !isOpenAIImagesModel(model) {
		return 0, unsupportedImageModelError(model)
	}
	fidelity := strings.ToLower(strings.TrimSpace(inputFidelity))
	if fidelity == "" {
		fidelity = "low"
	}
	switch fidelity {
	case "low", "high":
	default:
		return 0, fmt.Errorf("input_fidelity must be high or low")
	}
	var total uint64
	for _, image := range images {
		tokens, err := openAIImagesEditImageTokens(image.Width, image.Height, fidelity)
		if err != nil {
			return 0, err
		}
		total += tokens
	}
	return total, nil
}

func isOpenAIImagesModel(model string) bool {
	name := stripOpenAIDateSuffix(model)
	return name == "gpt-image-1" ||
		name == "gpt-image-1-mini" ||
		name == "chatgpt-image-latest" ||
		hasModelPrefix(name, "gpt-image-1") ||
		hasModelPrefix(name, "gpt-image-1.5") ||
		hasModelPrefix(name, "gpt-image-2") ||
		hasModelPrefix(name, "gpt-image-2.5")
}

func openAIImagesEditImageTokens(width, height int, fidelity string) (uint64, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("image dimensions are invalid")
	}
	scaledW, scaledH := scaleShortestSide(width, height, openAIImagesEditShortEdge)
	tiles := ceilDiv(scaledW, openAIImagesEditTileEdge) * ceilDiv(scaledH, openAIImagesEditTileEdge)
	if tiles == 0 {
		tiles = 1
	}
	tokens := openAIImagesEditBaseTokens + openAIImagesEditTileTokens*tiles
	if fidelity == "high" {
		if width == height {
			tokens += openAIImagesEditHighSquare
		} else {
			tokens += openAIImagesEditHighNonSquare
		}
	}
	return tokens, nil
}

// scaleShortestSide downscales so the shortest side equals target when larger.
func scaleShortestSide(width, height int, target float64) (float64, float64) {
	w, h := float64(width), float64(height)
	short := math.Min(w, h)
	if short <= 0 || short <= target {
		return w, h
	}
	scale := target / short
	return w * scale, h * scale
}

func estimateGoogleImagesEditInputTokens(model string, images []CanonicalImageInfo) (uint64, error) {
	if !isGoogleImagesModel(model) {
		return 0, unsupportedImageModelError(model)
	}
	return googleGemini3ImageTokens * uint64(len(images)), nil
}

func isGoogleImagesModel(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(name, "image"):
		return true
	case strings.Contains(name, "nano-banana"):
		return true
	case googleDefaultResolutionModel.MatchString(name):
		return true
	default:
		return false
	}
}
