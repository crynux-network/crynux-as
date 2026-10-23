package llmadapter

import (
	"bytes"
	"crynux_as/models"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const imagesEditsMaxImages = 16

var imagesEditsAllowedFields = map[string]struct{}{
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
	"stream":              {},
	"partial_images":      {},
	"safety_identifier":   {},
	"user":                {},
	"quantization_config": {},
	"components":          {},
	"vram_limit":          {},
	"images":              {},
	"image":               {},
	"mask":                {},
	"input_fidelity":      {},
	"strength":            {},
	"steps":               {},
	"negative_prompt":     {},
	"dtype":               {},
	"lora":                {},
}

type ImagesEditsRequest struct {
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
	Images                []string
}

// ParseImagesEditsJSONRequest parses an OpenAI Images edits JSON body.
func ParseImagesEditsJSONRequest(body []byte) (ImagesEditsRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ImagesEditsRequest{}, newValidationError("", "invalid JSON body")
	}
	for field := range raw {
		if _, ok := imagesEditsAllowedFields[field]; !ok {
			return ImagesEditsRequest{}, newValidationError(field, "is not supported")
		}
	}
	if value, ok := raw["image"]; ok && !isJSONNull(value) {
		return ImagesEditsRequest{}, newValidationError("image", "is not supported; use images")
	}

	images, err := parseImagesEditsJSONImages(raw["images"])
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	return parseImagesEditsSharedFields(raw, images)
}

// ParseImagesEditsMultipartRequest parses an OpenAI Images edits multipart form.
// Nested Crynux extension objects may arrive either as one JSON form field or as
// OpenAI SDK bracket fields such as quantization_config[quant_kwargs][load_in_4bit].
func ParseImagesEditsMultipartRequest(form *multipart.Form) (ImagesEditsRequest, error) {
	if form == nil {
		return ImagesEditsRequest{}, newValidationError("", "invalid multipart form")
	}
	if _, ok := form.File["mask"]; ok {
		return ImagesEditsRequest{}, newValidationError("mask", "is not supported")
	}
	for field := range form.File {
		switch field {
		case "image", "image[]":
			continue
		case "mask":
			return ImagesEditsRequest{}, newValidationError("mask", "is not supported")
		default:
			return ImagesEditsRequest{}, newValidationError(field, "is not supported")
		}
	}

	images, err := encodeMultipartImageFiles(form)
	if err != nil {
		return ImagesEditsRequest{}, err
	}

	raw, err := multipartFormValuesToRaw(form.Value)
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	return parseImagesEditsSharedFields(raw, images)
}

var imagesEditsNestedObjectFields = map[string]struct{}{
	"quantization_config": {},
	"components":          {},
}

type multipartPathSegment struct {
	key       string
	arrayLeaf bool
}

func multipartFormValuesToRaw(values map[string][]string) (map[string]json.RawMessage, error) {
	raw := map[string]json.RawMessage{}
	nested := map[string]any{}
	nestedUsed := map[string]string{}

	for field, fieldValues := range values {
		if len(fieldValues) == 0 {
			continue
		}
		root, segments, nestedField, err := parseMultipartBracketField(field)
		if err != nil {
			return nil, newValidationError(field, err.Error())
		}
		if !nestedField {
			if _, ok := imagesEditsAllowedFields[field]; !ok {
				return nil, newValidationError(field, "is not supported")
			}
			value := fieldValues[len(fieldValues)-1]
			switch field {
			case "n", "vram_limit", "stream", "partial_images", "output_compression":
				trimmed := strings.TrimSpace(value)
				if trimmed == "" {
					continue
				}
				raw[field] = json.RawMessage(trimmed)
			case "quantization_config", "components":
				trimmed := strings.TrimSpace(value)
				if trimmed == "" {
					continue
				}
				if !json.Valid([]byte(trimmed)) {
					return nil, newValidationError(field, "must be an object or null")
				}
				if other, ok := nestedUsed[field]; ok {
					return nil, newValidationError(field, fmt.Sprintf("must not mix JSON form value with bracket fields such as %s", other))
				}
				raw[field] = json.RawMessage(trimmed)
				nestedUsed[field] = field
			default:
				encoded, err := json.Marshal(value)
				if err != nil {
					return nil, newValidationError(field, "must be a string")
				}
				raw[field] = encoded
			}
			continue
		}

		if _, ok := imagesEditsAllowedFields[root]; !ok {
			return nil, newValidationError(field, "is not supported")
		}
		if _, ok := imagesEditsNestedObjectFields[root]; !ok {
			return nil, newValidationError(field, "is not supported")
		}
		if existing, ok := nestedUsed[root]; ok && existing == root {
			return nil, newValidationError(root, fmt.Sprintf("must not mix JSON form value with bracket fields such as %s", field))
		}
		if err := setMultipartNestedValue(nested, root, segments, fieldValues); err != nil {
			return nil, newValidationError(field, err.Error())
		}
		nestedUsed[root] = field
	}

	for root, value := range nested {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, newValidationError(root, "must be an object or null")
		}
		raw[root] = encoded
	}
	return raw, nil
}

