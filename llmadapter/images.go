package llmadapter

import (
	"crynux_as/models"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	imagesMaxPromptRunes = 32000
	imagesDefaultWidth   = 1024
	imagesDefaultHeight  = 1024
	imagesMaxEdge        = 3840
	imagesMaxPixels      = 3840 * 2160
)

var imagesAllowedFields = map[string]struct{}{
	"prompt":              {},
	"model":               {},
	"n":                   {},
	"size":                {},
	"quality":             {},
	"response_format":     {},
	"output_format":       {},
	"output_compression":  {},
	"background":          {},
	"moderation":          {},
	"style":               {},
	"stream":              {},
	"partial_images":      {},
	"safety_identifier":   {},
	"user":                {},
	"quantization_config": {},
	"components":          {},
	"vram_limit":          {},
	"queue_timeout":       {},
}

var imagesNamedSizes = map[string][2]int{
	"256x256":   {256, 256},
	"512x512":   {512, 512},
	"1024x1024": {1024, 1024},
	"1024x1536": {1024, 1536},
	"1536x1024": {1536, 1024},
	"1792x1024": {1792, 1024},
	"1024x1792": {1024, 1792},
}

type ImagesGenerationsRequest struct {
	Prompt                string
	Model                 string
	N                     int
	Size                  string
	Quality               string
	ResponseFormat        string
	OutputFormat          string
	SafetyIdentifier      string
	User                  string
	Width                 int
	Height                int
	Steps                 int
	EchoSize              string
	EchoQuality           string
	EchoOutputFormat      string
	QuantizationConfig    any
	HasQuantizationConfig bool
	Components            any
	HasComponents         bool
	VramLimit             *uint64
	QueueTimeout          *uint64
}

type ImagesGenerationsResponse struct {
	Created      int64                    `json:"created"`
	Data         []ImagesGenerationsDatum `json:"data"`
	Size         string                   `json:"size,omitempty"`
	Quality      string                   `json:"quality,omitempty"`
	OutputFormat string                   `json:"output_format,omitempty"`
}

type ImagesGenerationsDatum struct {
	B64JSON string `json:"b64_json"`
}

