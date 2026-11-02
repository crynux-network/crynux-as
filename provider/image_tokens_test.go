package provider

import (
	"crynux_as/models"
	"testing"
)

func TestEstimateImageInputTokensOpenAITileAndPatch(t *testing.T) {
	tileImage := CanonicalImageInfo{MIMEType: "image/png", Width: 1024, Height: 1024, Detail: "high"}
	tokens, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-4o", []CanonicalImageInfo{tileImage})
	if err != nil {
		t.Fatal(err)
	}
	// 1024 -> short side 768 -> 2x2 tiles * 170 + 85 = 765
	if tokens != 765 {
		t.Fatalf("gpt-4o high tokens=%d", tokens)
	}

	low, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-4o", []CanonicalImageInfo{{
		Width: 4096, Height: 4096, Detail: "low",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if low != 85 {
		t.Fatalf("gpt-4o low tokens=%d", low)
	}

	patch, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-4.1-mini", []CanonicalImageInfo{{
		Width: 32, Height: 32,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if patch != 2 { // 1 patch * 1.62 -> ceil 2
		t.Fatalf("gpt-4.1-mini tokens=%d", patch)
	}
}

func TestEstimateImageInputTokensAnthropicAndGoogle(t *testing.T) {
	img := CanonicalImageInfo{Width: 1000, Height: 1000}
	anthropic, err := EstimateImageInputTokens(models.ServiceProviderAnthropic, "claude-haiku-5-5", []CanonicalImageInfo{img})
	if err != nil {
		t.Fatal(err)
	}
	// ceil(1000/28)^2 = 36*36 = 1296
	if anthropic != 1296 {
		t.Fatalf("anthropic tokens=%d", anthropic)
	}

	google3, err := EstimateImageInputTokens(models.ServiceProviderGoogle, "gemini-3.8-flash", []CanonicalImageInfo{img})
	if err != nil {
		t.Fatal(err)
	}
	if google3 != 1120 {
		t.Fatalf("gemini-3 tokens=%d", google3)
	}

	googleLegacy, err := EstimateImageInputTokens(models.ServiceProviderGoogle, "gemini-2.0-flash", []CanonicalImageInfo{{
		Width: 200, Height: 200,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if googleLegacy != 258 {
		t.Fatalf("gemini-2 tokens=%d", googleLegacy)
	}
}

func TestEstimateImageInputTokensUnknownModelWithImagesFails(t *testing.T) {
	_, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "unknown-vision-model", []CanonicalImageInfo{{
		Width: 1, Height: 1,
	}})
	if err == nil {
		t.Fatal("expected unknown model error")
	}
	tokens, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "unknown-vision-model", nil)
	if err != nil || tokens != 0 {
		t.Fatalf("text-only estimate tokens=%d err=%v", tokens, err)
	}
}
