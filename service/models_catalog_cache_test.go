package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func setupModelsCatalogTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupSharedAPIKeyLimitsTestDB(t)
	t.Cleanup(func() {
		_ = InitProviderCatalog(&config.AppConfig{})
		ResetModelsCatalogCacheForTest()
		listVisibleProviderModels = ListVisibleProviderModels
		llmModelsCache.mu.Lock()
		llmModelsCache.catalogModels = nil
		llmModelsCache.mu.Unlock()
		sdModelsCache.mu.Lock()
		sdModelsCache.catalogModels = nil
		sdModelsCache.mu.Unlock()
	})
	return db
}

func setCatalogLLMModels(models map[string]LoadedLLMModel) {
	llmModelsCache.mu.Lock()
	llmModelsCache.catalogModels = models
	llmModelsCache.mu.Unlock()
}

func setCatalogSDModels(models map[string]LoadedLLMModel) {
	sdModelsCache.mu.Lock()
	sdModelsCache.catalogModels = models
	sdModelsCache.mu.Unlock()
}

func initOpenAICatalogProviders(t *testing.T) {
	t.Helper()
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
}

func TestRefreshModelsCatalogUsesVisibleProvidersAndSorts(t *testing.T) {
	db := setupModelsCatalogTest(t)
	user := models.User{Address: "0xcatalogcache1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog_cache")
	initOpenAICatalogProviders(t)

	setCatalogLLMModels(map[string]LoadedLLMModel{
		"z-model|bf16": {
			ModelID:         "z-org/z-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
		},
	})
	setCatalogSDModels(map[string]LoadedLLMModel{
		"a-image|bf16": {
			ModelID:         "a-org/a-image",
			MinVRAM:         12,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
			Components:      map[string]any{},
			Lora:            nil,
		},
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
	if catalogItemID(all[0]) != "a-org/a-image" {
		t.Fatalf("all[0]=%v", all[0])
	}
	if catalogItemID(all[len(all)-1]) != "z-org/z-model" {
		t.Fatalf("all[last]=%v", all[len(all)-1])
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

func TestRefreshModelsCatalogReusesProviderRowsOnVisibilityFailure(t *testing.T) {
	db := setupModelsCatalogTest(t)
	user := models.User{Address: "0xcatalogcache2"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog_reuse")
	initOpenAICatalogProviders(t)

	setCatalogLLMModels(map[string]LoadedLLMModel{
		"old|bf16": {
			ModelID:         "old-org/old-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
		},
	})
	setCatalogSDModels(map[string]LoadedLLMModel{})
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	setCatalogLLMModels(map[string]LoadedLLMModel{
		"new|bf16": {
			ModelID:         "new-org/new-model",
			MinVRAM:         16,
			OnDiskNodeCount: 4,
			ExecutionDType:  "bfloat16",
		},
	})
	listVisibleProviderModels = func(context.Context, *gorm.DB, string) ([]ProviderModelRoute, error) {
		return nil, errors.New("db down")
	}
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	chat := ListModelsCatalogChat()
	if len(chat) != 2 {
		t.Fatalf("chat len=%d data=%v", len(chat), chat)
	}
	foundCrynux, foundProvider := false, false
	for _, item := range chat {
		row, ok := item.(ModelsCatalogChatItem)
		if !ok {
			t.Fatalf("unexpected chat item %T", item)
		}
		switch {
		case row.ID == "new-org/new-model" && row.OwnedBy == "crynux":
			foundCrynux = true
		case row.ID == "gpt-6-astra" && row.OwnedBy == "openai":
			foundProvider = true
		case row.ID == "old-org/old-model":
			t.Fatalf("stale Crynux row still present: %v", row)
		}
	}
	if !foundCrynux || !foundProvider {
		t.Fatalf("expected updated Crynux and reused Provider rows, chat=%v", chat)
	}
}

func TestRefreshModelsCatalogFirstFailureKeepsCrynuxOnly(t *testing.T) {
	db := setupModelsCatalogTest(t)
	initOpenAICatalogProviders(t)
	setCatalogLLMModels(map[string]LoadedLLMModel{
		"only|bf16": {
			ModelID:         "only-org/only-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
		},
	})
	setCatalogSDModels(map[string]LoadedLLMModel{})
	listVisibleProviderModels = func(context.Context, *gorm.DB, string) ([]ProviderModelRoute, error) {
		return nil, errors.New("db down")
	}
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	chat := ListModelsCatalogChat()
	if len(chat) != 1 {
		t.Fatalf("chat len=%d data=%v", len(chat), chat)
	}
	row, ok := chat[0].(ModelsCatalogChatItem)
	if !ok || row.ID != "only-org/only-model" || row.OwnedBy != "crynux" {
		t.Fatalf("chat[0]=%v", chat[0])
	}
	if len(ListModelsCatalogImage()) != 0 {
		t.Fatalf("image should be empty")
	}
	if len(ListModelsCatalogAll()) != 1 {
		t.Fatalf("all len=%d", len(ListModelsCatalogAll()))
	}
}

func TestRefreshModelsCatalogPreservesMultiDtypeOrder(t *testing.T) {
	db := setupModelsCatalogTest(t)
	if err := InitProviderCatalog(&config.AppConfig{}); err != nil {
		t.Fatal(err)
	}
	setCatalogLLMModels(map[string]LoadedLLMModel{
		"same|float16": {
			ModelID:         "same-org/same-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "float16",
			ModelCacheHash:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		"same|bfloat16": {
			ModelID:         "same-org/same-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
			ModelCacheHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	})
	setCatalogSDModels(map[string]LoadedLLMModel{})
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	chat := ListModelsCatalogChat()
	if len(chat) != 2 {
		t.Fatalf("chat len=%d data=%v", len(chat), chat)
	}
	first, ok := chat[0].(ModelsCatalogChatItem)
	if !ok || first.ExecutionDType != "bfloat16" {
		t.Fatalf("chat[0]=%v", chat[0])
	}
	second, ok := chat[1].(ModelsCatalogChatItem)
	if !ok || second.ExecutionDType != "float16" {
		t.Fatalf("chat[1]=%v", chat[1])
	}
}

func TestListModelsCatalogByIDReturnsCrynuxAndProvider(t *testing.T) {
	db := setupModelsCatalogTest(t)
	user := models.User{Address: "0xcatalogcache3"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog_byid")
	if err := InitProviderCatalog(&config.AppConfig{
		Provider: config.ProviderConfig{
			Services: map[string]config.ProviderServiceConfig{
				"openai": {
					DisplayName: "OpenAI",
					Models: []config.ProviderModelConfig{
						{Name: "shared-model", APIKind: config.ProviderAPIKindChat, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	setCatalogLLMModels(map[string]LoadedLLMModel{
		"shared|bf16": {
			ModelID:         "shared-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
		},
	})
	setCatalogSDModels(map[string]LoadedLLMModel{})
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	byID := ListModelsCatalogByID("SHARED-MODEL")
	if len(byID) != 2 {
		t.Fatalf("byID=%v", byID)
	}
	first, ok := byID[0].(ModelsCatalogChatItem)
	if !ok || first.OwnedBy != "crynux" {
		t.Fatalf("byID[0]=%v", byID[0])
	}
	second, ok := byID[1].(ModelsCatalogChatItem)
	if !ok || second.OwnedBy != "openai" {
		t.Fatalf("byID[1]=%v", byID[1])
	}
}

func TestRefreshModelsCatalogRequiresEnabledProviderKey(t *testing.T) {
	db := setupModelsCatalogTest(t)
	initOpenAICatalogProviders(t)
	setCatalogLLMModels(map[string]LoadedLLMModel{})
	setCatalogSDModels(map[string]LoadedLLMModel{})

	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if len(ListModelsCatalogChat()) != 0 || len(ListModelsCatalogImage()) != 0 {
		t.Fatalf("provider models visible without enabled key")
	}

	user := models.User{Address: "0xcatalogcache4"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog_enable")
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	chat := ListModelsCatalogChat()
	image := ListModelsCatalogImage()
	if len(chat) != 1 || len(image) != 1 {
		t.Fatalf("chat=%v image=%v", chat, image)
	}
}

func TestRefreshModelsCatalogSerializesConcurrentRefreshes(t *testing.T) {
	db := setupModelsCatalogTest(t)
	initOpenAICatalogProviders(t)
	setCatalogLLMModels(map[string]LoadedLLMModel{})
	setCatalogSDModels(map[string]LoadedLLMModel{})

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	listVisibleProviderModels = func(context.Context, *gorm.DB, string) ([]ProviderModelRoute, error) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := maxInFlight.Load()
			if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return nil, nil
	}

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- RefreshModelsCatalog(context.Background(), db)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("expected serialized provider lookups, maxInFlight=%d", got)
	}
}

func TestRefreshModelsCatalogFailedRefreshDoesNotRestoreStaleProviders(t *testing.T) {
	db := setupModelsCatalogTest(t)
	user := models.User{Address: "0xcatalogcache5"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	_ = createTestSharedAPIKey(t, db, user.ID, "key_openai_catalog_race")
	initOpenAICatalogProviders(t)
	setCatalogLLMModels(map[string]LoadedLLMModel{
		"race|bf16": {
			ModelID:         "race-org/race-model",
			MinVRAM:         8,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
		},
	})
	setCatalogSDModels(map[string]LoadedLLMModel{})
	if err := RefreshModelsCatalog(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if len(filterProviderCatalogItems(ListModelsCatalogChat())) == 0 {
		t.Fatal("expected seeded provider rows")
	}

	entered := make(chan struct{})
	releaseFail := make(chan struct{})
	var calls atomic.Int32
	listVisibleProviderModels = func(context.Context, *gorm.DB, string) ([]ProviderModelRoute, error) {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-releaseFail
			return nil, errors.New("db down")
		}
		return nil, nil
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := RefreshModelsCatalog(context.Background(), db); err != nil {
			t.Errorf("first refresh: %v", err)
		}
	}()
	<-entered
	go func() {
		defer wg.Done()
		if err := RefreshModelsCatalog(context.Background(), db); err != nil {
			t.Errorf("second refresh: %v", err)
		}
	}()
	time.Sleep(30 * time.Millisecond)
	close(releaseFail)
	wg.Wait()

	if got := filterProviderCatalogItems(ListModelsCatalogChat()); len(got) != 0 {
		t.Fatalf("stale provider rows restored after successful empty visibility refresh: %v", got)
	}
	chat := ListModelsCatalogChat()
	if len(chat) != 1 {
		t.Fatalf("chat=%v", chat)
	}
	row, ok := chat[0].(ModelsCatalogChatItem)
	if !ok || row.ID != "race-org/race-model" || row.OwnedBy != "crynux" {
		t.Fatalf("chat[0]=%v", chat[0])
	}
}
