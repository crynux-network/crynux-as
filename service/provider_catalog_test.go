package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitProviderCatalogLowercasesModelNames(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.Provider.Services = map[string]config.ProviderServiceConfig{
		"openai": {
			DisplayName: "OpenAI",
			Models: []config.ProviderModelConfig{
				{Name: "  GPT-4o  ", APIKind: config.ProviderAPIKindChat, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
			},
		},
	}
	if err := InitProviderCatalog(cfg); err != nil {
		t.Fatalf("InitProviderCatalog: %v", err)
	}
	t.Cleanup(func() {
		_ = InitProviderCatalog(&config.AppConfig{})
	})

	route, ok := LookupProviderModel("GPT-4o")
	if !ok {
		t.Fatal("Lookup with mixed case failed")
	}
	if route.ModelName != "gpt-4o" {
		t.Fatalf("ModelName=%q", route.ModelName)
	}
	if route.ServiceProvider != models.ServiceProviderOpenAI {
		t.Fatalf("ServiceProvider=%q", route.ServiceProvider)
	}
	if _, ok := LookupProviderModel("gpt-4o"); !ok {
		t.Fatal("lookup with lowercase failed")
	}
}

func TestValidateProviderCrynuxCollisions(t *testing.T) {
	if err := InitProviderCatalog(&config.AppConfig{
		Provider: config.ProviderConfig{
			Services: map[string]config.ProviderServiceConfig{
				"openai": {
					DisplayName: "OpenAI",
					Models: []config.ProviderModelConfig{
						{Name: "gpt-4o", APIKind: config.ProviderAPIKindChat, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("InitProviderCatalog: %v", err)
	}
	t.Cleanup(func() {
		_ = InitProviderCatalog(&config.AppConfig{})
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "success",
			"data": []map[string]any{
				testLLMRow("collide", "GPT-4o", "bfloat16", nil, 8, 1, 1),
				testLLMRow("other", "org/other", "bfloat16", nil, 8, 1, 1),
			},
		})
	}))
	t.Cleanup(server.Close)
	InitLoadedModelsCache(relay.NewClient(server.URL))

	err := ValidateProviderCrynuxCollisions(context.Background())
	if err == nil {
		t.Fatal("expected collision error")
	}
	if errors.Is(err, ErrLoadedModelsFetchFailed) {
		t.Fatalf("unexpected fetch failure wrap: %v", err)
	}
	if got := err.Error(); got != "provider model name(s) collide with Crynux catalog: gpt-4o" {
		t.Fatalf("error=%q", got)
	}
}

func TestValidateProviderCrynuxCollisionsFetchFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	InitLoadedModelsCache(relay.NewClient(server.URL))

	err := ValidateProviderCrynuxCollisions(context.Background())
	if !errors.Is(err, ErrLoadedModelsFetchFailed) {
		t.Fatalf("err=%v", err)
	}
}

func TestListVisibleProviderModelsFiltersByAPIKindAndEnabledKey(t *testing.T) {
	db := setupSharedAPIKeyLimitsTestDB(t)
	user := models.User{Address: "0xcatalog1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog")

	if err := InitProviderCatalog(&config.AppConfig{
		Provider: config.ProviderConfig{
			Services: map[string]config.ProviderServiceConfig{
				"openai": {
					DisplayName: "OpenAI",
					Models: []config.ProviderModelConfig{
						{Name: "gpt-6-astra", APIKind: config.ProviderAPIKindChat, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1", ReasoningTokens: "1"}},
						{Name: "gpt-image-2.5-sunburst", APIKind: config.ProviderAPIKindImage, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", ImageInputTokens: "1", ImageOutputTokens: "1"}},
					},
				},
				"google": {
					DisplayName: "Google",
					Models: []config.ProviderModelConfig{
						{Name: "gemini-3.1-flash-image", APIKind: config.ProviderAPIKindImage, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", ImageInputTokens: "1", ImageOutputTokens: "1", ReasoningTokens: "1"}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = InitProviderCatalog(&config.AppConfig{})
	})

	chat, err := ListVisibleProviderModels(context.Background(), db, config.ProviderAPIKindChat)
	if err != nil {
		t.Fatal(err)
	}
	if len(chat) != 1 || chat[0].ModelName != "gpt-6-astra" {
		t.Fatalf("chat=%v", chat)
	}

	image, err := ListVisibleProviderModels(context.Background(), db, config.ProviderAPIKindImage)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != 1 || image[0].ModelName != "gpt-image-2.5-sunburst" {
		t.Fatalf("image=%v", image)
	}
}
