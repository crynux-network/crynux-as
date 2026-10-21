package service

import (
	"bytes"
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const loadedModelTypeLLM = "llm"
const loadedModelTypeSD = "sd"

type LoadedLLMModel struct {
	ModelID            string
	ExecutionDType     string
	QuantizationConfig json.RawMessage
	QuantizationKey    string
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

func modelCacheKey(modelID, quantKey, executionDType string) string {
	return strings.ToLower(modelID) + "\x00" + quantKey + "\x00" + strings.ToLower(executionDType)
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

func betterMaxMinVRAM(candidate, existing LoadedLLMModel) bool {
	if candidate.MinVRAM != existing.MinVRAM {
		return candidate.MinVRAM > existing.MinVRAM
	}
	if candidate.QuantizationKey != existing.QuantizationKey {
		return candidate.QuantizationKey < existing.QuantizationKey
	}
	return candidate.ExecutionDType < existing.ExecutionDType
}

func pickMaxMinVRAM(models map[string]LoadedLLMModel, modelID string) (LoadedLLMModel, bool) {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	var best LoadedLLMModel
	found := false
	for _, model := range models {
		if model.ModelID != modelID {
			continue
		}
		if !found || betterMaxMinVRAM(model, best) {
			best = model
			found = true
		}
	}
	return best, found
}

func sortLoadedModels(models []LoadedLLMModel) {
	sort.Slice(models, func(i, j int) bool {
		if models[i].ModelID != models[j].ModelID {
			return models[i].ModelID < models[j].ModelID
		}
		if models[i].ExecutionDType != models[j].ExecutionDType {
			return models[i].ExecutionDType < models[j].ExecutionDType
		}
		return models[i].QuantizationKey < models[j].QuantizationKey
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
		quantRaw, quantKey, err := normalizeQuantizationConfig(loadedModel.QuantizationConfig)
		if err != nil {
			log.Warnf("skip loaded model %s: invalid quantization_config: %v", modelID, err)
			continue
		}
		executionDType := strings.ToLower(strings.TrimSpace(loadedModel.ExecutionDType))
		entry := LoadedLLMModel{
			ModelID:            modelID,
			ExecutionDType:     executionDType,
			QuantizationConfig: quantRaw,
			QuantizationKey:    quantKey,
			MinVRAM:            loadedModel.MinVRAM,
			NodeCount:          loadedModel.InMemoryNodeCount,
			OnDiskNodeCount:    loadedModel.OnDiskNodeCount,
		}
		key := modelCacheKey(modelID, quantKey, executionDType)
		switch loadedModel.ModelType {
		case loadedModelTypeLLM:
			llmSnapshot[key] = entry
		case loadedModelTypeSD:
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

func GetLoadedSDModel(modelID string) (LoadedLLMModel, bool) {
	sdModelsCache.mu.RLock()
	defer sdModelsCache.mu.RUnlock()
	return pickMaxMinVRAM(sdModelsCache.models, modelID)
}

func GetLoadedSDModelExact(modelID string, quantizationConfig any, executionDType string) (LoadedLLMModel, bool) {
	quantKey, err := models.QuantizationConfigKey(quantizationConfig)
	if err != nil {
		return LoadedLLMModel{}, false
	}
	key := modelCacheKey(modelID, quantKey, executionDType)
	sdModelsCache.mu.RLock()
	defer sdModelsCache.mu.RUnlock()
	model, ok := sdModelsCache.models[key]
	return model, ok
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
// with the greatest min_vram (ties broken by quantization key then dtype).
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
	key := modelCacheKey(modelID, quantKey, executionDType)
	llmModelsCache.mu.RLock()
	defer llmModelsCache.mu.RUnlock()
	model, ok := llmModelsCache.models[key]
	return model, ok
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
// execution dtype, and quantization key.
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
