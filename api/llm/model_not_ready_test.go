package llm

import (
	"crynux_as/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWriteModelNotReadyErrorShape(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeModelNotReadyError(c, gin.H{"id": "resp_test"})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "resp_test" {
		t.Fatalf("id=%v", body["id"])
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["type"] != "invalid_request_error" || errObj["code"] != "model_not_ready" || errObj["param"] != "model" {
		t.Fatalf("error=%v", errObj)
	}
	msg, _ := errObj["message"].(string)
	if msg == "" || msg[:10] != "This model" {
		t.Fatalf("message=%q", msg)
	}
}
