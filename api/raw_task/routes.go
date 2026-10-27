package raw_task

import "github.com/gin-gonic/gin"

func InitRoutes(group *gin.RouterGroup) {
	group.POST("/inference_tasks", CreateTask)
	group.GET("/inference_tasks/:client_task_id", GetTask)
	group.POST("/inference_tasks/:client_task_id/abort", AbortTask)
	group.GET("/inference_tasks/:client_task_id/llm", DownloadLLM)
	group.GET("/inference_tasks/:client_task_id/images/:index", DownloadImage)
	group.POST("/inference_tasks/batch", BatchCreate)
	group.POST("/inference_tasks/batch/status", BatchStatus)
	group.POST("/inference_tasks/batch/abort", BatchAbort)
}
