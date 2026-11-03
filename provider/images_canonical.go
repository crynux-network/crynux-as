package provider

import (
	"crynux_as/models"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
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

// CanonicalImagesRequest is the shared OpenAI-shaped Images request after parse.
// Provider converters validate which fields they accept.
type CanonicalImagesRequest struct {
	APIType              models.TaskAPIType
	Prompt               string
	Model                string
	N                    int
	Size                 string
	Quality              string
	ResponseFormat       string
	OutputFormat         string
	OutputCompression    *int
	Background           string
	Moderation           string
	InputFidelity        string
	SafetyIdentifier     string
	User                 string
	QueueTimeout         *uint64
	Width                int
	Height               int
	EchoSize             string
	Images               []CanonicalImageDataURL
	Mask                 *CanonicalImageDataURL
	Markup               *CanonicalImageDataURL
	RawFields            map[string]json.RawMessage
	UnknownFields        []string
	CrynuxExtensionFields []string
}

type CanonicalImageDataURL struct {
	DataURL  string
	MIMEType string
	Base64   string
	Width    int
	Height   int
}

var imagesCrynuxExtensionFields = map[string]struct{}{
	"quantization_config": {},
	"components":          {},
	"vram_limit":          {},
	"dtype":               {},
	"steps":               {},
	"negative_prompt":     {},
	"lora":                {},
	"strength":            {},
}

var imagesSharedForbiddenFields = map[string]struct{}{
	"stream":         {},
	"partial_images": {},
	"style":          {},
}

func ParseCanonicalImagesJSON(body []byte, apiType models.TaskAPIType) (CanonicalImagesRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return CanonicalImagesRequest{}, fmt.Errorf("invalid JSON body")
	}
	return parseCanonicalImagesMap(raw, apiType)
}

func ParseCanonicalImagesMultipart(form *multipart.Form, apiType models.TaskAPIType) (CanonicalImagesRequest, error) {
	if form == nil {
		return CanonicalImagesRequest{}, fmt.Errorf("invalid multipart form")
	}
	raw := map[string]json.RawMessage{}
	for key, values := range form.Value {
		if len(values) == 0 {
			continue
		}
		raw[key] = mustJSONString(values[0])
	}
	req, err := parseCanonicalImagesMap(raw, apiType)
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	if apiType != models.TaskAPITypeImagesEdits {
		if len(form.File) > 0 {
			return CanonicalImagesRequest{}, fmt.Errorf("file uploads are only supported on images edits")
		}
		return req, nil
	}
	images := make([]CanonicalImageDataURL, 0)
	for _, key := range []string{"image", "image[]"} {
		for _, fh := range form.File[key] {
			dataURL, err := multipartFileToDataURL(fh)
			if err != nil {
				return CanonicalImagesRequest{}, err
			}
			images = append(images, dataURL)
		}
	}
	if len(images) == 0 {
		return CanonicalImagesRequest{}, fmt.Errorf("images is required")
	}
	if len(images) > 16 {
		return CanonicalImagesRequest{}, fmt.Errorf("images must contain at most 16 entries")
	}
	req.Images = images
	if files := form.File["mask"]; len(files) > 0 {
		mask, err := multipartFileToDataURL(files[0])
		if err != nil {
			return CanonicalImagesRequest{}, err
		}
		req.Mask = &mask
	}
	return req, nil
}

