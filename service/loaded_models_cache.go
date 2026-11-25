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

const (
	loadedModelTypeLLM   = "llm"
	loadedModelTypeSD    = "sd"
	loadedModelTypeVideo = "video"
)

// LoadedLLMModel is one Relay loaded-models row identified by its local model
// cache hash. Components, Lora, and Loras use the TaskArgs shape: Components is
// set for image and video rows, Lora for image rows, and Loras for video rows.
type LoadedLLMModel struct {
	ModelCacheHash     string
	TaskKind           string
	ModelID            string
	DType              string
	QuantizationConfig json.RawMessage
	QuantizationKey    string
	Components         map[string]any
	ComponentsKey      string
	Lora               map[string]any
	LoraKey            string
	Loras              []any
	LorasKey           string
	MinVRAM            uint64
	NodeCount          int64
	OnDiskNodeCount    int64
}

func (m LoadedLLMModel) identity() modelCacheIdentity {
	identity := modelCacheIdentity{
		TaskKind:        m.TaskKind,
		ModelID:         m.ModelID,
		DType:           m.DType,
		QuantizationKey: m.QuantizationKey,
		ComponentsKey:   m.ComponentsKey,
		LoraKey:         m.LoraKey,
	}
	if m.TaskKind == modelCacheTaskKindVideo {
		identity.LoraKey = m.LorasKey
	}
	return identity
}

type loadedModelsCache struct {
	mu            sync.RWMutex
	client        *relay.Client
	models        map[string]LoadedLLMModel
	catalogModels map[string]LoadedLLMModel
}

func newLoadedModelsCache() *loadedModelsCache {
	return &loadedModelsCache{
		models:        map[string]LoadedLLMModel{},
		catalogModels: map[string]LoadedLLMModel{},
	}
}

var llmModelsCache = newLoadedModelsCache()

var sdModelsCache = newLoadedModelsCache()

var videoModelsCache = newLoadedModelsCache()

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

// decodeOptionalJSON decodes raw with json.Number for numbers. Empty raw decodes to nil.
func decodeOptionalJSON(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// newLoadedModel builds a cache row from a public Relay row and derives its
// local model cache hash from the normalized execution identity.
func newLoadedModel(loaded relay.LoadedModel) (LoadedLLMModel, error) {
	modelID := strings.ToLower(strings.TrimSpace(loaded.ModelID))
	if modelID == "" {
		return LoadedLLMModel{}, errors.New("model_id is empty")
	}
	dtype, err := models.ParseConcreteDType(loaded.DType)
	if err != nil {
		return LoadedLLMModel{}, fmt.Errorf("invalid dtype %q: %w", loaded.DType, err)
	}
	quantRaw, quantKey, err := normalizeQuantizationConfig(loaded.QuantizationConfig)
	if err != nil {
		return LoadedLLMModel{}, fmt.Errorf("invalid quantization_config: %w", err)
	}
	entry := LoadedLLMModel{
		ModelID:            modelID,
		DType:              string(dtype),
		QuantizationConfig: quantRaw,
		QuantizationKey:    quantKey,
		MinVRAM:            loaded.MinVRAM,
		NodeCount:          loaded.InMemoryNodeCount,
		OnDiskNodeCount:    loaded.OnDiskNodeCount,
	}
	switch loaded.ModelType {
	case loadedModelTypeLLM:
		entry.TaskKind = modelCacheTaskKindLLM
	case loadedModelTypeSD, loadedModelTypeVideo:
		components, err := decodeOptionalJSON(loaded.Components)
		if err != nil {
			return LoadedLLMModel{}, fmt.Errorf("invalid components: %w", err)
		}
		if entry.Components, entry.ComponentsKey, err = normalizeTaskArgsComponents(components); err != nil {
			return LoadedLLMModel{}, fmt.Errorf("invalid components: %w", err)
		}
		if loaded.ModelType == loadedModelTypeSD {
			entry.TaskKind = modelCacheTaskKindImage
			lora, err := decodeOptionalJSON(loaded.Lora)
			if err != nil {
				return LoadedLLMModel{}, fmt.Errorf("invalid lora: %w", err)
			}
			if entry.Lora, entry.LoraKey, err = normalizeTaskArgsLora(lora); err != nil {
				return LoadedLLMModel{}, fmt.Errorf("invalid lora: %w", err)
			}
		} else {
			entry.TaskKind = modelCacheTaskKindVideo
			loras, err := decodeOptionalJSON(loaded.Loras)
			if err != nil {
				return LoadedLLMModel{}, fmt.Errorf("invalid loras: %w", err)
			}
			if entry.Loras, entry.LorasKey, err = normalizeTaskArgsVideoLoras(loras); err != nil {
				return LoadedLLMModel{}, fmt.Errorf("invalid loras: %w", err)
			}
		}
	default:
		return LoadedLLMModel{}, fmt.Errorf("unsupported model_type %q", loaded.ModelType)
	}
	if entry.ModelCacheHash, err = entry.identity().hash(); err != nil {
		return LoadedLLMModel{}, err
	}
	return entry, nil
}

func betterMaxMinVRAM(candidate, existing LoadedLLMModel) bool {
	if candidate.MinVRAM != existing.MinVRAM {
		return candidate.MinVRAM > existing.MinVRAM
	}
	if candidate.QuantizationKey != existing.QuantizationKey {
		return candidate.QuantizationKey < existing.QuantizationKey
	}
	if candidate.DType != existing.DType {
		return candidate.DType < existing.DType
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
		if models[i].DType != models[j].DType {
			return models[i].DType < models[j].DType
		}
		if models[i].QuantizationKey != models[j].QuantizationKey {
			return models[i].QuantizationKey < models[j].QuantizationKey
		}
		return models[i].ModelCacheHash < models[j].ModelCacheHash
	})
}

