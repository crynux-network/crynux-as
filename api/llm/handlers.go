package llm

import (
	"crynux_as/models"

	"github.com/gin-gonic/gin"
)

func ChatCompletions(c *gin.Context) {
	handleLLMJobRequest(c, models.LLMAPITypeChatCompletions)
}

func Completions(c *gin.Context) {
	handleLLMJobRequest(c, models.LLMAPITypeCompletions)
}