func parseCanonicalImagesMap(raw map[string]json.RawMessage, apiType models.TaskAPIType) (CanonicalImagesRequest, error) {
	req := CanonicalImagesRequest{
		APIType:   apiType,
		N:         1,
		RawFields: raw,
		Width:     imagesDefaultWidth,
		Height:    imagesDefaultHeight,
		EchoSize:  "1024x1024",
	}
	for field := range raw {
		if _, ok := imagesCrynuxExtensionFields[field]; ok {
			req.CrynuxExtensionFields = append(req.CrynuxExtensionFields, field)
			continue
		}
		if _, ok := imagesSharedForbiddenFields[field]; ok {
			return CanonicalImagesRequest{}, fmt.Errorf("%s is not supported", field)
		}
	}
	getString := func(field string) (string, error) {
		value, ok := raw[field]
		if !ok || string(value) == "null" {
			return "", nil
		}
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return "", fmt.Errorf("%s must be a string", field)
		}
		return s, nil
	}

	prompt, err := getString("prompt")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return CanonicalImagesRequest{}, fmt.Errorf("prompt is required")
	}
	if utf8.RuneCountInString(prompt) > imagesMaxPromptRunes {
		return CanonicalImagesRequest{}, fmt.Errorf("prompt must be at most %d characters", imagesMaxPromptRunes)
	}
	req.Prompt = prompt

	model, err := getString("model")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return CanonicalImagesRequest{}, fmt.Errorf("model is required")
	}
	req.Model = model

	if value, ok := raw["n"]; ok && string(value) != "null" {
		var parsed float64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return CanonicalImagesRequest{}, fmt.Errorf("n must be an integer")
		}
		if parsed != float64(int(parsed)) || parsed < 1 || parsed > 10 {
			return CanonicalImagesRequest{}, fmt.Errorf("n must be between 1 and 10")
		}
		req.N = int(parsed)
	}

	size, err := getString("size")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	width, height, echoSize, err := parseImagesSize(size)
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.Size = size
	req.Width = width
	req.Height = height
	req.EchoSize = echoSize

	quality, err := getString("quality")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.Quality = quality

	responseFormat, err := getString("response_format")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	if responseFormat != "" && responseFormat != "b64_json" {
		return CanonicalImagesRequest{}, fmt.Errorf("response_format must be b64_json")
	}
	req.ResponseFormat = responseFormat

	outputFormat, err := getString("output_format")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.OutputFormat = outputFormat

	if value, ok := raw["output_compression"]; ok && string(value) != "null" {
		var parsed float64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return CanonicalImagesRequest{}, fmt.Errorf("output_compression must be an integer")
		}
		if parsed != float64(int(parsed)) || parsed < 0 || parsed > 100 {
			return CanonicalImagesRequest{}, fmt.Errorf("output_compression must be between 0 and 100")
		}
		v := int(parsed)
		req.OutputCompression = &v
	}

	background, err := getString("background")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.Background = background

	moderation, err := getString("moderation")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.Moderation = moderation

	inputFidelity, err := getString("input_fidelity")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.InputFidelity = inputFidelity

	safety, err := getString("safety_identifier")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.SafetyIdentifier = safety
	user, err := getString("user")
	if err != nil {
		return CanonicalImagesRequest{}, err
	}
	req.User = user

	if value, ok := raw["queue_timeout"]; ok && string(value) != "null" {
		var parsed float64
		if err := json.Unmarshal(value, &parsed); err != nil {
			return CanonicalImagesRequest{}, fmt.Errorf("queue_timeout must be an unsigned integer")
		}
		if parsed != float64(uint64(parsed)) {
			return CanonicalImagesRequest{}, fmt.Errorf("queue_timeout must be an unsigned integer")
		}
		v := uint64(parsed)
		req.QueueTimeout = &v
	}

	if apiType == models.TaskAPITypeImagesEdits {
		if value, ok := raw["images"]; ok && string(value) != "null" {
			images, err := parseImagesArray(value)
			if err != nil {
				return CanonicalImagesRequest{}, err
			}
			req.Images = images
		}
		if value, ok := raw["mask"]; ok && string(value) != "null" {
			mask, err := parseSingleImageObject(value, "mask")
			if err != nil {
				return CanonicalImagesRequest{}, err
			}
			req.Mask = &mask
		}
		if value, ok := raw["markup"]; ok && string(value) != "null" {
			markup, err := parseSingleImageObject(value, "markup")
			if err != nil {
				return CanonicalImagesRequest{}, err
			}
			req.Markup = &markup
		}
	}

	known := map[string]struct{}{
		"prompt": {}, "model": {}, "n": {}, "size": {}, "quality": {},
		"response_format": {}, "output_format": {}, "output_compression": {},
		"background": {}, "moderation": {}, "input_fidelity": {},
		"safety_identifier": {}, "user": {}, "queue_timeout": {},
		"images": {}, "mask": {}, "markup": {}, "image": {},
	}
	for field := range raw {
		if _, ok := known[field]; ok {
			continue
		}
		if _, ok := imagesCrynuxExtensionFields[field]; ok {
			continue
		}
		req.UnknownFields = append(req.UnknownFields, field)
	}
	return req, nil
}