func parseMultipartBracketField(field string) (root string, segments []multipartPathSegment, nested bool, err error) {
	open := strings.IndexByte(field, '[')
	if open < 0 {
		return field, nil, false, nil
	}
	if open == 0 {
		return "", nil, false, fmt.Errorf("is not supported")
	}
	root = field[:open]
	rest := field[open:]
	for len(rest) > 0 {
		if rest[0] != '[' {
			return "", nil, false, fmt.Errorf("is not supported")
		}
		closeIdx := strings.IndexByte(rest, ']')
		if closeIdx < 0 {
			return "", nil, false, fmt.Errorf("is not supported")
		}
		inner := rest[1:closeIdx]
		rest = rest[closeIdx+1:]
		if inner == "" {
			if len(rest) != 0 {
				return "", nil, false, fmt.Errorf("is not supported")
			}
			if len(segments) == 0 {
				return "", nil, false, fmt.Errorf("is not supported")
			}
			segments[len(segments)-1].arrayLeaf = true
			continue
		}
		segments = append(segments, multipartPathSegment{key: inner})
	}
	if len(segments) == 0 {
		return "", nil, false, fmt.Errorf("is not supported")
	}
	return root, segments, true, nil
}

func setMultipartNestedValue(roots map[string]any, root string, segments []multipartPathSegment, values []string) error {
	current, ok := roots[root]
	if !ok {
		current = map[string]any{}
		roots[root] = current
	}
	obj, ok := current.(map[string]any)
	if !ok {
		return fmt.Errorf("must be an object or null")
	}

	for i, segment := range segments {
		last := i == len(segments)-1
		if last {
			coerced := make([]any, 0, len(values))
			for _, value := range values {
				coerced = append(coerced, coerceMultipartScalar(value))
			}
			if segment.arrayLeaf {
				existing, exists := obj[segment.key]
				if !exists {
					obj[segment.key] = coerced
					return nil
				}
				arr, ok := existing.([]any)
				if !ok {
					return fmt.Errorf("must be an object or null")
				}
				obj[segment.key] = append(arr, coerced...)
				return nil
			}
			if len(coerced) != 1 {
				return fmt.Errorf("must be an object or null")
			}
			if _, exists := obj[segment.key]; exists {
				return fmt.Errorf("is not supported")
			}
			obj[segment.key] = coerced[0]
			return nil
		}
		if segment.arrayLeaf {
			return fmt.Errorf("is not supported")
		}
		next, exists := obj[segment.key]
		if !exists {
			next = map[string]any{}
			obj[segment.key] = next
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("must be an object or null")
		}
		obj = child
	}
	return fmt.Errorf("is not supported")
}

func coerceMultipartScalar(value string) any {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if trimmed == "" {
		return value
	}
	if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}
	return value
}

