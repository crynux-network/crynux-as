package llm

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/utils"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const projectContextKey = "llm_project"

func ProjectAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		endpointToken := c.Param("endpoint_token")
		if endpointToken == "" {
			writeAuthError(c, "missing endpoint token")
			c.Abort()
			return
		}

		authHeader := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeAuthError(c, "missing or invalid authorization header")
			c.Abort()
			return
		}
		apiKey := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
		if apiKey == "" {
			writeAuthError(c, "missing or invalid authorization header")
			c.Abort()
			return
		}

		db := config.GetDB()
		dbCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var project models.Project
		if err := db.WithContext(dbCtx).
			Where("endpoint_token = ?", endpointToken).
			First(&project).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeAuthError(c, "invalid endpoint token or api key")
				c.Abort()
				return
			}
			log.Errorf("Error loading project by endpoint token: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"message": "internal server error",
					"type":    "server_error",
				},
			})
			return
		}

		if project.Status != models.ProjectStatusActive {
			writeAuthError(c, "project is disabled")
			c.Abort()
			return
		}

		if utils.HashToken(apiKey) != project.APIKeyHash {
			writeAuthError(c, "invalid endpoint token or api key")
			c.Abort()
			return
		}

		c.Set(projectContextKey, &project)
		c.Next()
	}
}

func GetProject(c *gin.Context) *models.Project {
	v, ok := c.Get(projectContextKey)
	if !ok {
		return nil
	}
	project, _ := v.(*models.Project)
	return project
}

func writeAuthError(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}
