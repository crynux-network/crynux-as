package service

import (
	"bytes"
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const loadedModelTypeLLM = "llm"
const loadedModelTypeSD = "sd"

// LoadedLLMModel is one Relay loaded-models row identified by its model cache
// hash and execution dtype. Components and Lora use the Image TaskArgs shape.
type LoadedLLMModel struct {
	ModelCacheHash     string
	TaskKind           string
	ModelID            string
	ExecutionDType     string
	QuantizationConfig json.RawMessage
	QuantizationKey    string
	Components         map[string]any
	ComponentsKey      string
	Lora               map[string]any
	LoraKey            string
	MinVRAM            uint64
	NodeCount          int64
	OnDiskNodeCount    int64
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

func modelCacheKey(modelCacheHash, executionDType string) string {
	return modelCacheHash + "\x00" + strings.ToLower(executionDType)
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

func normalizeQuantizationConfig(raw json.RawMessage) (json.RawMessage, string, error) {
	key, err := models.QuantizationConfigKey(raw)
	if err != nil {
		return nil, "", err
	}
	if key == "null" {
		return json.RawMessage("null"), "null", nil
	}
	return json.RawMessage(key), key, nil
}

// newLoadedModel builds a cache row from a Relay row and verifies that the
// top-level fields agree with the canonical model_cache_config.
func newLoadedModel(loaded relay.LoadedModel) (LoadedLLMModel, error) {
	modelID := strings.ToLower(strings.TrimSpace(loaded.ModelID))
	if modelID == "" {
		return LoadedLLMModel{}, errors.New("model_id is empty")
	}
	hash, err := normalizeModelCacheHash(loaded.ModelCacheHash)
	if err != nil {
		return LoadedLLMModel{}, err
	}
	cacheConfig, err := parseModelCacheConfig(loaded.ModelCacheConfig)
	if err != nil {
		return LoadedLLMModel{}, err
	}
	wantTaskKind := modelCacheTaskKindLLM
	if loaded.ModelType == loadedModelTypeSD {
		wantTaskKind = modelCacheTaskKindImage
	}
	if cacheConfig.TaskKind != wantTaskKind {
		return LoadedLLMModel{}, fmt.Errorf("model_cache_config task_kind %q does not match model_type %q", cacheConfig.TaskKind, loaded.ModelType)
	}
	if cacheConfig.ModelID != modelID {
		return LoadedLLMModel{}, fmt.Errorf("model_cache_config model %q does not match model_id %q", cacheConfig.ModelID, modelID)
	}
	executionDType := strings.ToLower(strings.TrimSpace(loaded.ExecutionDType))
	if cacheConfig.DType != executionDType {
		return LoadedLLMModel{}, fmt.Errorf("model_cache_config dtype %q does not match execution_dtype %q", cacheConfig.DType, executionDType)
	}
	_, quantKey, err := normalizeQuantizationConfig(loaded.QuantizationConfig)
	if err != nil {
		return LoadedLLMModel{}, fmt.Errorf("invalid quantization_config: %w", err)
	}
	if cacheConfig.QuantizationKey != quantKey {
		return LoadedLLMModel{}, errors.New("model_cache_config quantization_config does not match quantization_config")
	}
	return LoadedLLMModel{
		ModelCacheHash:     hash,
		TaskKind:           cacheConfig.TaskKind,
		ModelID:            modelID,
		ExecutionDType:     executionDType,
		QuantizationConfig: cacheConfig.QuantizationConfig,
		QuantizationKey:    cacheConfig.QuantizationKey,
		Components:         cacheConfig.Components,
		ComponentsKey:      cacheConfig.ComponentsKey,
		Lora:               cacheConfig.Lora,
		LoraKey:            cacheConfig.LoraKey,
		MinVRAM:            loaded.MinVRAM,
		NodeCount:          loaded.InMemoryNodeCount,
		OnDiskNodeCount:    loaded.OnDiskNodeCount,
	}, nil
}

func betterMaxMinVRAM(candidate, existing LoadedLLMModel) bool {
	if candidate.MinVRAM != existing.MinVRAM {
		return candidate.MinVRAM > existing.MinVRAM
	}
	if candidate.QuantizationKey != existing.QuantizationKey {
		return candidate.QuantizationKey < existing.QuantizationKey
	}
	if candidate.ExecutionDType != existing.ExecutionDType {
		return candidate.ExecutionDType < existing.ExecutionDType
	}
	return candidate.ModelCacheHash < existing.ModelCacheHash
}

// pickBest returns the matching row with the greatest min_vram.
func pickBest(models map[string]LoadedLLMModel, match func(LoadedLLMModel) bool) (LoadedLLMModel, bool) {
	var best LoadedLLMModel
	found := false
	for _, model := range models {
		if !match(model) {
			continue
		}
		if !found || betterMaxMinVRAM(model, best) {
			best = model
			found = true
		}
	}
	return best, found
}

func pickMaxMinVRAM(models map[string]LoadedLLMModel, modelID string) (LoadedLLMModel, bool) {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return pickBest(models, func(model LoadedLLMModel) bool { return model.ModelID == modelID })
}

func sortLoadedModels(models []LoadedLLMModel) {
	sort.Slice(models, func(i, j int) bool {
		if models[i].ModelID != models[j].ModelID {
			return models[i].ModelID < models[j].ModelID
		}
		if models[i].ExecutionDType != models[j].ExecutionDType {
			return models[i].ExecutionDType < models[j].ExecutionDType
		}
		if models[i].QuantizationKey != models[j].QuantizationKey {
			return models[i].QuantizationKey < models[j].QuantizationKey
		}
		return models[i].ModelCacheHash < models[j].ModelCacheHash
	})
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
// Rows with a missing or inconsistent model_cache_hash or model_cache_config are skipped.
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
		if loadedModel.ModelType != loadedModelTypeLLM && loadedModel.ModelType != loadedModelTypeSD {
			continue
		}
		entry, err := newLoadedModel(loadedModel)
		if err != nil {
			log.Warnf("skip loaded model %q (type %s, cache hash %q): %v", loadedModel.ModelID, loadedModel.ModelType, loadedModel.ModelCacheHash, err)
			continue
		}
		key := modelCacheKey(entry.ModelCacheHash, entry.ExecutionDType)
		if loadedModel.ModelType == loadedModelTypeLLM {
			llmSnapshot[key] = entry
		} else {
			sdSnapshot[key] = entry
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

// GetLoadedSDModel returns the Image row of modelID with the greatest min_vram.
// It is for model-level checks only; executable selection MUST use the full cache identity.
func GetLoadedSDModel(modelID string) (LoadedLLMModel, bool) {
	sdModelsCache.mu.RLock()
	defer sdModelsCache.mu.RUnlock()
	return pickMaxMinVRAM(sdModelsCache.models, modelID)
}

// pickLoadedSD returns the Image row with the greatest min_vram among rows matching identity.
func pickLoadedSD(identity imageRequestIdentity) (LoadedLLMModel, bool) {
	sdModelsCache.mu.RLock()
	defer sdModelsCache.mu.RUnlock()
	return pickBest(sdModelsCache.models, identity.matches)
}

func ListLoadedSDModels() []LoadedLLMModel {
	sdModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(sdModelsCache.catalogModels))
	for _, model := range sdModelsCache.catalogModels {
		models = append(models, model)
	}
	sdModelsCache.mu.RUnlock()
	sortLoadedModels(models)
	return models
}

// GetLoadedLLMModel looks up cached LLM rows by model ID and returns the row
// with the greatest min_vram (ties broken by quantization key, dtype, then cache hash).
func GetLoadedLLMModel(modelID string) (LoadedLLMModel, bool) {
	llmModelsCache.mu.RLock()
	defer llmModelsCache.mu.RUnlock()
	return pickMaxMinVRAM(llmModelsCache.models, modelID)
}

func GetLoadedLLMModelExact(modelID string, quantizationConfig any, executionDType string) (LoadedLLMModel, bool) {
	quantKey, err := models.QuantizationConfigKey(quantizationConfig)
	if err != nil {
		return LoadedLLMModel{}, false
	}
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	executionDType = strings.ToLower(executionDType)
	llmModelsCache.mu.RLock()
	defer llmModelsCache.mu.RUnlock()
	return pickBest(llmModelsCache.models, func(model LoadedLLMModel) bool {
		return model.ModelID == modelID && model.QuantizationKey == quantKey && model.ExecutionDType == executionDType
	})
}

// GetCatalogLLMModel returns the catalog row for modelID with the greatest min_vram.
func GetCatalogLLMModel(modelID string) (LoadedLLMModel, bool) {
	llmModelsCache.mu.RLock()
	defer llmModelsCache.mu.RUnlock()
	return pickMaxMinVRAM(llmModelsCache.catalogModels, modelID)
}

// ListCatalogLLMModelsByID returns every catalog LLM row for modelID.
func ListCatalogLLMModelsByID(modelID string) []LoadedLLMModel {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	llmModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0)
	for _, model := range llmModelsCache.catalogModels {
		if model.ModelID == modelID {
			models = append(models, model)
		}
	}
	llmModelsCache.mu.RUnlock()
	sortLoadedModels(models)
	return models
}

// ListLoadedLLMModels returns the catalog LLM models sorted by model ID,
// execution dtype, quantization key, and model cache hash.
func ListLoadedLLMModels() []LoadedLLMModel {
	llmModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(llmModelsCache.catalogModels))
	for _, model := range llmModelsCache.catalogModels {
		models = append(models, model)
	}
	llmModelsCache.mu.RUnlock()
	sortLoadedModels(models)
	return models
}

// QuantizationConfigForAPI returns a JSON value suitable for API responses.
func QuantizationConfigForAPI(raw json.RawMessage) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil
	}
	return parsed
}
