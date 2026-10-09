package llm

import (
	"crynux_as/service"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type modelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
	MinVram uint64 `json:"min_vram"`
}

type imageModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
	MinVram uint64 `json:"min_vram"`
	Variant string `json:"variant"`
}

func newModelObject(model service.LoadedLLMModel) modelObject {
	return modelObject{
		ID:      model.ModelID,
		Object:  "model",
		Created: 0,
		OwnedBy: "crynux",
		MinVram: model.MinVRAM,
	}
}

func newImageModelObject(model service.LoadedLLMModel) imageModelObject {
	return imageModelObject{
		ID:      model.ModelID,
		Object:  "model",
		Created: 0,
		OwnedBy: "crynux",
		MinVram: model.MinVRAM,
		Variant: model.Variant,
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
func RetrieveModel(c *gin.Context) {
	modelID := strings.TrimPrefix(c.Param("model"), "/")
	if modelID == "image" {
		ImageModels(c)
		return
	}
	model, ok := service.GetCatalogLLMModel(modelID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "The model '" + modelID + "' does not exist",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}
	c.JSON(http.StatusOK, newModelObject(model))
}
