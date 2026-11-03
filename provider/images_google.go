package provider

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type GoogleImagesUpstreamRequest struct {
	Body           []byte
	AspectRatio    string
	ImageSize      string
	OutputFormat   string // empty means return upstream bytes
	OutputCompress *int
	N              int
}

// ValidateAndBuildGoogleImagesRequest validates Google-supported Images fields
// and builds one generateContent body. Callers repeat the call for n > 1.
func ValidateAndBuildGoogleImagesRequest(req CanonicalImagesRequest) (GoogleImagesUpstreamRequest, error) {
	if err := rejectCrynuxAndUnknown(req); err != nil {
		return GoogleImagesUpstreamRequest{}, err
	}
	if req.APIType == models.TaskAPITypeImagesEdits && len(req.Images) == 0 {
		return GoogleImagesUpstreamRequest{}, fmt.Errorf("images is required")
	}
	if req.Quality != "" {
		return GoogleImagesUpstreamRequest{}, fmt.Errorf("quality is not supported for Google image models")
	}
	if req.Background != "" {
		return GoogleImagesUpstreamRequest{}, fmt.Errorf("background is not supported for Google image models")
	}
	if req.Moderation != "" {
		return GoogleImagesUpstreamRequest{}, fmt.Errorf("moderation is not supported for Google image models")
	}
	if req.Mask != nil || req.Markup != nil || req.InputFidelity != "" {
		return GoogleImagesUpstreamRequest{}, fmt.Errorf("mask, markup, and input_fidelity are not supported for Google image models")
	}

	outputFormat := strings.TrimSpace(req.OutputFormat)
	switch outputFormat {
	case "", "png", "jpeg", "webp":
	default:
		return GoogleImagesUpstreamRequest{}, fmt.Errorf("output_format must be png, jpeg, or webp")
	}
	if req.OutputCompression != nil {
		if outputFormat == "" || outputFormat == "png" {
			return GoogleImagesUpstreamRequest{}, fmt.Errorf("output_compression requires output_format jpeg or webp")
		}
	}

	aspect, imageSize := mapSizeToGoogleImageConfig(req.Width, req.Height)
	parts := make([]map[string]any, 0, 1+len(req.Images))
	for _, img := range req.Images {
		parts = append(parts, map[string]any{
			"inlineData": map[string]string{
				"mimeType": img.MIMEType,
				"data":     img.Base64,
			},
		})
	}
	parts = append(parts, map[string]any{"text": req.Prompt})

	body := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": parts},
		},
		"generationConfig": map[string]any{
			"responseModalities": []string{"IMAGE"},
			"imageConfig": map[string]string{
				"aspectRatio": aspect,
				"imageSize":   imageSize,
			},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return GoogleImagesUpstreamRequest{}, err
	}
	return GoogleImagesUpstreamRequest{
		Body:           encoded,
		AspectRatio:    aspect,
		ImageSize:      imageSize,
		OutputFormat:   outputFormat,
		OutputCompress: req.OutputCompression,
		N:              req.N,
	}, nil
}

func mapSizeToGoogleImageConfig(width, height int) (aspectRatio, imageSize string) {
	if width <= 0 {
		width = imagesDefaultWidth
	}
	if height <= 0 {
		height = imagesDefaultHeight
	}
	aspectRatio = nearestGoogleAspectRatio(width, height)
	longEdge := width
	if height > longEdge {
		longEdge = height
	}
	switch {
	case longEdge <= 768:
		imageSize = "512"
	case longEdge <= 1536:
		imageSize = "1K"
	case longEdge <= 3072:
		imageSize = "2K"
	default:
		imageSize = "4K"
	}
	return aspectRatio, imageSize
}

func nearestGoogleAspectRatio(width, height int) string {
	candidates := []struct {
		label string
		ratio float64
	}{
		{"1:1", 1}, {"2:3", 2.0 / 3.0}, {"3:2", 1.5}, {"3:4", 0.75}, {"4:3", 4.0 / 3.0},
		{"4:5", 0.8}, {"5:4", 1.25}, {"9:16", 9.0 / 16.0}, {"16:9", 16.0 / 9.0},
		{"21:9", 21.0 / 9.0}, {"1:4", 0.25}, {"4:1", 4}, {"1:8", 0.125}, {"8:1", 8},
	}
	target := float64(width) / float64(height)
	best := candidates[0]
	bestDelta := math.Abs(math.Log(target / best.ratio))
	for _, c := range candidates[1:] {
		delta := math.Abs(math.Log(target / c.ratio))
		if delta < bestDelta {
			best = c
			bestDelta = delta
		}
	}
	return best.label
}

// EstimateGoogleImageOutputTokens returns the official approximate image output
// token count for one image at the mapped Google imageSize.
func EstimateGoogleImageOutputTokens(imageSize string) uint64 {
	switch imageSize {
	case "512":
		return 560
	case "1K":
		return 1120
	case "2K":
		return 1680
	case "4K":
		return 3780
	default:
		return 1120
	}
}
