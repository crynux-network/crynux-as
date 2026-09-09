package llm

import (
	"crynux_as/api/llm/responses"

	"github.com/gin-gonic/gin"
)

func InitRoutes(engine *gin.Engine) {
	group := engine.Group("/api/:endpoint_token/v1")
	group.Use(ProjectAuthMiddleware())
	group.POST("/chat/completions", ChatCompletions)
	group.POST("/completions", Completions)
	group.POST("/:vram_limit/chat/completions", ChatCompletions)
	group.POST("/:vram_limit/completions", Completions)
	group.POST("/responses", responses.CreateResponse)
	group.POST("/:vram_limit/responses", responses.CreateResponse)
	group.GET("/responses/:response_id", responses.GetResponse)
	group.GET("/models", Models)
	group.GET("/models/*model", RetrieveModel)
}
