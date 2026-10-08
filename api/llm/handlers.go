package llm

import (
	"crynux_as/models"

	"github.com/gin-gonic/gin"
)

func ChatCompletions(c *gin.Context) {
	handleTaskJobRequest(c, models.TaskAPITypeChatCompletions)
}

func Completions(c *gin.Context) {
	handleTaskJobRequest(c, models.TaskAPITypeCompletions)
}
