package provider

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIImagesConverterPassThroughBackground(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gpt-image-2.5-sunburst",
		"prompt":"a cat",
		"size":"1024x1024",
		"background":"transparent",
		"output_format":"png"
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	body, err := ValidateAndBuildOpenAIImagesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["background"] != "transparent" {
		t.Fatalf("background=%v", parsed["background"])
	}
}

func TestOpenAIImagesConverterRejectsTransparentJPEG(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gpt-image-2.5-sunburst",
		"prompt":"a cat",
		"background":"transparent",
		"output_format":"jpeg"
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAndBuildOpenAIImagesRequest(req); err == nil {
		t.Fatal("expected error")
	}
}

func TestGoogleImagesConverterRejectsBackground(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gemini-nano-banana-2.1",
		"prompt":"a cat",
		"background":"transparent"
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAndBuildGoogleImagesRequest(req); err == nil || !strings.Contains(err.Error(), "background") {
		t.Fatalf("err=%v", err)
	}
}

func TestGoogleImagesConverterMapsSize(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gemini-nano-banana-2.1",
		"prompt":"a cat",
		"size":"1536x1024",
		"output_format":"png"
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	built, err := ValidateAndBuildGoogleImagesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if built.AspectRatio == "" || built.ImageSize == "" {
		t.Fatalf("aspect=%q size=%q", built.AspectRatio, built.ImageSize)
	}
	var parsed map[string]any
	if err := json.Unmarshal(built.Body, &parsed); err != nil {
		t.Fatal(err)
	}
	gen := parsed["generationConfig"].(map[string]any)
	modalities := gen["responseModalities"].([]any)
	if len(modalities) != 1 || modalities[0] != "IMAGE" {
		t.Fatalf("modalities=%v", modalities)
	}
}

func TestEstimateGoogleImageOutputTokens(t *testing.T) {
	if got := EstimateGoogleImageOutputTokens("1K"); got != 1120 {
		t.Fatalf("got %d", got)
	}
	if got := EstimateProviderImageOutputTokens("google", "m", "", 1024, 1024, 2); got != 2240 {
		t.Fatalf("got %d", got)
	}
}

func TestGoogleImagesConverterRejectsQuality(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gemini-nano-banana-2.1",
		"prompt":"a cat",
		"quality":"high"
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAndBuildGoogleImagesRequest(req); err == nil || !strings.Contains(err.Error(), "quality") {
		t.Fatalf("err=%v", err)
	}
}

func TestGoogleImagesConverterRejectsCompressionWithoutLossyFormat(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gemini-nano-banana-2.1",
		"prompt":"a cat",
		"output_format":"png",
		"output_compression":80
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAndBuildGoogleImagesRequest(req); err == nil || !strings.Contains(err.Error(), "output_compression") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenAIImagesConverterPassThroughMask(t *testing.T) {
	bodyJSON := []byte(`{
		"model":"gpt-image-2.5-sunburst",
		"prompt":"a cat",
		"images":[{"image_url":"data:image/png;base64,` + testPNG1B64 + `"}],
		"mask":{"image_url":"data:image/png;base64,` + testPNG1B64 + `"},
		"input_fidelity":"high"
	}`)
	req, err := ParseCanonicalImagesJSON(bodyJSON, models.TaskAPITypeImagesEdits)
	if err != nil {
		t.Fatal(err)
	}
	body, err := ValidateAndBuildOpenAIImagesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["input_fidelity"] != "high" {
		t.Fatalf("input_fidelity=%v", parsed["input_fidelity"])
	}
	if parsed["mask"] == nil {
		t.Fatal("mask missing")
	}
}
