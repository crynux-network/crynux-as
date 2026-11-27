package result

import (
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func DownloadImage(c *gin.Context) {
	index, err := strconv.ParseUint(c.Param("index"), 10, 64)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	downloadByHash(c, models.TaskTypeImage, index)
}

func DownloadVideo(c *gin.Context) {
	downloadByHash(c, models.TaskTypeVideo, 0)
}

func downloadByHash(c *gin.Context, expectedType models.TaskType, index uint64) {
	hash := strings.TrimSpace(c.Param("result_download_hash"))
	if hash == "" {
		c.Status(http.StatusNotFound)
		return
	}
	var job models.TaskJob
	err := config.GetDB().WithContext(c.Request.Context()).
		Where("result_download_hash = ?", hash).
		First(&job).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorf("lookup result download hash failed: %v", err)
		}
		c.Status(http.StatusNotFound)
		return
	}
	if job.TaskType != expectedType || job.Status != models.TaskJobStatusCompleted {
		c.Status(http.StatusNotFound)
		return
	}
	if expectedType == models.TaskTypeVideo {
		body, err := service.ReadVideoResultMP4(job.ID)
		if err != nil || len(body) == 0 {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Disposition", "attachment; filename=0.mp4")
		c.Data(http.StatusOK, "video/mp4", body)
		return
	}
	body, err := service.ReadImageResultPNG(job.ID, index)
	if err != nil || len(body) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+strconv.FormatUint(index, 10)+".png")
	c.Data(http.StatusOK, "image/png", body)
}