func parseImagesEditsSharedFields(raw map[string]json.RawMessage, images []string) (ImagesEditsRequest, error) {
	if len(images) == 0 {
		return ImagesEditsRequest{}, newValidationError("images", "at least one image is required")
	}
	if len(images) > imagesEditsMaxImages {
		return ImagesEditsRequest{}, newValidationError("images", fmt.Sprintf("must contain at most %d images", imagesEditsMaxImages))
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
		return ImagesEditsRequest{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return ImagesEditsRequest{}, newValidationError("prompt", "prompt is required")
	}
	if utf8.RuneCountInString(prompt) > imagesMaxPromptRunes {
		return ImagesEditsRequest{}, newValidationError("prompt", fmt.Sprintf("must be at most %d characters", imagesMaxPromptRunes))
	}

	model, err := getString("model")
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	model = NormalizeModelID(model)
	if model == "" {
		return ImagesEditsRequest{}, newValidationError("model", "model is required")
	}
	if err := ValidateHuggingFaceModelID(model); err != nil {
		return ImagesEditsRequest{}, err
	}

	n := 1
	if value, ok := raw["n"]; ok && !isJSONNull(value) {
		var parsed float64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return ImagesEditsRequest{}, newValidationError("n", "must be an integer")
		}
		if parsed != float64(int(parsed)) {
			return ImagesEditsRequest{}, newValidationError("n", "must be an integer")
		}
		n = int(parsed)
		if n < 1 || n > 10 {
			return ImagesEditsRequest{}, newValidationError("n", "must be between 1 and 10")
		}
	}

	size, err := getString("size")
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	width, height, echoSize, err := parseImagesSize(size)
	if err != nil {
		return ImagesEditsRequest{}, err
	}

	quality, err := getString("quality")
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	steps, echoQuality, err := mapImagesQualityToSteps(quality)
	if err != nil {
		return ImagesEditsRequest{}, err
	}

	responseFormat, err := getString("response_format")
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	if responseFormat == "" {
		responseFormat = "b64_json"
	}
	if responseFormat != "b64_json" {
		return ImagesEditsRequest{}, newValidationError("response_format", "only b64_json is supported")
	}

	outputFormat, err := getString("output_format")
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	if outputFormat == "" {
		outputFormat = "png"
	}
	if outputFormat != "png" {
		return ImagesEditsRequest{}, newValidationError("output_format", "only png is supported")
	}

	for _, field := range []string{
		"output_compression",
		"background",
		"moderation",
		"partial_images",
		"mask",
		"input_fidelity",
		"strength",
		"steps",
		"negative_prompt",
		"dtype",
		"lora",
	} {
		if value, ok := raw[field]; ok && !isJSONNull(value) {
			return ImagesEditsRequest{}, newValidationError(field, "is not supported")
		}
	}
	if value, ok := raw["stream"]; ok && !isJSONNull(value) {
		var stream bool
		if err := json.Unmarshal(value, &stream); err != nil {
			return ImagesEditsRequest{}, newValidationError("stream", "must be a boolean")
		}
		if stream {
			return ImagesEditsRequest{}, newValidationError("stream", "streaming is not supported")
		}
	}

	safetyIdentifier, err := getString("safety_identifier")
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	user, err := getString("user")
	if err != nil {
		return ImagesEditsRequest{}, err
	}

	quantizationConfig, hasQuantizationConfig, err := getObjectOrNull("quantization_config", raw)
	if err != nil {
		return ImagesEditsRequest{}, err
	}
	components, hasComponents, err := getObjectOrNull("components", raw)
	if err != nil {
		return ImagesEditsRequest{}, err
	}

	var vramLimit *uint64
	if value, ok := raw["vram_limit"]; ok && !isJSONNull(value) {
		var parsed uint64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return ImagesEditsRequest{}, newValidationError("vram_limit", "must be an unsigned integer")
		}
		vramLimit = &parsed
	}

	return ImagesEditsRequest{
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
		Images:                images,
	}, nil
}

