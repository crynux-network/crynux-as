package llm

import (
	"crynux_as/service"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type modelObject struct {
	ID                 string `json:"id"`
	Object             string `json:"object"`
	Created            int64  `json:"created"`
	OwnedBy            string `json:"owned_by"`
	MinVram            uint64 `json:"min_vram"`
	OnDiskNodeCount    int64  `json:"on_disk_node_count"`
	ExecutionDType     string `json:"execution_dtype"`
	QuantizationConfig any    `json:"quantization_config"`
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
// in-memory loaded-models cache.
func Models(c *gin.Context) {
	loadedModels := service.ListLoadedLLMModels()
	data := make([]modelObject, 0, len(loadedModels))
	for _, model := range loadedModels {
		data = append(data, newModelObject(model))
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
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
	if len(models) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "The model '" + modelID + "' does not exist",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}
	data := make([]modelObject, 0, len(models))
	for _, model := range models {
		data = append(data, newModelObject(model))
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}
