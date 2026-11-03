package llm

import (
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/service"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type modelObject struct {
	ID                 string `json:"id"`
	Object             string `json:"object"`
	Created            int64  `json:"created"`
	OwnedBy            string `json:"owned_by"`
	MinVram            uint64 `json:"min_vram,omitempty"`
	OnDiskNodeCount    int64  `json:"on_disk_node_count,omitempty"`
	ExecutionDType     string `json:"execution_dtype,omitempty"`
	QuantizationConfig any    `json:"quantization_config,omitempty"`
}

func newModelObject(model service.LoadedLLMModel) modelObject {
	return modelObject{
		ID:                 model.ModelID,
		Object:             "model",
		Created:            0,
		OwnedBy:            "crynux",
		MinVram:            model.MinVRAM,
		OnDiskNodeCount:    model.OnDiskNodeCount,
		ExecutionDType:     model.ExecutionDType,
		QuantizationConfig: service.QuantizationConfigForAPI(model.QuantizationConfig),
	}
}

func newProviderModelObject(route service.ProviderModelRoute) modelObject {
	return modelObject{
		ID:      route.ModelName,
		Object:  "model",
		Created: 0,
		OwnedBy: string(route.ServiceProvider),
	}
}

// imageModelObject extends the model object with executable Image TaskArgs fields.
type imageModelObject struct {
	modelObject
	Components map[string]any `json:"components"`
	Lora       map[string]any `json:"lora"`
}

func newImageModelObject(model service.LoadedLLMModel) imageModelObject {
	return imageModelObject{
		modelObject: newModelObject(model),
		Components:  model.Components,
		Lora:        model.Lora,
	}
}

type catalogEntry struct {
	ID      string
	OwnedBy string
	Value   any
}

func sortCatalogEntries(entries []catalogEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].ID != entries[j].ID {
			return entries[i].ID < entries[j].ID
		}
		return providerOwnedByRank(entries[i].OwnedBy) < providerOwnedByRank(entries[j].OwnedBy)
	})
}

func catalogValues(entries []catalogEntry) []any {
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Value)
	}
	return out
}

func appendVisibleProviderModels(c *gin.Context, entries []catalogEntry, apiKind string) []catalogEntry {
	providerModels, err := service.ListVisibleProviderModels(c.Request.Context(), config.GetDB(), apiKind)
	if err != nil {
		log.Errorf("list visible provider %s models failed: %v", apiKind, err)
		return entries
	}
	for _, route := range providerModels {
		obj := newProviderModelObject(route)
		entries = append(entries, catalogEntry{ID: obj.ID, OwnedBy: obj.OwnedBy, Value: obj})
	}
	return entries
}

func chatCatalogEntries(c *gin.Context) []catalogEntry {
	loadedModels := service.ListLoadedLLMModels()
	entries := make([]catalogEntry, 0, len(loadedModels))
	for _, model := range loadedModels {
		obj := newModelObject(model)
		entries = append(entries, catalogEntry{ID: obj.ID, OwnedBy: obj.OwnedBy, Value: obj})
	}
	return appendVisibleProviderModels(c, entries, config.ProviderAPIKindChat)
}

func imageCatalogEntries(c *gin.Context) []catalogEntry {
	loadedModels := service.ListLoadedSDModels()
	entries := make([]catalogEntry, 0, len(loadedModels))
	for _, model := range loadedModels {
		obj := newImageModelObject(model)
		entries = append(entries, catalogEntry{ID: obj.ID, OwnedBy: obj.OwnedBy, Value: obj})
	}
	return appendVisibleProviderModels(c, entries, config.ProviderAPIKindImage)
}

func writeCatalogList(c *gin.Context, entries []catalogEntry) {
	sortCatalogEntries(entries)
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   catalogValues(entries),
	})
}

// Models returns the merged catalog of all chat and image models.
func Models(c *gin.Context) {
	entries := append(chatCatalogEntries(c), imageCatalogEntries(c)...)
	writeCatalogList(c, entries)
}

// ChatModels returns Crynux LLM catalog rows merged with visible Provider chat models.
func ChatModels(c *gin.Context) {
	writeCatalogList(c, chatCatalogEntries(c))
}

// ImageModels returns Crynux SD catalog rows merged with visible Provider image models.
func ImageModels(c *gin.Context) {
	writeCatalogList(c, imageCatalogEntries(c))
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

// RetrieveModel implements the OpenAI-compatible retrieve model endpoint.
// When multiple catalog rows share a model id, the response is a list envelope.
func RetrieveModel(c *gin.Context) {
	requested := strings.TrimPrefix(c.Param("model"), "/")
	switch requested {
	case "chat":
		ChatModels(c)
		return
	case "image":
		ImageModels(c)
		return
	}

	modelID := llmadapter.NormalizeModelID(requested)
	entries := make([]catalogEntry, 0)
	for _, model := range service.ListCatalogLLMModelsByID(modelID) {
		obj := newModelObject(model)
		entries = append(entries, catalogEntry{ID: obj.ID, OwnedBy: obj.OwnedBy, Value: obj})
	}
	for _, model := range service.ListCatalogSDModelsByID(modelID) {
		obj := newImageModelObject(model)
		entries = append(entries, catalogEntry{ID: obj.ID, OwnedBy: obj.OwnedBy, Value: obj})
	}
	if route, ok := service.LookupProviderModel(modelID); ok {
		visible, err := service.ProviderHasEnabledKey(c.Request.Context(), config.GetDB(), route.ServiceProvider)
		if err != nil {
			log.Errorf("provider key visibility check failed: %v", err)
		} else if visible {
			obj := newProviderModelObject(route)
			entries = append(entries, catalogEntry{ID: obj.ID, OwnedBy: obj.OwnedBy, Value: obj})
		}
	}
	if len(entries) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "The model '" + requested + "' does not exist",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}
	writeCatalogList(c, entries)
}