// ParseImagesGenerationsRequest parses an OpenAI Images generations body.
func ParseImagesGenerationsRequest(body []byte) (ImagesGenerationsRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ImagesGenerationsRequest{}, newValidationError("", "invalid JSON body")
	}
	for field := range raw {
		if _, ok := imagesAllowedFields[field]; !ok {
			return ImagesGenerationsRequest{}, newValidationError(field, "is not supported")
		}
	}

	getString := func(field string) (string, error) {
		value, ok := raw[field]
		if !ok || isJSONNull(value) {
			return "", nil
		}
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return "", newValidationError(field, "must be a string")
		}
		return s, nil
	}

	prompt, err := getString("prompt")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return ImagesGenerationsRequest{}, newValidationError("prompt", "prompt is required")
	}
	if utf8.RuneCountInString(prompt) > imagesMaxPromptRunes {
		return ImagesGenerationsRequest{}, newValidationError("prompt", fmt.Sprintf("must be at most %d characters", imagesMaxPromptRunes))
	}

	model, err := getString("model")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	model = NormalizeModelID(model)
	if model == "" {
		return ImagesGenerationsRequest{}, newValidationError("model", "model is required")
	}
	if err := ValidateHuggingFaceModelID(model); err != nil {
		return ImagesGenerationsRequest{}, err
	}

	n := 1
	if value, ok := raw["n"]; ok && !isJSONNull(value) {
		var parsed float64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return ImagesGenerationsRequest{}, newValidationError("n", "must be an integer")
		}
		if parsed != float64(int(parsed)) {
			return ImagesGenerationsRequest{}, newValidationError("n", "must be an integer")
		}
		n = int(parsed)
		if n < 1 || n > 10 {
			return ImagesGenerationsRequest{}, newValidationError("n", "must be between 1 and 10")
		}
	}

	size, err := getString("size")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	width, height, echoSize, err := parseImagesSize(size)
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}

	quality, err := getString("quality")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	steps, echoQuality, err := mapImagesQualityToSteps(quality)
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}

	responseFormat, err := getString("response_format")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	if responseFormat == "" {
		responseFormat = "b64_json"
	}
	if responseFormat != "b64_json" {
		return ImagesGenerationsRequest{}, newValidationError("response_format", "only b64_json is supported")
	}

	outputFormat, err := getString("output_format")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	if outputFormat == "" {
		outputFormat = "png"
	}
	if outputFormat != "png" {
		return ImagesGenerationsRequest{}, newValidationError("output_format", "only png is supported")
	}

	if value, ok := raw["output_compression"]; ok && !isJSONNull(value) {
		return ImagesGenerationsRequest{}, newValidationError("output_compression", "is not supported")
	}
	if value, ok := raw["background"]; ok && !isJSONNull(value) {
		return ImagesGenerationsRequest{}, newValidationError("background", "is not supported")
	}
	if value, ok := raw["moderation"]; ok && !isJSONNull(value) {
		return ImagesGenerationsRequest{}, newValidationError("moderation", "is not supported")
	}
	if value, ok := raw["style"]; ok && !isJSONNull(value) {
		return ImagesGenerationsRequest{}, newValidationError("style", "is not supported")
	}
	if value, ok := raw["partial_images"]; ok && !isJSONNull(value) {
		return ImagesGenerationsRequest{}, newValidationError("partial_images", "is not supported")
	}
	if value, ok := raw["stream"]; ok && !isJSONNull(value) {
		var stream bool
		if err := json.Unmarshal(value, &stream); err != nil {
			return ImagesGenerationsRequest{}, newValidationError("stream", "must be a boolean")
		}
		if stream {
			return ImagesGenerationsRequest{}, newValidationError("stream", "streaming is not supported")
		}
	}

	safetyIdentifier, err := getString("safety_identifier")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	user, err := getString("user")
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}

	quantizationConfig, hasQuantizationConfig, err := getObjectOrNull("quantization_config", raw)
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}
	components, hasComponents, err := getObjectOrNull("components", raw)
	if err != nil {
		return ImagesGenerationsRequest{}, err
	}

	var vramLimit *uint64
	if value, ok := raw["vram_limit"]; ok && !isJSONNull(value) {
		var parsed uint64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return ImagesGenerationsRequest{}, newValidationError("vram_limit", "must be an unsigned integer")
		}
		vramLimit = &parsed
	}
	var queueTimeout *uint64
	if value, ok := raw["queue_timeout"]; ok && !isJSONNull(value) {
		var parsed uint64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return ImagesGenerationsRequest{}, newValidationError("queue_timeout", "must be an unsigned integer")
		}
		if parsed == 0 {
			return ImagesGenerationsRequest{}, newValidationError("queue_timeout", "must be greater than 0")
		}
		queueTimeout = &parsed
	}

	return ImagesGenerationsRequest{
		Prompt:                prompt,
		Model:                 model,
		N:                     n,
		Size:                  size,
		Quality:               quality,
		ResponseFormat:        responseFormat,
		OutputFormat:          outputFormat,
		SafetyIdentifier:      safetyIdentifier,
		User:                  user,
		Width:                 width,
		Height:                height,
		Steps:                 steps,
		EchoSize:              echoSize,
		EchoQuality:           echoQuality,
		EchoOutputFormat:      outputFormat,
		QuantizationConfig:    quantizationConfig,
		HasQuantizationConfig: hasQuantizationConfig,
		Components:            components,
		HasComponents:         hasComponents,
		VramLimit:             vramLimit,
		QueueTimeout:          queueTimeout,
	}, nil
}

