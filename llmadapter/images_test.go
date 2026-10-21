package llmadapter

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseImagesGenerationsRequestDefaults(t *testing.T) {
	req, err := ParseImagesGenerationsRequest([]byte(`{
		"model": "Qwen/Qwen-Image-2.1",
		"prompt": "a cat"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "qwen/qwen-image-2.1" {
		t.Fatalf("model = %q", req.Model)
	}
	if req.N != 1 || req.Width != 1024 || req.Height != 1024 || req.Steps != 40 {
		t.Fatalf("defaults = n=%d size=%dx%d steps=%d", req.N, req.Width, req.Height, req.Steps)
	}
	if req.EchoSize != "1024x1024" || req.EchoQuality != "auto" || req.EchoOutputFormat != "png" {
		t.Fatalf("echo = %q %q %q", req.EchoSize, req.EchoQuality, req.EchoOutputFormat)
	}
}

func TestParseImagesGenerationsRequestQualityAndSize(t *testing.T) {
	req, err := ParseImagesGenerationsRequest([]byte(`{
		"model": "org/model",
		"prompt": "hi",
		"n": 2,
		"size": "512x512",
		"quality": "high"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.N != 2 || req.Width != 512 || req.Height != 512 || req.Steps != 50 {
		t.Fatalf("mapped = n=%d size=%dx%d steps=%d", req.N, req.Width, req.Height, req.Steps)
	}
}

func TestParseImagesGenerationsRequestRejectsUnsupported(t *testing.T) {
	cases := []string{
		`{"model":"org/m","prompt":"a","steps":20}`,
		`{"model":"org/m","prompt":"a","stream":true}`,
		`{"model":"org/m","prompt":"a","response_format":"url"}`,
		`{"model":"org/m","prompt":"a","output_format":"jpeg"}`,
		`{"model":"org/m","prompt":"a","background":"auto"}`,
		`{"model":"org/m","prompt":"a","partial_images":1}`,
		`{"model":"org/m","prompt":"a","size":"100x100"}`,
	}
	for _, body := range cases {
		if _, err := ParseImagesGenerationsRequest([]byte(body)); err == nil {
			t.Fatalf("expected error for %s", body)
		}
	}
}

func TestParseImagesGenerationsRequestArbitrarySize(t *testing.T) {
	req, err := ParseImagesGenerationsRequest([]byte(`{
		"model": "org/model",
		"prompt": "hi",
		"size": "1536x864"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Width != 1536 || req.Height != 864 || req.EchoSize != "1536x864" {
		t.Fatalf("size = %dx%d echo=%q", req.Width, req.Height, req.EchoSize)
	}
}

func TestBuildAndFormatImagesGenerations(t *testing.T) {
	req, err := ParseImagesGenerationsRequest([]byte(`{
		"model": "org/model",
		"prompt": "hi",
		"n": 1,
		"quality": "low"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	taskArgs, err := BuildImagesGenerationsTaskArgs(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(taskArgs, `"steps":20`) || !strings.Contains(taskArgs, `"num_images_per_prompt":1`) {
		t.Fatalf("task args = %s", taskArgs)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	body, err := FormatImagesGenerationsResponse(123, req, [][]byte{png})
	if err != nil {
		t.Fatal(err)
	}
	var resp ImagesGenerationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Created != 123 || len(resp.Data) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.Data[0].B64JSON)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(png) {
		t.Fatalf("decoded = %v", decoded)
	}
}