func parseImagesEditsJSONImages(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, newValidationError("images", "at least one image is required")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, newValidationError("images", "must be an array of image objects")
		}
		items = []json.RawMessage{json.RawMessage(trimmed)}
	}
	if len(items) == 0 {
		return nil, newValidationError("images", "at least one image is required")
	}
	if len(items) > imagesEditsMaxImages {
		return nil, newValidationError("images", fmt.Sprintf("must contain at most %d images", imagesEditsMaxImages))
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			return nil, newValidationError("images", fmt.Sprintf("images[%d] must be an object", i))
		}
		for field := range obj {
			if field != "image_url" && field != "file_id" {
				return nil, newValidationError("images", fmt.Sprintf("images[%d].%s is not supported", i, field))
			}
		}
		_, hasFileID := obj["file_id"]
		imageURLRaw, hasImageURL := obj["image_url"]
		if hasFileID && !isJSONNull(obj["file_id"]) {
			return nil, newValidationError("images", fmt.Sprintf("images[%d].file_id is not supported", i))
		}
		if !hasImageURL || isJSONNull(imageURLRaw) {
			return nil, newValidationError("images", fmt.Sprintf("images[%d].image_url is required", i))
		}
		var imageURL string
		if err := json.Unmarshal(imageURLRaw, &imageURL); err != nil {
			return nil, newValidationError("images", fmt.Sprintf("images[%d].image_url must be a string", i))
		}
		if err := validateImagesEditsDataURL(imageURL); err != nil {
			return nil, newValidationError("images", fmt.Sprintf("images[%d].image_url %s", i, err.Error()))
		}
		out = append(out, imageURL)
	}
	return out, nil
}

func validateImagesEditsDataURL(imageURL string) error {
	trimmed := strings.TrimSpace(imageURL)
	if trimmed == "" {
		return fmt.Errorf("must be a data URL")
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("http and https URLs are not supported")
	}
	if !strings.HasPrefix(lower, "data:image/") {
		return fmt.Errorf("must be a data:image/...;base64 URL")
	}
	comma := strings.Index(trimmed, ",")
	if comma < 0 {
		return fmt.Errorf("must be a data:image/...;base64 URL")
	}
	header := strings.ToLower(trimmed[:comma])
	if !strings.Contains(header, ";base64") {
		return fmt.Errorf("must be a data:image/...;base64 URL")
	}
	payload := trimmed[comma+1:]
	if payload == "" {
		return fmt.Errorf("must include base64 image data")
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		if _, err := base64.RawStdEncoding.DecodeString(payload); err != nil {
			return fmt.Errorf("must include valid base64 image data")
		}
	}
	return nil
}

func encodeMultipartImageFiles(form *multipart.Form) ([]string, error) {
	files := make([]*multipart.FileHeader, 0)
	files = append(files, form.File["image"]...)
	files = append(files, form.File["image[]"]...)
	if len(files) == 0 {
		return nil, newValidationError("image", "at least one image is required")
	}
	if len(files) > imagesEditsMaxImages {
		return nil, newValidationError("image", fmt.Sprintf("must contain at most %d images", imagesEditsMaxImages))
	}
	out := make([]string, 0, len(files))
	for i, header := range files {
		file, err := header.Open()
		if err != nil {
			return nil, newValidationError("image", fmt.Sprintf("failed to read image[%d]", i))
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return nil, newValidationError("image", fmt.Sprintf("failed to read image[%d]", i))
		}
		if len(data) == 0 {
			return nil, newValidationError("image", fmt.Sprintf("image[%d] must not be empty", i))
		}
		mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
		if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			mimeType = http.DetectContentType(data)
		}
		if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			mimeType = "image/png"
		}
		out = append(out, fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)))
	}
	return out, nil
}

// BuildImagesEditsTaskArgs builds Image TaskArgs JSON for Bridge.
func BuildImagesEditsTaskArgs(req ImagesEditsRequest) (string, error) {
	payload := map[string]any{
		"version": models.GPTTaskArgsVersion,
		"base_model": map[string]any{
			"name": req.Model,
		},
		"prompt": req.Prompt,
		"images": req.Images,
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

// FormatImagesEditsResponse builds the OpenAI Images response for HTTP only.
func FormatImagesEditsResponse(created int64, req ImagesEditsRequest, pngs [][]byte) ([]byte, error) {
	return FormatImagesGenerationsResponse(created, ImagesGenerationsRequest{
		EchoSize:         req.EchoSize,
		EchoQuality:      req.EchoQuality,
		EchoOutputFormat: req.EchoOutputFormat,
	}, pngs)
}
