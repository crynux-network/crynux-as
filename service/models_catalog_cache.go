package service

import (
	"context"
	"crynux_as/config"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
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
	all   []any
}

var modelsCatalogCache struct {
	mu       sync.RWMutex
	snapshot modelsCatalogSnapshot
}

// listVisibleProviderModels is the Provider visibility lookup used by catalog refresh.
// Tests may replace it to simulate query failures.
var listVisibleProviderModels = ListVisibleProviderModels

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

func filterProviderCatalogItems(items []any) []any {
	out := make([]any, 0)
	for _, item := range items {
		ownedBy := catalogItemOwnedBy(item)
		if ownedBy == "crynux" || ownedBy == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// RefreshModelsCatalog rebuilds the public models API snapshot from the current
// Crynux catalog caches and visible Provider models. Crynux rows are always
// rebuilt. When the Provider visibility query fails, previous Provider rows are
// reused when present; otherwise the snapshot contains Crynux rows only.
func RefreshModelsCatalog(ctx context.Context, db *gorm.DB) error {
	providerRoutes, err := listVisibleProviderModels(ctx, db, "")
	var providerChat []any
	var providerImage []any
	if err != nil {
		modelsCatalogCache.mu.RLock()
		providerChat = filterProviderCatalogItems(modelsCatalogCache.snapshot.chat)
		providerImage = filterProviderCatalogItems(modelsCatalogCache.snapshot.image)
		modelsCatalogCache.mu.RUnlock()
		if len(providerChat) > 0 || len(providerImage) > 0 {
			log.Errorf("provider visibility query failed; reusing previous provider rows: %v", err)
		} else {
			log.Errorf("provider visibility query failed; refreshing Crynux catalog rows only: %v", err)
		}
	} else {
		providerChat = appendProviderCatalogItems(nil, providerRoutes, config.ProviderAPIKindChat)
		providerImage = appendProviderCatalogItems(nil, providerRoutes, config.ProviderAPIKindImage)
	}

	loadedChat := ListLoadedLLMModels()
	chat := make([]any, 0, len(loadedChat)+len(providerChat))
	for _, model := range loadedChat {
		chat = append(chat, newCrynuxChatCatalogItem(model))
	}
	chat = append(chat, providerChat...)
	sortModelsCatalogItems(chat)

	loadedImage := ListLoadedSDModels()
	image := make([]any, 0, len(loadedImage)+len(providerImage))
	for _, model := range loadedImage {
		image = append(image, newCrynuxImageCatalogItem(model))
	}
	image = append(image, providerImage...)
	sortModelsCatalogItems(image)

	all := make([]any, 0, len(chat)+len(image))
	all = append(all, chat...)
	all = append(all, image...)
	sortModelsCatalogItems(all)

	modelsCatalogCache.mu.Lock()
	modelsCatalogCache.snapshot = modelsCatalogSnapshot{
		chat:  chat,
		image: image,
		all:   all,
	}
	modelsCatalogCache.mu.Unlock()
	log.Infof(
		"models catalog cache refreshed: %d chat, %d image, %d total",
		len(chat),
		len(image),
		len(all),
	)
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
	return copyCatalogItems(modelsCatalogCache.snapshot.all)
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
