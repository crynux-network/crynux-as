package llm

import (
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
