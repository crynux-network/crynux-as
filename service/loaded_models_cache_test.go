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
				{"model_id": "z/image", "model_type": "sd", "variant": "fp16", "min_vram": 16, "in_memory_node_count": 2},
				{"model_id": "a/llm", "model_type": "llm", "variant": "", "min_vram": 24, "in_memory_node_count": 3},
				{"model_id": "a/image", "model_type": "sd", "variant": "", "min_vram": 8, "in_memory_node_count": 1},
				{"model_id": "a/image", "model_type": "sd", "variant": "fp16", "min_vram": 12, "in_memory_node_count": 4},
				{"model_id": "b/llm", "model_type": "llm", "variant": "fp16", "min_vram": 99, "in_memory_node_count": 1},
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
	if len(imageModels) != 3 {
		t.Fatalf("image models = %+v", imageModels)
	}
	if imageModels[0].ModelID != "a/image" || imageModels[0].Variant != "" {
		t.Fatalf("first image model = %+v", imageModels[0])
	}
	if imageModels[1].ModelID != "a/image" || imageModels[1].Variant != "fp16" || imageModels[1].MinVRAM != 12 {
		t.Fatalf("second image model = %+v", imageModels[1])
	}
	if imageModels[2].ModelID != "z/image" || imageModels[2].Variant != "fp16" {
		t.Fatalf("third image model = %+v", imageModels[2])
	}
	if _, ok := GetLoadedLLMModel("a/image"); ok {
		t.Fatal("image model leaked into LLM cache")
	}
	if _, ok := GetLoadedSDModel("a/llm", ""); ok {
		t.Fatal("LLM model leaked into image cache")
	}
	got, ok := GetLoadedSDModel("A/Image", "FP16")
	if !ok || got.MinVRAM != 12 {
		t.Fatalf("GetLoadedSDModel by variant = %+v %v", got, ok)
	}
	got, ok = GetLoadedSDModel("a/image", "")
	if !ok || got.MinVRAM != 8 {
		t.Fatalf("GetLoadedSDModel empty variant = %+v %v", got, ok)
	}
}
