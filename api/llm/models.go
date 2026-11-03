package llm

import (
	"crynux_as/llmadapter"
	"crynux_as/service"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func writeCatalogList(c *gin.Context, data []any) {
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}

// Models returns the merged catalog of all chat and image models.
func Models(c *gin.Context) {
	writeCatalogList(c, service.ListModelsCatalogAll())
}

// ChatModels returns Crynux LLM catalog rows merged with visible Provider chat models.
func ChatModels(c *gin.Context) {
	writeCatalogList(c, service.ListModelsCatalogChat())
}

// ImageModels returns Crynux SD catalog rows merged with visible Provider image models.
func ImageModels(c *gin.Context) {
	writeCatalogList(c, service.ListModelsCatalogImage())
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

	data := service.ListModelsCatalogByID(llmadapter.NormalizeModelID(requested))
	if len(data) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "The model '" + requested + "' does not exist",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}
	writeCatalogList(c, data)
}
