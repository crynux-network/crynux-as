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

type LoadedLLMModel struct {
	ModelID string
	MinVRAM uint64
}

type loadedModelsCache struct {
	mu     sync.RWMutex
	client *relay.Client
	models map[string]LoadedLLMModel
}

var llmModelsCache = &loadedModelsCache{
	models: map[string]LoadedLLMModel{},
}

// InitLoadedModelsCache sets the Relay client used by cache refreshes. It must
// be called once at startup before RefreshLoadedModels.
func InitLoadedModelsCache(client *relay.Client) {
	llmModelsCache.mu.Lock()
	defer llmModelsCache.mu.Unlock()
	llmModelsCache.client = client
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

	snapshot := make(map[string]LoadedLLMModel)
	for _, loadedModel := range loadedModels {
		if loadedModel.ModelType != loadedModelTypeLLM {
			continue
		}
		modelID := strings.ToLower(loadedModel.ModelID)
		snapshot[modelID] = LoadedLLMModel{
			ModelID: modelID,
			MinVRAM: loadedModel.MinVRAM,
		}
	}

	llmModelsCache.mu.Lock()
	llmModelsCache.models = snapshot
	llmModelsCache.mu.Unlock()

	log.Infof("loaded models cache refreshed: %d LLM models", len(snapshot))
	return nil
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
