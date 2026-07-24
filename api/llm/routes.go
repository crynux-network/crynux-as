package llm

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// InitRoutes registers the private OpenAI-compatible LLM API endpoints.
// The endpoint token in the URL locates the project, and the API key in the
// Authorization header authenticates the caller.
func InitRoutes(engine *gin.Engine) {
	group := engine.Group("/api/:endpoint_token/v1")
	group.Use(ProjectAuthMiddleware())
	group.POST("/chat/completions", ChatCompletions)
	group.POST("/completions", Completions)
	group.GET("/models", Models)
}

func Models(ctx *gin.Context) {
	ctx.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
