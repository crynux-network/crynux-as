package service

import (
	"context"
	"crynux_as/relay"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadedModelCachesSeparateLLMAndImageModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "success",
			"data": []map[string]interface{}{
				{"model_id": "z/image", "model_type": "sd", "min_vram": 16, "in_memory_node_count": 2},
				{"model_id": "a/llm", "model_type": "llm", "min_vram": 24, "in_memory_node_count": 3},
				{"model_id": "a/image", "model_type": "sd", "min_vram": 8, "in_memory_node_count": 1},
			},
		})
	}))
	defer server.Close()

	InitLoadedModelsCache(relay.NewClient(server.URL))
	if err := RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 1 || llmModels[0].ModelID != "a/llm" {
		t.Fatalf("LLM models = %+v", llmModels)
	}
	imageModels := ListLoadedSDModels()
	if len(imageModels) != 2 || imageModels[0].ModelID != "a/image" || imageModels[1].ModelID != "z/image" {
		t.Fatalf("image models = %+v", imageModels)
	}
	if _, ok := GetLoadedLLMModel("a/image"); ok {
		t.Fatal("image model leaked into LLM cache")
	}
	if _, ok := GetLoadedSDModel("a/llm"); ok {
		t.Fatal("LLM model leaked into image cache")
	}
}
