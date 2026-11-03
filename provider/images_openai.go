package provider

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateAndBuildOpenAIImagesRequest validates OpenAI Images fields and builds
// an upstream JSON body for generations or edits.
func ValidateAndBuildOpenAIImagesRequest(req CanonicalImagesRequest) ([]byte, error) {
	if err := rejectCrynuxAndUnknown(req); err != nil {
		return nil, err
	}
	if req.APIType == models.TaskAPITypeImagesEdits {
		if len(req.Images) == 0 {
			return nil, fmt.Errorf("images is required")
		}
	} else if len(req.Images) > 0 || req.Mask != nil || req.Markup != nil || req.InputFidelity != "" {
		return nil, fmt.Errorf("edit-only fields are not supported on images generations")
	}

	outputFormat := strings.TrimSpace(req.OutputFormat)
	if outputFormat == "" {
		outputFormat = "png"
	}
	switch outputFormat {
	case "png", "jpeg", "webp":
	default:
		return nil, fmt.Errorf("output_format must be png, jpeg, or webp")
	}
	background := strings.TrimSpace(req.Background)
	if background != "" {
		switch background {
		case "transparent", "opaque", "auto":
		default:
			return nil, fmt.Errorf("background must be transparent, opaque, or auto")
		}
		if background == "transparent" && outputFormat == "jpeg" {
			return nil, fmt.Errorf("background=transparent requires output_format png or webp")
		}
	}
	if req.OutputCompression != nil && outputFormat == "png" {
		return nil, fmt.Errorf("output_compression is only supported for jpeg or webp")
	}
	if req.Moderation != "" {
		switch req.Moderation {
		case "low", "auto":
		default:
			return nil, fmt.Errorf("moderation must be low or auto")
		}
	}
	if req.InputFidelity != "" {
		switch req.InputFidelity {
		case "high", "low":
		default:
			return nil, fmt.Errorf("input_fidelity must be high or low")
		}
		if req.APIType != models.TaskAPITypeImagesEdits {
			return nil, fmt.Errorf("input_fidelity is only supported on images edits")
		}
	}
	if req.Quality != "" {
		switch req.Quality {
		case "low", "medium", "high", "xhigh", "max", "auto", "standard", "hd":
		default:
			return nil, fmt.Errorf("quality is invalid")
		}
	}

	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
		"n":      req.N,
	}
	if req.EchoSize != "" {
		body["size"] = req.EchoSize
	}
	if req.Quality != "" {
		body["quality"] = req.Quality
	}
	body["output_format"] = outputFormat
	if background != "" {
		body["background"] = background
	}
	if req.Moderation != "" {
		body["moderation"] = req.Moderation
	}
	if req.OutputCompression != nil {
		body["output_compression"] = *req.OutputCompression
	}
	if req.APIType == models.TaskAPITypeImagesEdits {
		images := make([]map[string]string, 0, len(req.Images))
		for _, img := range req.Images {
			images = append(images, map[string]string{"image_url": img.DataURL})
		}
		body["images"] = images
		if req.Mask != nil {
			body["mask"] = map[string]string{"image_url": req.Mask.DataURL}
		}
		if req.Markup != nil {
			body["markup"] = map[string]string{"image_url": req.Markup.DataURL}
		}
		if req.InputFidelity != "" {
			body["input_fidelity"] = req.InputFidelity
		}
	}
	return json.Marshal(body)
}