func parseImagesSize(size string) (width, height int, echo string, err error) {
	trimmed := strings.TrimSpace(size)
	if trimmed == "" || trimmed == "auto" {
		return imagesDefaultWidth, imagesDefaultHeight, "1024x1024", nil
	}
	named := map[string][2]int{
		"256x256": {256, 256}, "512x512": {512, 512}, "1024x1024": {1024, 1024},
		"1024x1536": {1024, 1536}, "1536x1024": {1536, 1024},
		"1792x1024": {1792, 1024}, "1024x1792": {1024, 1792},
	}
	if dims, ok := named[trimmed]; ok {
		return dims[0], dims[1], trimmed, nil
	}
	parts := strings.Split(strings.ToLower(trimmed), "x")
	if len(parts) != 2 {
		return 0, 0, "", fmt.Errorf("size is invalid")
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 0, 0, "", fmt.Errorf("size is invalid")
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil || h <= 0 {
		return 0, 0, "", fmt.Errorf("size is invalid")
	}
	if w%8 != 0 || h%8 != 0 {
		return 0, 0, "", fmt.Errorf("size edges must be divisible by 8")
	}
	if w > imagesMaxEdge || h > imagesMaxEdge {
		return 0, 0, "", fmt.Errorf("size edges must be at most %d", imagesMaxEdge)
	}
	if w*h > imagesMaxPixels {
		return 0, 0, "", fmt.Errorf("size pixel count exceeds limit")
	}
	ratio := float64(w) / float64(h)
	if ratio < 1.0/3.0 || ratio > 3.0 {
		return 0, 0, "", fmt.Errorf("size aspect ratio must be between 1/3 and 3")
	}
	return w, h, fmt.Sprintf("%dx%d", w, h), nil
}

func parseImagesArray(value json.RawMessage) ([]CanonicalImageDataURL, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, fmt.Errorf("images must be an array")
	}
	if len(items) == 0 || len(items) > 16 {
		return nil, fmt.Errorf("images must contain between 1 and 16 entries")
	}
	out := make([]CanonicalImageDataURL, 0, len(items))
	for _, item := range items {
		img, err := parseSingleImageObject(item, "images")
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, nil
}

func parseSingleImageObject(value json.RawMessage, field string) (CanonicalImageDataURL, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(value, &obj); err != nil {
		return CanonicalImageDataURL{}, fmt.Errorf("%s entries must be objects", field)
	}
	if _, ok := obj["file_id"]; ok {
		return CanonicalImageDataURL{}, fmt.Errorf("file_id is not supported")
	}
	var imageURL string
	if raw, ok := obj["image_url"]; ok {
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			imageURL = asString
		} else {
			var nested struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(raw, &nested); err != nil || nested.URL == "" {
				return CanonicalImageDataURL{}, fmt.Errorf("%s.image_url is invalid", field)
			}
			imageURL = nested.URL
		}
	}
	if imageURL == "" {
		return CanonicalImageDataURL{}, fmt.Errorf("%s.image_url is required", field)
	}
	return parseDataURLImage(imageURL)
}

func parseDataURLImage(dataURL string) (CanonicalImageDataURL, error) {
	if strings.HasPrefix(strings.ToLower(dataURL), "http://") || strings.HasPrefix(strings.ToLower(dataURL), "https://") {
		return CanonicalImageDataURL{}, fmt.Errorf("http and https image URLs are not supported")
	}
	if !strings.HasPrefix(dataURL, "data:") {
		return CanonicalImageDataURL{}, fmt.Errorf("image_url must be a data URL")
	}
	comma := strings.IndexByte(dataURL, ',')
	if comma < 0 {
		return CanonicalImageDataURL{}, fmt.Errorf("image_url must be a data URL")
	}
	meta := dataURL[5:comma]
	payload := dataURL[comma+1:]
	parts := strings.Split(meta, ";")
	mimeType := parts[0]
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" {
		return CanonicalImageDataURL{}, fmt.Errorf("unsupported image mime type")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return CanonicalImageDataURL{}, fmt.Errorf("image_url base64 is invalid")
		}
	}
	dims, err := ReadImageDimensions(decoded)
	if err != nil {
		return CanonicalImageDataURL{}, fmt.Errorf("image bytes are invalid")
	}
	return CanonicalImageDataURL{
		DataURL:  dataURL,
		MIMEType: mimeType,
		Base64:   base64.StdEncoding.EncodeToString(decoded),
		Width:    dims.Width,
		Height:   dims.Height,
	}, nil
}

func multipartFileToDataURL(fh *multipart.FileHeader) (CanonicalImageDataURL, error) {
	f, err := fh.Open()
	if err != nil {
		return CanonicalImageDataURL{}, fmt.Errorf("failed to read uploaded image")
	}
	defer f.Close()
	buf := make([]byte, fh.Size)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return CanonicalImageDataURL{}, fmt.Errorf("failed to read uploaded image")
	}
	buf = buf[:n]
	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" || !strings.HasPrefix(mimeType, "image/") {
		mimeType = http.DetectContentType(buf)
	}
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = mediaType
	}
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" {
		mimeType = "image/png"
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(buf)
	return parseDataURLImage(dataURL)
}

func mustJSONString(v string) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func rejectCrynuxAndUnknown(req CanonicalImagesRequest) error {
	if len(req.CrynuxExtensionFields) > 0 {
		return fmt.Errorf("%s is not supported for Provider models", req.CrynuxExtensionFields[0])
	}
	if len(req.UnknownFields) > 0 {
		return fmt.Errorf("%s is not supported", req.UnknownFields[0])
	}
	return nil
}
