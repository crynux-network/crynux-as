package llm

import (
	"crynux_as/config"
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

// imageModelObject extends the model object with the Image cache identity fields.
type imageModelObject struct {
	modelObject
	ModelCacheHash string         `json:"model_cache_hash"`
	Components     map[string]any `json:"components"`
	Lora           map[string]any `json:"lora"`
}

func newImageModelObject(model service.LoadedLLMModel) imageModelObject {
	return imageModelObject{
		modelObject:    newModelObject(model),
		ModelCacheHash: model.ModelCacheHash,
		Components:     model.Components,
		Lora:           model.Lora,
	}
}

// Models implements the OpenAI-compatible list models endpoint from the
// in-memory loaded-models cache merged with visible Provider models.
func Models(c *gin.Context) {
	loadedModels := service.ListLoadedLLMModels()
	data := make([]modelObject, 0, len(loadedModels))
	for _, model := range loadedModels {
		data = append(data, newModelObject(model))
	}
	providerModels, err := service.ListVisibleProviderModels(c.Request.Context(), config.GetDB())
	if err != nil {
		log.Errorf("list visible provider models failed: %v", err)
	} else {
		for _, route := range providerModels {
			data = append(data, newProviderModelObject(route))
		}
	}
	sort.SliceStable(data, func(i, j int) bool {
		if data[i].ID != data[j].ID {
			return data[i].ID < data[j].ID
		}
		return providerOwnedByRank(data[i].OwnedBy) < providerOwnedByRank(data[j].OwnedBy)
	})
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
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

func ImageModels(c *gin.Context) {
	loadedModels := service.ListLoadedSDModels()
	data := make([]imageModelObject, 0, len(loadedModels))
	for _, model := range loadedModels {
		data = append(data, newImageModelObject(model))
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}

// RetrieveModel implements the OpenAI-compatible retrieve model endpoint.
// When multiple catalog rows share a model id, the response is a list envelope.
func RetrieveModel(c *gin.Context) {
	modelID := strings.TrimPrefix(c.Param("model"), "/")
	if modelID == "image" {
		ImageModels(c)
		return
	}
	models := service.ListCatalogLLMModelsByID(modelID)
	data := make([]modelObject, 0, len(models)+1)
	for _, model := range models {
		data = append(data, newModelObject(model))
	}
	if route, ok := service.LookupProviderModel(modelID); ok {
		visible, err := service.ProviderHasEnabledKey(c.Request.Context(), config.GetDB(), route.ServiceProvider)
		if err != nil {
			log.Errorf("provider key visibility check failed: %v", err)
		} else if visible {
			data = append(data, newProviderModelObject(route))
		}
	}
	if len(data) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "The model '" + modelID + "' does not exist",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}
