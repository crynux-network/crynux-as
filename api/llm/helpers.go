package llm

import (
	"crynux_as/service"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func writeClientError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeModelNotReadyError(c *gin.Context, extra gin.H) {
	body := gin.H{
		"error": gin.H{
			"message": service.ModelNotReadyMessage(),
			"type":    "invalid_request_error",
			"code":    "model_not_ready",
			"param":   "model",
		},
	}
	for key, value := range extra {
		body[key] = value
	}
	c.JSON(http.StatusBadRequest, body)
}

func writeRequestBodyReadError(c *gin.Context, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeClientError(c, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeClientError(c, http.StatusBadRequest, "failed to read request body")
}

func writeServerError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "internal server error",
			"type":    "server_error",
		},
	})
}
