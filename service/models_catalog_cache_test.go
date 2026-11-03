package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestRefreshModelsCatalogUsesVisibleProvidersAndSorts(t *testing.T) {
	db := setupSharedAPIKeyLimitsTestDB(t)
	user := models.User{Address: "0xcatalogcache1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog_cache")

	if err := InitProviderCatalog(&config.AppConfig{
		Provider: config.ProviderConfig{
			Services: map[string]config.ProviderServiceConfig{
				"openai": {
					DisplayName: "OpenAI",
					Models: []config.ProviderModelConfig{
						{Name: "gpt-6-astra", APIKind: config.ProviderAPIKindChat, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
						{Name: "gpt-image-2.5-sunburst", APIKind: config.ProviderAPIKindImage, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", ImageInputTokens: "1", ImageOutputTokens: "1"}},
					},
				},
				"google": {
					DisplayName: "Google",
					Models: []config.ProviderModelConfig{
						{Name: "gemini-3.1-flash-image", APIKind: config.ProviderAPIKindImage, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", ImageInputTokens: "1", ImageOutputTokens: "1"}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = InitProviderCatalog(&config.AppConfig{})
		ResetModelsCatalogCacheForTest()
	})

	llmModelsCache.mu.Lock()
	llmModelsCache.catalogModels = map[string]LoadedLLMModel{
		"z-model|bf16": {
			ModelID:         "z-org/z-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
		},
	}
	llmModelsCache.mu.Unlock()
	sdModelsCache.mu.Lock()
	sdModelsCache.catalogModels = map[string]LoadedLLMModel{
		"a-image|bf16": {
			ModelID:         "a-org/a-image",
			MinVRAM:         12,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
			Components:      map[string]any{},
			Lora:            nil,
		},
	}
	sdModelsCache.mu.Unlock()
	t.Cleanup(func() {
		llmModelsCache.mu.Lock()
		llmModelsCache.catalogModels = nil
		llmModelsCache.mu.Unlock()
		sdModelsCache.mu.Lock()
		sdModelsCache.catalogModels = nil
		sdModelsCache.mu.Unlock()
	})

	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	chat := ListModelsCatalogChat()
	if len(chat) != 2 {
		t.Fatalf("chat len=%d data=%v", len(chat), chat)
	}
	firstChat, ok := chat[0].(ModelsCatalogChatItem)
	if !ok || firstChat.ID != "gpt-6-astra" {
		t.Fatalf("chat[0]=%v", chat[0])
	}
	secondChat, ok := chat[1].(ModelsCatalogChatItem)
	if !ok || secondChat.ID != "z-org/z-model" || secondChat.OwnedBy != "crynux" {
		t.Fatalf("chat[1]=%v", chat[1])
	}

	image := ListModelsCatalogImage()
	if len(image) != 2 {
		t.Fatalf("image len=%d data=%v", len(image), image)
	}
	firstImage, ok := image[0].(ModelsCatalogImageItem)
	if !ok || firstImage.ID != "a-org/a-image" {
		t.Fatalf("image[0]=%v", image[0])
	}
	secondImage, ok := image[1].(ModelsCatalogChatItem)
	if !ok || secondImage.ID != "gpt-image-2.5-sunburst" {
		t.Fatalf("image[1]=%v", image[1])
	}

	all := ListModelsCatalogAll()
	if len(all) != 4 {
		t.Fatalf("all len=%d", len(all))
	}
	byID := ListModelsCatalogByID("GPT-6-ASTRA")
	if len(byID) != 1 {
		t.Fatalf("byID=%v", byID)
	}

	raw, err := json.Marshal(firstImage)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["model_cache_hash"]; ok {
		t.Fatalf("image row must not include model_cache_hash: %v", decoded)
	}
	if _, ok := decoded["components"]; !ok {
		t.Fatalf("image row missing components: %v", decoded)
	}
}
