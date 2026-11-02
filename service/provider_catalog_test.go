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
				{Name: "  GPT-4o  ", UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
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
						{Name: "gpt-4o", UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
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
