package llm

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestInitRoutesRegistersWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("route registration panicked: %v", r)
		}
	}()
	InitRoutes(gin.New())
}
