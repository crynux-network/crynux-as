package service

import (
	"context"
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
	ModelID   string
	MinVRAM   uint64
	NodeCount int64
}

type loadedModelsCache struct {
	mu     sync.RWMutex
	client *relay.Client
	models map[string]LoadedLLMModel
}

var llmModelsCache = &loadedModelsCache{
	models: map[string]LoadedLLMModel{},
}

var sdModelsCache = &loadedModelsCache{
	models: map[string]LoadedLLMModel{},
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
// in-memory LLM model snapshot. On failure the previous snapshot is kept.
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
		var snapshot map[string]LoadedLLMModel
		switch loadedModel.ModelType {
		case loadedModelTypeLLM:
			snapshot = llmSnapshot
		case loadedModelTypeSD:
			snapshot = sdSnapshot
		default:
			continue
		}
		modelID := strings.ToLower(loadedModel.ModelID)
		snapshot[modelID] = LoadedLLMModel{
			ModelID:   modelID,
			MinVRAM:   loadedModel.MinVRAM,
			NodeCount: loadedModel.InMemoryNodeCount,
		}
	}

	llmModelsCache.mu.Lock()
	llmModelsCache.models = llmSnapshot
	llmModelsCache.mu.Unlock()
	sdModelsCache.mu.Lock()
	sdModelsCache.models = sdSnapshot
	sdModelsCache.mu.Unlock()

	log.Infof("loaded models cache refreshed: %d LLM models, %d image models", len(llmSnapshot), len(sdSnapshot))
	return nil
}

func GetLoadedSDModel(modelID string) (LoadedLLMModel, bool) {
	sdModelsCache.mu.RLock()
	defer sdModelsCache.mu.RUnlock()
	model, ok := sdModelsCache.models[strings.ToLower(modelID)]
	return model, ok
}

func ListLoadedSDModels() []LoadedLLMModel {
	sdModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(sdModelsCache.models))
	for _, model := range sdModelsCache.models {
		models = append(models, model)
	}
	sdModelsCache.mu.RUnlock()
	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelID < models[j].ModelID
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

// ListLoadedLLMModels returns the cached LLM models sorted by model ID.
func ListLoadedLLMModels() []LoadedLLMModel {
	llmModelsCache.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(llmModelsCache.models))
	for _, model := range llmModelsCache.models {
		models = append(models, model)
	}
	llmModelsCache.mu.RUnlock()

	sort.Slice(models, func(i, j int) bool {
		return models[i].ModelID < models[j].ModelID
	})
	return models
}
