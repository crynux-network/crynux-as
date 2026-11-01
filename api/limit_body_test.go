package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLimitRequestBodyRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(LimitRequestBody(16))
	engine.POST("/echo", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				c.Status(http.StatusRequestEntityTooLarge)
				return
			}
			c.Status(http.StatusBadRequest)
			return
		}
		c.Data(http.StatusOK, "application/octet-stream", body)
	})

	withinLimit := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader([]byte("0123456789abcdef")))
	withinLimit.Header.Set("Content-Type", "application/octet-stream")
	withinRecorder := httptest.NewRecorder()
	engine.ServeHTTP(withinRecorder, withinLimit)
	if withinRecorder.Code != http.StatusOK {
		t.Fatalf("within-limit status = %d, want %d", withinRecorder.Code, http.StatusOK)
	}
	if got := withinRecorder.Body.String(); got != "0123456789abcdef" {
		t.Fatalf("within-limit body = %q, want 16 bytes echoed", got)
	}

	overLimit := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader([]byte("0123456789abcdefX")))
	overLimit.Header.Set("Content-Type", "application/octet-stream")
	overRecorder := httptest.NewRecorder()
	engine.ServeHTTP(overRecorder, overLimit)
	if overRecorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit status = %d, want %d", overRecorder.Code, http.StatusRequestEntityTooLarge)
	}
}
