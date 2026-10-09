package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/relay"
	"crynux_as/service/huggingface"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateTaskModelRejectsURL(t *testing.T) {
	err := ValidateTaskModel(context.Background(), huggingface.ModelKindLLM, "https://huggingface.co/org/model", "")
	if !errors.Is(err, ErrInvalidBaseModel) {
		t.Fatalf("expected ErrInvalidBaseModel, got %v", err)
	}
}

func TestValidateTaskModelSkipsHubWhenCached(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.MinCatalogOnDiskNodeCount = 3
	cfg.HuggingFace.APIBaseURL = "http://127.0.0.1:1"
	cfg.HuggingFace.TimeoutSeconds = 1
	cfg.HuggingFace.CacheTTLSeconds = 60
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		SetHuggingFaceClientForTest(nil)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "success",
			"data": []map[string]interface{}{
				{"model_id": "cached/llm", "model_type": "llm", "variant": "", "min_vram": 24, "in_memory_node_count": 1, "on_disk_node_count": 1},
			},
		})
	}))
	defer server.Close()

	InitLoadedModelsCache(relay.NewClient(server.URL))
	if err := RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("hub must not be called for cached model: %s", r.URL.Path)
	}))
	defer hub.Close()
	SetHuggingFaceClientForTest(huggingface.NewClient(hub.URL, time.Second, time.Minute))

	if err := ValidateTaskModel(context.Background(), huggingface.ModelKindLLM, "cached/llm", ""); err != nil {
		t.Fatalf("expected cached model to pass: %v", err)
	}
}

func TestValidateTaskModelUsesHubForUnknown(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.MinCatalogOnDiskNodeCount = 3
	cfg.HuggingFace.APIBaseURL = "https://huggingface.co"
	cfg.HuggingFace.TimeoutSeconds = 5
	cfg.HuggingFace.CacheTTLSeconds = 300
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		SetHuggingFaceClientForTest(nil)
	})

	loaded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "success",
			"data":    []map[string]interface{}{},
		})
	}))
	defer loaded.Close()
	InitLoadedModelsCache(relay.NewClient(loaded.URL))
	if err := RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/models/org/new" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"siblings": []map[string]string{{"rfilename": "config.json"}},
		})
	}))
	defer hub.Close()
	SetHuggingFaceClientForTest(huggingface.NewClient(hub.URL, time.Second, time.Minute))

	if err := ValidateTaskModel(context.Background(), huggingface.ModelKindLLM, "org/new", ""); err != nil {
		t.Fatalf("expected hub model to pass: %v", err)
	}
}