// getMaxMinVRAM returns the full-cache row of modelID with the greatest min_vram.
func (c *loadedModelsCache) getMaxMinVRAM(modelID string) (LoadedLLMModel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return pickMaxMinVRAM(c.models, modelID)
}

// pick returns the full-cache row with the greatest min_vram among rows matching match.
func (c *loadedModelsCache) pick(match func(LoadedLLMModel) bool) (LoadedLLMModel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return pickBest(c.models, match)
}

// hasOnDiskNodes reports whether any full-cache row matching match has on_disk_node_count > 0.
func (c *loadedModelsCache) hasOnDiskNodes(match func(LoadedLLMModel) bool) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, model := range c.models {
		if model.OnDiskNodeCount > 0 && match(model) {
			return true
		}
	}
	return false
}

// listCatalog returns the sorted catalog rows matching match; a nil match returns all rows.
func (c *loadedModelsCache) listCatalog(match func(LoadedLLMModel) bool) []LoadedLLMModel {
	c.mu.RLock()
	models := make([]LoadedLLMModel, 0, len(c.catalogModels))
	for _, model := range c.catalogModels {
		if match == nil || match(model) {
			models = append(models, model)
		}
	}
	c.mu.RUnlock()
	sortLoadedModels(models)
	return models
}

func (c *loadedModelsCache) listCatalogByID(modelID string) []LoadedLLMModel {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return c.listCatalog(func(model LoadedLLMModel) bool { return model.ModelID == modelID })
}

func (c *loadedModelsCache) replace(models map[string]LoadedLLMModel, minOnDiskNodeCount uint64) map[string]LoadedLLMModel {
	catalog := filterCatalogModels(models, minOnDiskNodeCount)
	c.mu.Lock()
	c.models = models
	c.catalogModels = catalog
	c.mu.Unlock()
	return catalog
}

// InitLoadedModelsCache sets the Relay client used by cache refreshes. It must
// be called once at startup before RefreshLoadedModels.
func InitLoadedModelsCache(client *relay.Client) {
	for _, cache := range []*loadedModelsCache{llmModelsCache, sdModelsCache, videoModelsCache} {
		cache.mu.Lock()
		cache.client = client
		cache.mu.Unlock()
	}
}

// RefreshLoadedModels fetches the loaded models from the Relay and replaces the
// in-memory LLM, image, and video model snapshots. On failure the previous
// snapshots are kept. Rows that fail validation are skipped.
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

	snapshots := map[string]map[string]LoadedLLMModel{
		loadedModelTypeLLM:   {},
		loadedModelTypeSD:    {},
		loadedModelTypeVideo: {},
	}
	for _, loadedModel := range loadedModels {
		snapshot, ok := snapshots[loadedModel.ModelType]
		if !ok {
			continue
		}
		entry, err := newLoadedModel(loadedModel)
		if err != nil {
			log.Warnf("skip loaded model %q (type %s, dtype %q): %v", loadedModel.ModelID, loadedModel.ModelType, loadedModel.DType, err)
			continue
		}
		if IsConfiguredProviderModel(entry.ModelID) {
			log.Warnf(
				"skip Crynux loaded model %q (type %s): name reserved by Provider configuration",
				entry.ModelID,
				loadedModel.ModelType,
			)
			continue
		}
		snapshot[entry.ModelCacheHash] = entry
	}

	minOnDiskNodeCount := config.GetConfig().LLM.MinCatalogOnDiskNodeCount
	llmCatalog := llmModelsCache.replace(snapshots[loadedModelTypeLLM], minOnDiskNodeCount)
	sdCatalog := sdModelsCache.replace(snapshots[loadedModelTypeSD], minOnDiskNodeCount)
	videoCatalog := videoModelsCache.replace(snapshots[loadedModelTypeVideo], minOnDiskNodeCount)

	log.Infof(
		"loaded models cache refreshed: %d LLM models (%d catalog), %d image models (%d catalog), %d video models (%d catalog)",
		len(snapshots[loadedModelTypeLLM]),
		len(llmCatalog),
		len(snapshots[loadedModelTypeSD]),
		len(sdCatalog),
		len(snapshots[loadedModelTypeVideo]),
		len(videoCatalog),
	)
	return nil
}

