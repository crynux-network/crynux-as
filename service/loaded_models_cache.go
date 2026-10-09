package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/relay"
	"errors"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const loadedModelTypeLLM = "llm"
const loadedModelTypeSD = "sd"

type LoadedLLMModel struct {
	ModelID         string
	Variant         string
	MinVRAM         uint64
	NodeCount       int64
	OnDiskNodeCount int64
}

type loadedModelsCache struct {
	mu            sync.RWMutex
	client        *relay.Client
	models        map[string]LoadedLLMModel
	catalogModels map[string]LoadedLLMModel
}

var llmModelsCache = &loadedModelsCache{
	models:        map[string]LoadedLLMModel{},
	catalogModels: map[string]LoadedLLMModel{},
}

var sdModelsCache = &loadedModelsCache{
	models:        map[string]LoadedLLMModel{},
	catalogModels: map[string]LoadedLLMModel{},
}

func sdCacheKey(modelID, variant string) string {
	return strings.ToLower(modelID) + "\x00" + strings.ToLower(variant)
}

func catalogVisible(model LoadedLLMModel, minOnDiskNodeCount uint64) bool {
	if model.OnDiskNodeCount < 0 {
		return false
	}
	return uint64(model.OnDiskNodeCount) >= minOnDiskNodeCount
}

func filterCatalogModels(models map[string]LoadedLLMModel, minOnDiskNodeCount uint64) map[string]LoadedLLMModel {
	catalog := make(map[string]LoadedLLMModel, len(models))
	for key, model := range models {
		if catalogVisible(model, minOnDiskNodeCount) {
			catalog[key] = model
		}
	}
	return catalog
}

// InitLoadedModelsCache sets the Relay client used by cache refreshes. It must
// be called once at startup before RefreshLoadedModels.
func InitLoadedModelsCache(client *relay.Client) {
	llmModelsCache.mu.Lock()
	defer llmModelsCache.mu.Unlock()
	llmModelsCache.client = client
	sdModelsCache.mu.Lock()
	sdModelsCache.client = client
	sdModelsCache.mu.Unlock()
}

// RefreshLoadedModels fetches the loaded models from the Relay and replaces the
// in-memory LLM and SD model snapshots. On failure the previous snapshots are kept.
func RefreshLoadedModels(ctx context.Context) error {
	llmModelsCache.mu.RLock()
	client := llmModelsCache.client
	llmModelsCache.mu.RUnlock()
	if client == nil {
		return errors.New("loaded models cache is not initialized")
	}

	loadedModels, err := client.GetLoadedModels(ctx)
	if err != nil {
		return err
	}

	llmSnapshot := make(map[string]LoadedLLMModel)
	sdSnapshot := make(map[string]LoadedLLMModel)
	for _, loadedModel := range loadedModels {
		modelID := strings.ToLower(strings.TrimSpace(loadedModel.ModelID))
		if modelID == "" {
			continue
		}
		variant := strings.ToLower(strings.TrimSpace(loadedModel.Variant))
		entry := LoadedLLMModel{
			ModelID:         modelID,
			Variant:         variant,
			MinVRAM:         loadedModel.MinVRAM,
			NodeCount:       loadedModel.InMemoryNodeCount,
			OnDiskNodeCount: loadedModel.OnDiskNodeCount,
		}
		switch loadedModel.ModelType {
		case loadedModelTypeLLM:
			if variant != "" {
				continue
			}
			llmSnapshot[modelID] = entry
		case loadedModelTypeSD:
			sdSnapshot[sdCacheKey(modelID, variant)] = entry
		}
	}

	minOnDiskNodeCount := config.GetConfig().LLM.MinCatalogOnDiskNodeCount
	llmCatalog := filterCatalogModels(llmSnapshot, minOnDiskNodeCount)
	sdCatalog := filterCatalogModels(sdSnapshot, minOnDiskNodeCount)

	llmModelsCache.mu.Lock()
	llmModelsCache.models = llmSnapshot
	llmModelsCache.catalogModels = llmCatalog
	llmModelsCache.mu.Unlock()
	sdModelsCache.mu.Lock()
	sdModelsCache.models = sdSnapshot
	sdModelsCache.catalogModels = sdCatalog
	sdModelsCache.mu.Unlock()

	log.Infof(
		"loaded models cache refreshed: %d LLM models (%d catalog), %d image models (%d catalog)",
		len(llmSnapshot),
		len(llmCatalog),
		len(sdSnapshot),
		len(sdCatalog),
	)
	return nil
}

func GetLoadedSDModel(modelID, variant string) (LoadedLLMModel, bool) {
	sdModelsCache.mu.RLock()
	defer sdModelsCache.mu.RUnlock()
	model, ok := sdModelsCache.models[sdCacheKey(modelID, variant)]
	return model, ok
}

func ListLoadedSDModels() []LoadedLLMModel {
	sdModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(sdModelsCache.catalogModels))
	for _, model := range sdModelsCache.catalogModels {
		models = append(models, model)
	}
	sdModelsCache.mu.RUnlock()
	sort.Slice(models, func(i, j int) bool {
		if models[i].ModelID != models[j].ModelID {
			return models[i].ModelID < models[j].ModelID
		}
		return models[i].Variant < models[j].Variant
	})
	return models
}

// GetLoadedLLMModel looks up a cached LLM model by model ID (case-insensitive).
func GetLoadedLLMModel(modelID string) (LoadedLLMModel, bool) {
	llmModelsCache.mu.RLock()
	defer llmModelsCache.mu.RUnlock()
	model, ok := llmModelsCache.models[strings.ToLower(modelID)]
	return model, ok
}

// GetCatalogLLMModel looks up an LLM model in the catalog snapshot by model ID
// (case-insensitive). Models below the configured on-disk node threshold are absent.
func GetCatalogLLMModel(modelID string) (LoadedLLMModel, bool) {
	llmModelsCache.mu.RLock()
	defer llmModelsCache.mu.RUnlock()
	model, ok := llmModelsCache.catalogModels[strings.ToLower(modelID)]
	return model, ok
}

// ListLoadedLLMModels returns the catalog LLM models sorted by model ID.
func ListLoadedLLMModels() []LoadedLLMModel {
	llmModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(llmModelsCache.catalogModels))
	for _, model := range llmModelsCache.catalogModels {
		models = append(models, model)
	}
	llmModelsCache.mu.RUnlock()

	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelID < models[j].ModelID
	})
	return models
}