func getObjectOrNull(field string, raw map[string]json.RawMessage) (any, bool, error) {
	value, ok := raw[field]
	if !ok {
		return nil, false, nil
	}
	if isJSONNull(value) {
		return nil, true, nil
	}
	var parsed any
	if err := json.Unmarshal(value, &parsed); err != nil {
		return nil, false, newValidationError(field, "must be an object or null")
	}
	if _, isObject := parsed.(map[string]any); !isObject {
		return nil, false, newValidationError(field, "must be an object or null")
	}
	return parsed, true, nil
}

func parseImagesSize(size string) (width, height int, echo string, err error) {
	trimmed := strings.TrimSpace(size)
	if trimmed == "" || trimmed == "auto" {
		return imagesDefaultWidth, imagesDefaultHeight, "1024x1024", nil
	}
	if dims, ok := imagesNamedSizes[trimmed]; ok {
		return dims[0], dims[1], trimmed, nil
	}
	parts := strings.Split(strings.ToLower(trimmed), "x")
	if len(parts) != 2 {
		return 0, 0, "", newValidationError("size", "must be auto, a named size, or WIDTHxHEIGHT")
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, "", newValidationError("size", "width and height must be positive integers")
	}
	if w%8 != 0 || h%8 != 0 {
		return 0, 0, "", newValidationError("size", "width and height must be divisible by 8")
	}
	if w > imagesMaxEdge || h > imagesMaxEdge {
		return 0, 0, "", newValidationError("size", fmt.Sprintf("each edge must be at most %d", imagesMaxEdge))
	}
	if w*h > imagesMaxPixels {
		return 0, 0, "", newValidationError("size", "pixel count exceeds the maximum")
	}
	ratio := float64(w) / float64(h)
	if ratio < 1.0/3.0 || ratio > 3.0 {
		return 0, 0, "", newValidationError("size", "aspect ratio must be between 1:3 and 3:1")
	}
	echo = fmt.Sprintf("%dx%d", w, h)
	return w, h, echo, nil
}

func mapImagesQualityToSteps(quality string) (steps int, echo string, err error) {
	trimmed := strings.TrimSpace(strings.ToLower(quality))
	switch trimmed {
	case "":
		return 30, "auto", nil
	case "auto", "medium", "standard":
		return 30, trimmed, nil
	case "low":
		return 20, "low", nil
	case "high", "hd", "xhigh", "max":
		return 50, trimmed, nil
	default:
		return 0, "", newValidationError("quality", "unsupported quality value")
	}
}

func encodeImagesBase64(png []byte) string {
	return base64.StdEncoding.EncodeToString(png)
}

// BuildImagesGenerationsTaskArgs builds Image TaskArgs JSON for Bridge.
func BuildImagesGenerationsTaskArgs(req ImagesGenerationsRequest) (string, error) {
	payload := map[string]any{
		"version": models.GPTTaskArgsVersion,
		"base_model": map[string]any{
			"name": req.Model,
		},
		"prompt": req.Prompt,
		"task_config": map[string]any{
			"image_width":           req.Width,
			"image_height":          req.Height,
			"steps":                 req.Steps,
			"num_images_per_prompt": req.N,
		},
	}
	if req.HasQuantizationConfig {
		payload["quantization_config"] = req.QuantizationConfig
	}
	if req.HasComponents {
		payload["components"] = req.Components
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FormatImagesGenerationsResponse builds the OpenAI Images response for HTTP only.
func FormatImagesGenerationsResponse(created int64, req ImagesGenerationsRequest, pngs [][]byte) ([]byte, error) {
	data := make([]ImagesGenerationsDatum, 0, len(pngs))
	for _, png := range pngs {
		if len(png) == 0 {
			return nil, fmt.Errorf("empty image bytes")
		}
		data = append(data, ImagesGenerationsDatum{
			B64JSON: encodeImagesBase64(png),
		})
	}
	quality := req.EchoQuality
	if quality == "" {
		quality = "auto"
	}
	resp := ImagesGenerationsResponse{
		Created:      created,
		Data:         data,
		Size:         req.EchoSize,
		Quality:      quality,
		OutputFormat: req.EchoOutputFormat,
	}
	return json.Marshal(resp)
}
