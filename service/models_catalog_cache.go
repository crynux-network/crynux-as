package service

import (
	"context"
	"crynux_as/config"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// ModelsCatalogChatItem is one public chat catalog row.
type ModelsCatalogChatItem struct {
	ID                 string `json:"id"`
	Object             string `json:"object"`
	Created            int64  `json:"created"`
	OwnedBy            string `json:"owned_by"`
	MinVram            uint64 `json:"min_vram,omitempty"`
	OnDiskNodeCount    int64  `json:"on_disk_node_count,omitempty"`
	ExecutionDType     string `json:"execution_dtype,omitempty"`
	QuantizationConfig any    `json:"quantization_config,omitempty"`
}

// ModelsCatalogImageItem is one public image catalog row.
type ModelsCatalogImageItem struct {
	ModelsCatalogChatItem
	Components map[string]any `json:"components"`
	Lora       map[string]any `json:"lora"`
}

type modelsCatalogSnapshot struct {
	chat  []any
	image []any
}

var modelsCatalogCache struct {
	mu       sync.RWMutex
	snapshot modelsCatalogSnapshot
}

func newCrynuxChatCatalogItem(model LoadedLLMModel) ModelsCatalogChatItem {
	return ModelsCatalogChatItem{
		ID:                 model.ModelID,
		Object:             "model",
		Created:            0,
		OwnedBy:            "crynux",
		MinVram:            model.MinVRAM,
		OnDiskNodeCount:    model.OnDiskNodeCount,
		ExecutionDType:     model.ExecutionDType,
		QuantizationConfig: QuantizationConfigForAPI(model.QuantizationConfig),
	}
}

func newCrynuxImageCatalogItem(model LoadedLLMModel) ModelsCatalogImageItem {
	return ModelsCatalogImageItem{
		ModelsCatalogChatItem: newCrynuxChatCatalogItem(model),
		Components:            model.Components,
		Lora:                  model.Lora,
	}
}

func newProviderCatalogItem(route ProviderModelRoute) ModelsCatalogChatItem {
	return ModelsCatalogChatItem{
		ID:      route.ModelName,
		Object:  "model",
		Created: 0,
		OwnedBy: string(route.ServiceProvider),
	}
}

func providerOwnedByRank(ownedBy string) int {
	switch ownedBy {
	case "crynux":
		return 0
	case "openai":
		return 1
	case "anthropic":
		return 2
	case "google":
		return 3
	default:
		return 4
	}
}

func catalogItemID(item any) string {
	switch v := item.(type) {
	case ModelsCatalogChatItem:
		return v.ID
	case ModelsCatalogImageItem:
		return v.ID
	default:
		return ""
	}
}

func catalogItemOwnedBy(item any) string {
	switch v := item.(type) {
	case ModelsCatalogChatItem:
		return v.OwnedBy
	case ModelsCatalogImageItem:
		return v.OwnedBy
	default:
		return ""
	}
}

func sortModelsCatalogItems(items []any) {
	sort.SliceStable(items, func(i, j int) bool {
		idi, idj := catalogItemID(items[i]), catalogItemID(items[j])
		if idi != idj {
			return idi < idj
		}
		return providerOwnedByRank(catalogItemOwnedBy(items[i])) < providerOwnedByRank(catalogItemOwnedBy(items[j]))
	})
}

func appendProviderCatalogItems(items []any, routes []ProviderModelRoute, apiKind string) []any {
	wantKind := strings.TrimSpace(strings.ToLower(apiKind))
	for _, route := range routes {
		if wantKind != "" && strings.TrimSpace(strings.ToLower(route.APIKind)) != wantKind {
			continue
		}
		items = append(items, newProviderCatalogItem(route))
	}
	return items
}

// RefreshModelsCatalog rebuilds the public models API snapshot from the current
// Crynux catalog caches and visible Provider models. A failed refresh leaves the
// previous snapshot unchanged.
func RefreshModelsCatalog(ctx context.Context, db *gorm.DB) error {
	providerRoutes, err := ListVisibleProviderModels(ctx, db, "")
	if err != nil {
		return err
	}

	loadedChat := ListLoadedLLMModels()
	chat := make([]any, 0, len(loadedChat)+len(providerRoutes))
	for _, model := range loadedChat {
		chat = append(chat, newCrynuxChatCatalogItem(model))
	}
	chat = appendProviderCatalogItems(chat, providerRoutes, config.ProviderAPIKindChat)
	sortModelsCatalogItems(chat)

	loadedImage := ListLoadedSDModels()
	image := make([]any, 0, len(loadedImage)+len(providerRoutes))
	for _, model := range loadedImage {
		image = append(image, newCrynuxImageCatalogItem(model))
	}
	image = appendProviderCatalogItems(image, providerRoutes, config.ProviderAPIKindImage)
	sortModelsCatalogItems(image)

	modelsCatalogCache.mu.Lock()
	modelsCatalogCache.snapshot = modelsCatalogSnapshot{
		chat:  chat,
		image: image,
	}
	modelsCatalogCache.mu.Unlock()
	return nil
}

func copyCatalogItems(items []any) []any {
	out := make([]any, len(items))
	copy(out, items)
	return out
}

// ListModelsCatalogChat returns the cached chat catalog rows.
func ListModelsCatalogChat() []any {
	modelsCatalogCache.mu.RLock()
	defer modelsCatalogCache.mu.RUnlock()
	return copyCatalogItems(modelsCatalogCache.snapshot.chat)
}

// ListModelsCatalogImage returns the cached image catalog rows.
func ListModelsCatalogImage() []any {
	modelsCatalogCache.mu.RLock()
	defer modelsCatalogCache.mu.RUnlock()
	return copyCatalogItems(modelsCatalogCache.snapshot.image)
}

// ListModelsCatalogAll returns the cached merged chat and image catalog rows.
func ListModelsCatalogAll() []any {
	modelsCatalogCache.mu.RLock()
	defer modelsCatalogCache.mu.RUnlock()
	out := make([]any, 0, len(modelsCatalogCache.snapshot.chat)+len(modelsCatalogCache.snapshot.image))
	out = append(out, modelsCatalogCache.snapshot.chat...)
	out = append(out, modelsCatalogCache.snapshot.image...)
	sortModelsCatalogItems(out)
	return out
}

// ListModelsCatalogByID returns every cached catalog row whose id matches modelID.
func ListModelsCatalogByID(modelID string) []any {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == "" {
		return nil
	}
	modelsCatalogCache.mu.RLock()
	defer modelsCatalogCache.mu.RUnlock()
	out := make([]any, 0)
	appendMatches := func(items []any) {
		for _, item := range items {
			if strings.ToLower(catalogItemID(item)) == modelID {
				out = append(out, item)
			}
		}
	}
	appendMatches(modelsCatalogCache.snapshot.chat)
	appendMatches(modelsCatalogCache.snapshot.image)
	sortModelsCatalogItems(out)
	return out
}

// ResetModelsCatalogCacheForTest clears the public models catalog snapshot.
func ResetModelsCatalogCacheForTest() {
	modelsCatalogCache.mu.Lock()
	modelsCatalogCache.snapshot = modelsCatalogSnapshot{}
	modelsCatalogCache.mu.Unlock()
}
