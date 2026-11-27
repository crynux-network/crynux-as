package result

import "github.com/gin-gonic/gin"

func InitRoutes(engine *gin.Engine) {
	group := engine.Group("/api/result")
	group.GET("/:result_download_hash/images/:index", DownloadImage)
	group.GET("/:result_download_hash/video", DownloadVideo)
}
