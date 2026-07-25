package llm

import (
	"github.com/gin-gonic/gin"
)

func InitRoutes(engine *gin.Engine) {
	group := engine.Group("/api/:endpoint_token/v1")
	group.Use(ProjectAuthMiddleware())
	group.POST("/chat/completions", ChatCompletions)
	group.POST("/completions", Completions)
	group.POST("/:vram_limit/chat/completions", ChatCompletions)
	group.POST("/:vram_limit/completions", Completions)
	group.GET("/models", Models)
	group.GET("/models/*model", RetrieveModel)
}
