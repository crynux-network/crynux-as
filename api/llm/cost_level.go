package llm

import (
	"crynux_as/config"
	"crynux_as/service"
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type updateCostLevelRequest struct {
	CostLevelMode         *string `json:"cost_level_mode"`
	PriorityGwei          *string `json:"priority_gwei"`
	AutoQueuePosition     *int    `json:"auto_queue_position"`
	AutoMaxCreditsPerTask *string `json:"auto_max_credits_per_task"`
}

// GetCostLevel returns the authenticated project's Cost Level settings.
func GetCostLevel(c *gin.Context) {
	project := GetProject(c)
	if project == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "project is not authenticated"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    service.ProjectCostLevelViewFromProject(project),
	})
}

// PutCostLevel updates the authenticated project's Cost Level settings.
func PutCostLevel(c *gin.Context) {
	project := GetProject(c)
	if project == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "project is not authenticated"})
		return
	}

	var body updateCostLevelRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "validation error",
			"data":    gin.H{"body": "request body must be a JSON object"},
		})
		return
	}

	updated, fieldErr, err := service.UpdateProjectCostLevelAndSave(
		c.Request.Context(),
		config.GetDB(),
		project,
		service.ProjectCostLevelUpdate{
			CostLevelMode:         body.CostLevelMode,
			PriorityGwei:          body.PriorityGwei,
			AutoQueuePosition:     body.AutoQueuePosition,
			AutoMaxCreditsPerTask: body.AutoMaxCreditsPerTask,
		},
	)
	if fieldErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "validation error",
			"data":    gin.H{fieldErr.Field: fieldErr.Message},
		})
		return
	}
	if err != nil {
		log.Errorf("Error updating project %d cost level: %v", project.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    service.ProjectCostLevelViewFromProject(updated),
	})
}

// GetCostLevelHints returns Crynux Network Cost Level reasonableness fields.
func GetCostLevelHints(c *gin.Context) {
	if GetProject(c) == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "project is not authenticated"})
		return
	}
	hints, err := service.BuildCostLevelHints()
	if err != nil {
		log.Errorf("Error building cost level hints: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    hints,
	})
}
