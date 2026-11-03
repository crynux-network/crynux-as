package provider

import (
	"crynux_as/models"
	"mime/multipart"
	"strings"
	"testing"
)

func TestEstimateProviderImagesEditInputTokensOpenAI(t *testing.T) {
	tokens, err := EstimateProviderImagesEditInputTokens(
		models.ServiceProviderOpenAI,
		"gpt-image-2.5-sunburst",
		[]CanonicalImageInfo{{Width: 1024, Height: 1024}},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if tokens != 194 {
		t.Fatalf("tokens=%d, want 194", tokens)
	}
	high, err := EstimateProviderImagesEditInputTokens(
		models.ServiceProviderOpenAI,
		"gpt-image-2.5-flare",
		[]CanonicalImageInfo{{Width: 1024, Height: 1024}},
		"high",
	)
	if err != nil {
		t.Fatal(err)
	}
	if high != 194+4160 {
		t.Fatalf("high=%d", high)
	}
}

func TestEstimateProviderImagesEditInputTokensGoogle(t *testing.T) {
	tokens, err := EstimateProviderImagesEditInputTokens(
		models.ServiceProviderGoogle,
		"gemini-nano-banana-2.1",
		[]CanonicalImageInfo{{Width: 1024, Height: 1024}, {Width: 512, Height: 512}},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if tokens != 2240 {
		t.Fatalf("tokens=%d, want 2240", tokens)
	}
}

func TestEstimateProviderImagesEditInputTokensRejectsChatModel(t *testing.T) {
	_, err := EstimateProviderImagesEditInputTokens(
		models.ServiceProviderOpenAI,
		"gpt-6-astra",
		[]CanonicalImageInfo{{Width: 1024, Height: 1024}},
		"",
	)
	if err == nil {
		t.Fatal("expected error for chat model")
	}
}

func TestParseCanonicalImagesMultipartNumericFields(t *testing.T) {
	form := &multipart.Form{
		Value: map[string][]string{
			"model":              {"gpt-image-2.5-sunburst"},
			"prompt":             {"edit me"},
			"n":                  {"2"},
			"output_compression": {"80"},
			"queue_timeout":      {"30"},
			"output_format":      {"jpeg"},
		},
		File: map[string][]*multipart.FileHeader{},
	}
	// Without files edits fail after numeric parse; generations not used with files.
	// Build a form for generations-like values through map path by calling edits with image file absent.
	_, err := ParseCanonicalImagesMultipart(form, models.TaskAPITypeImagesEdits)
	if err == nil || !strings.Contains(err.Error(), "images is required") {
		// If numeric parse failed first, surface that.
		if err != nil && strings.Contains(err.Error(), "must be an integer") {
			t.Fatalf("numeric multipart fields rejected: %v", err)
		}
		if err == nil {
			t.Fatal("expected images required after numeric parse")
		}
	}

	rawForm := &multipart.Form{
		Value: map[string][]string{
			"model":  {"gpt-image-2.5-sunburst"},
			"prompt": {"hi"},
			"n":      {"2"},
		},
	}
	req, err := ParseCanonicalImagesMultipart(rawForm, models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if req.N != 2 {
		t.Fatalf("n=%d", req.N)
	}
}

func TestParseCanonicalImagesMultipartRejectsCrynuxField(t *testing.T) {
	form := &multipart.Form{
		Value: map[string][]string{
			"model":  {"gpt-image-2.5-sunburst"},
			"prompt": {"hi"},
			"steps":  {"20"},
		},
	}
	req, err := ParseCanonicalImagesMultipart(form, models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectCrynuxAndUnknown(req); err == nil || !strings.Contains(err.Error(), "steps") {
		t.Fatalf("err=%v", err)
	}
}

func TestGoogleImagesConverterRejectsNGreaterThanOne(t *testing.T) {
	req, err := ParseCanonicalImagesJSON([]byte(`{
		"model":"gemini-nano-banana-2.1",
		"prompt":"a cat",
		"n":2
	}`), models.TaskAPITypeImagesGenerations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAndBuildGoogleImagesRequest(req); err == nil || !strings.Contains(err.Error(), "n must be 1") {
		t.Fatalf("err=%v", err)
	}
}
