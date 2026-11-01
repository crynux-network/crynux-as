package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// LimitRequestBody caps the request body size for all routes, including Gin
// handlers that read the body outside the Tonic binding hook.
func LimitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