// GetLoadedSDModel returns the Image row of modelID with the greatest min_vram.
// It is for model-level checks only; executable selection MUST use the full cache identity.
func GetLoadedSDModel(modelID string) (LoadedLLMModel, bool) {
	return sdModelsCache.getMaxMinVRAM(modelID)
}

// pickLoadedSD returns the Image row with the greatest min_vram among rows matching identity.
func pickLoadedSD(identity diffusionRequestIdentity) (LoadedLLMModel, bool) {
	return sdModelsCache.pick(identity.matches)
}

func ListLoadedSDModels() []LoadedLLMModel {
	return sdModelsCache.listCatalog(nil)
}

// GetLoadedVideoModel returns the Video row of modelID with the greatest min_vram.
// It is for model-level checks only; executable selection MUST use the full cache identity.
func GetLoadedVideoModel(modelID string) (LoadedLLMModel, bool) {
	return videoModelsCache.getMaxMinVRAM(modelID)
}

// pickLoadedVideo returns the Video row with the greatest min_vram among rows matching identity.
func pickLoadedVideo(identity diffusionRequestIdentity) (LoadedLLMModel, bool) {
	return videoModelsCache.pick(identity.matches)
}

// ListLoadedVideoModels returns the catalog Video models sorted by model ID,
// dtype, quantization key, and model cache hash.
func ListLoadedVideoModels() []LoadedLLMModel {
	return videoModelsCache.listCatalog(nil)
}

// ListCatalogVideoModelsByID returns every catalog Video row for modelID.
func ListCatalogVideoModelsByID(modelID string) []LoadedLLMModel {
	return videoModelsCache.listCatalogByID(modelID)
}

// GetLoadedLLMModel looks up cached LLM rows by model ID and returns the row
// with the greatest min_vram (ties broken by quantization key, dtype, then cache hash).
func GetLoadedLLMModel(modelID string) (LoadedLLMModel, bool) {
	return llmModelsCache.getMaxMinVRAM(modelID)
}

// llmModelHasOnDiskNodes reports whether any full-cache LLM row for modelID has
// on_disk_node_count greater than zero.
func llmModelHasOnDiskNodes(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return llmModelsCache.hasOnDiskNodes(func(model LoadedLLMModel) bool {
		return model.ModelID == modelID
	})
}

// sdModelHasOnDiskNodes reports whether any full-cache Image row for modelID and
// componentsKey has on_disk_node_count greater than zero.
func sdModelHasOnDiskNodes(modelID, componentsKey string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return sdModelsCache.hasOnDiskNodes(func(model LoadedLLMModel) bool {
		return model.ModelID == modelID && model.ComponentsKey == componentsKey
	})
}

// videoModelHasOnDiskNodes reports whether any full-cache Video row for modelID and
// componentsKey has on_disk_node_count greater than zero.
func videoModelHasOnDiskNodes(modelID, componentsKey string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return videoModelsCache.hasOnDiskNodes(func(model LoadedLLMModel) bool {
		return model.ModelID == modelID && model.ComponentsKey == componentsKey
	})
}

func GetLoadedLLMModelExact(modelID string, quantizationConfig any, dtype string) (LoadedLLMModel, bool) {
	quantKey, err := models.QuantizationConfigKey(quantizationConfig)
	if err != nil {
		return LoadedLLMModel{}, false
	}
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	dtype = strings.ToLower(dtype)
	return llmModelsCache.pick(func(model LoadedLLMModel) bool {
		return model.ModelID == modelID && model.QuantizationKey == quantKey && model.DType == dtype
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
	return llmModelsCache.listCatalogByID(modelID)
}

// ListCatalogSDModelsByID returns every catalog Image row for modelID.
func ListCatalogSDModelsByID(modelID string) []LoadedLLMModel {
	return sdModelsCache.listCatalogByID(modelID)
}

// ListLoadedLLMModels returns the catalog LLM models sorted by model ID,
// dtype, quantization key, and model cache hash.
func ListLoadedLLMModels() []LoadedLLMModel {
	return llmModelsCache.listCatalog(nil)
}

// QuantizationConfigForAPI returns a JSON value suitable for API responses and
// TaskArgs injection. Empty or null input yields a true nil any. Non-empty
// input is returned as json.RawMessage so number literals such as 6.0 stay
// unchanged when the value is marshaled again.
func QuantizationConfigForAPI(raw json.RawMessage) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if !json.Valid(trimmed) {
		return nil
	}
	out := make(json.RawMessage, len(trimmed))
	copy(out, trimmed)
	return out
}
