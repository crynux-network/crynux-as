package llmadapter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"
)

func tinyPNGDataURL(t *testing.T) string {
	t.Helper()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func TestParseImagesEditsJSONRequestDefaults(t *testing.T) {
	dataURL := tinyPNGDataURL(t)
	body, _ := json.Marshal(map[string]any{
		"model":  "Qwen/Qwen-Image-2.1",
		"prompt": "make it snowy",
		"images": []map[string]string{{"image_url": dataURL}},
	})
	req, err := ParseImagesEditsJSONRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "qwen/qwen-image-2.1" {
		t.Fatalf("model = %q", req.Model)
	}
	if req.N != 1 || req.Width != 1024 || req.Height != 1024 || req.Steps != 40 {
		t.Fatalf("defaults = n=%d size=%dx%d steps=%d", req.N, req.Width, req.Height, req.Steps)
	}
	if len(req.Images) != 1 || req.Images[0] != dataURL {
		t.Fatalf("images = %#v", req.Images)
	}
}

func TestParseAndBuildImagesEditsTaskArgs(t *testing.T) {
	dataURL := tinyPNGDataURL(t)
	body, _ := json.Marshal(map[string]any{
		"model":   "org/model",
		"prompt":  "edit",
		"n":       2,
		"size":    "512x512",
		"quality": "low",
		"images":  []map[string]string{{"image_url": dataURL}},
		"quantization_config": map[string]any{
			"quant_method":             "bitsandbytes",
			"components_to_quantize":   []string{"text_encoder"},
			"quant_kwargs":             map[string]any{"load_in_8bit": true},
		},
		"components": map[string]any{
			"transformer": map[string]any{
				"format":        "gguf",
				"repo":          "org/repo",
				"format_kwargs": map[string]any{"filename": "a.gguf"},
			},
		},
	})
	req, err := ParseImagesEditsJSONRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	taskArgs, err := BuildImagesEditsTaskArgs(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgs), &payload); err != nil {
		t.Fatal(err)
	}
	images, ok := payload["images"].([]any)
	if !ok || len(images) != 1 || images[0] != dataURL {
		t.Fatalf("images = %#v", payload["images"])
	}
	cfg := payload["task_config"].(map[string]any)
	if cfg["steps"] != float64(20) || cfg["num_images_per_prompt"] != float64(2) {
		t.Fatalf("task_config = %#v", cfg)
	}
	if payload["quantization_config"] == nil || payload["components"] == nil {
		t.Fatalf("missing extensions in %s", taskArgs)
	}
}

func TestParseImagesEditsJSONRequestRejectsUnsupported(t *testing.T) {
	dataURL := tinyPNGDataURL(t)
	base := map[string]any{
		"model":  "org/m",
		"prompt": "a",
		"images": []map[string]string{{"image_url": dataURL}},
	}
	cases := []map[string]any{
		{"mask": map[string]string{"image_url": dataURL}},
		{"input_fidelity": "high"},
		{"background": "auto"},
		{"moderation": "auto"},
		{"stream": true},
		{"partial_images": 1},
		{"response_format": "url"},
		{"output_format": "jpeg"},
		{"strength": 0.5},
		{"dtype": "bfloat16"},
		{"steps": 20},
		{"image": dataURL},
	}
	for _, extra := range cases {
		bodyMap := map[string]any{}
		for k, v := range base {
			bodyMap[k] = v
		}
		for k, v := range extra {
			bodyMap[k] = v
		}
		body, _ := json.Marshal(bodyMap)
		if _, err := ParseImagesEditsJSONRequest(body); err == nil {
			t.Fatalf("expected error for extra %#v", extra)
		}
	}
}

func TestParseImagesEditsJSONRequestAcceptsSingleObject(t *testing.T) {
	dataURL := tinyPNGDataURL(t)
	body, _ := json.Marshal(map[string]any{
		"model":  "org/model",
		"prompt": "edit",
		"images": map[string]string{"image_url": dataURL},
	})
	req, err := ParseImagesEditsJSONRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Images) != 1 || req.Images[0] != dataURL {
		t.Fatalf("images = %#v", req.Images)
	}
}

func TestParseImagesEditsJSONRequestRejectsHTTPURLAndFileID(t *testing.T) {
	cases := []string{
		`{"model":"org/m","prompt":"a","images":[{"image_url":"https://example.com/a.png"}]}`,
		`{"model":"org/m","prompt":"a","images":[{"file_id":"file_123"}]}`,
		`{"model":"org/m","prompt":"a","images":[{"image_url":"data:image/png;base64,AA==","file_id":"file_123"}]}`,
		`{"model":"org/m","prompt":"a","images":[]}`,
		`{"model":"org/m","prompt":"a"}`,
	}
	for _, body := range cases {
		if _, err := ParseImagesEditsJSONRequest([]byte(body)); err == nil {
			t.Fatalf("expected error for %s", body)
		}
	}
}

func TestParseImagesEditsMultipartRequest(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", "Org/Model")
	_ = writer.WriteField("prompt", "edit me")
	_ = writer.WriteField("quality", "high")
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="image"; filename="a.png"`)
	partHeader.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(&buf, writer.Boundary())
	form, err := reader.ReadForm(8 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer form.RemoveAll()

	req, err := ParseImagesEditsMultipartRequest(form)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "org/model" || req.Steps != 50 {
		t.Fatalf("req = model=%q steps=%d", req.Model, req.Steps)
	}
	if len(req.Images) != 1 || !strings.HasPrefix(req.Images[0], "data:image/png;base64,") {
		t.Fatalf("images = %#v", req.Images)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.SplitN(req.Images[0], ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, png) {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestParseImagesEditsMultipartRejectsMask(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", "org/m")
	_ = writer.WriteField("prompt", "a")
	imagePart, _ := writer.CreateFormFile("image", "a.png")
	_, _ = imagePart.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	maskPart, _ := writer.CreateFormFile("mask", "mask.png")
	_, _ = maskPart.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	_ = writer.Close()
	reader := multipart.NewReader(&buf, writer.Boundary())
	form, err := reader.ReadForm(8 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer form.RemoveAll()
	if _, err := ParseImagesEditsMultipartRequest(form); err == nil {
		t.Fatal("expected mask rejection")
	}
}

func TestFormatImagesEditsResponse(t *testing.T) {
	req := ImagesEditsRequest{
		EchoSize:         "1024x1024",
		EchoQuality:      "auto",
		EchoOutputFormat: "png",
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	body, err := FormatImagesEditsResponse(99, req, [][]byte{png})
	if err != nil {
		t.Fatal(err)
	}
	var resp ImagesGenerationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Created != 99 || len(resp.Data) != 1 || resp.Size != "1024x1024" {
		t.Fatalf("response = %+v", resp)
	}
}
