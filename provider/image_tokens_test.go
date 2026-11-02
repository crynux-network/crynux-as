package provider

import (
	"crynux_as/models"
	"strings"
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

func TestEstimateImageInputTokensOpenAIPatchFamilies(t *testing.T) {
	img1024 := CanonicalImageInfo{Width: 1024, Height: 1024, Detail: "high"}
	// gpt-5.2 / gpt-4.1-mini: 2048 max edge, 6144 budget, no resize for 1024^2 -> 32*32=1024 patches
	got, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-5.2", []CanonicalImageInfo{img1024})
	if err != nil {
		t.Fatal(err)
	}
	if got != 1229 { // ceil(1024 * 1.2)
		t.Fatalf("gpt-5.2 tokens=%d", got)
	}

	mini, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-4.1-mini", []CanonicalImageInfo{{
		Width: 3000, Height: 3000, Detail: "high",
	}})
	if err != nil {
		t.Fatal(err)
	}
	// scale to 2048, patches ceil(2048/32)^2=64^2=4096, under 6144, *1.62 -> ceil 6636
	if mini != 6636 {
		t.Fatalf("gpt-4.1-mini large tokens=%d", mini)
	}

	astra, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-6-astra", []CanonicalImageInfo{img1024})
	if err != nil {
		t.Fatal(err)
	}
	if astra != 1229 {
		t.Fatalf("gpt-6-astra high tokens=%d", astra)
	}

	for _, model := range []string{"gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-luna"} {
		tokens, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, model, []CanonicalImageInfo{img1024})
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		if tokens != 1229 {
			t.Fatalf("%s tokens=%d", model, tokens)
		}
	}

	astra2048, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-6-astra", []CanonicalImageInfo{{
		Width: 2048, Height: 2048, Detail: "high",
	}})
	if err != nil {
		t.Fatal(err)
	}
	// 4096 patches -> budget 2500 -> ceil(2500*1.2)=3000
	if astra2048 != 3000 {
		t.Fatalf("gpt-6-astra 2048 tokens=%d", astra2048)
	}
}

func TestEstimateImageInputTokensOpenAIDetailLevels(t *testing.T) {
	img := CanonicalImageInfo{Width: 1024, Height: 1024}

	low, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-6-astra", []CanonicalImageInfo{{
		Width: img.Width, Height: img.Height, Detail: "low",
	}})
	if err != nil {
		t.Fatal(err)
	}
	// low fits in 512 -> 16*16=256 patches * 1.2 -> 308
	if low != 308 {
		t.Fatalf("gpt-6-astra low tokens=%d", low)
	}

	auto, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-6-astra", []CanonicalImageInfo{{
		Width: img.Width, Height: img.Height, Detail: "",
	}})
	if err != nil {
		t.Fatal(err)
	}
	original, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-6-astra", []CanonicalImageInfo{{
		Width: img.Width, Height: img.Height, Detail: "original",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if auto != 1229 || original != 1229 {
		t.Fatalf("auto=%d original=%d", auto, original)
	}

	if _, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-5.2", []CanonicalImageInfo{{
		Width: 64, Height: 64, Detail: "original",
	}}); err == nil || !strings.Contains(err.Error(), "detail") {
		t.Fatalf("gpt-5.2 must reject original, got %v", err)
	}
	if _, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-4o", []CanonicalImageInfo{{
		Width: 64, Height: 64, Detail: "original",
	}}); err == nil || !strings.Contains(err.Error(), "detail") {
		t.Fatalf("gpt-4o must reject original, got %v", err)
	}
}

func TestEstimateImageInputTokensOpenAIRejectsOverPatchLimit(t *testing.T) {
	// 65536x65536 after edge clamp to 65535 still exceeds 30000 patches for original.
	_, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-6-astra", []CanonicalImageInfo{{
		Width: 65536, Height: 65536, Detail: "original",
	}})
	if err == nil || !strings.Contains(err.Error(), "patch") {
		t.Fatalf("expected patch limit error, got %v", err)
	}
}

func TestEstimateImageInputTokensOpenAIModelNameIsolation(t *testing.T) {
	img := CanonicalImageInfo{Width: 1024, Height: 1024, Detail: "high"}

	gpt5, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-5", []CanonicalImageInfo{img})
	if err != nil {
		t.Fatal(err)
	}
	// tile: short side 768 -> 2x2 * 140 + 70 = 630
	if gpt5 != 630 {
		t.Fatalf("gpt-5 tokens=%d", gpt5)
	}

	gpt51, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-5.1", []CanonicalImageInfo{img})
	if err != nil {
		t.Fatal(err)
	}
	if gpt51 != 630 {
		t.Fatalf("gpt-5.1 tokens=%d", gpt51)
	}

	gpt52, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-5.2", []CanonicalImageInfo{img})
	if err != nil {
		t.Fatal(err)
	}
	if gpt52 != 1229 {
		t.Fatalf("gpt-5.2 tokens=%d", gpt52)
	}

	gpt54, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-5.4", []CanonicalImageInfo{img})
	if err != nil {
		t.Fatal(err)
	}
	if gpt54 != 1229 {
		t.Fatalf("gpt-5.4 tokens=%d", gpt54)
	}

	dated, err := EstimateImageInputTokens(models.ServiceProviderOpenAI, "gpt-4.1-mini-2025-04-14", []CanonicalImageInfo{{
		Width: 32, Height: 32,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if dated != 2 {
		t.Fatalf("dated gpt-4.1-mini tokens=%d", dated)
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
