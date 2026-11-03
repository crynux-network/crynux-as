package provider

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIImagesUsageWithoutOutputDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"created": 1,
			"data":[{"b64_json":"`+testPNG1B64+`"}],
			"usage":{
				"input_tokens":50,
				"output_tokens":272,
				"total_tokens":322,
				"input_tokens_details":{"text_tokens":50,"image_tokens":0}
			}
		}`)
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		ConnectTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		StreamIdleTimeoutSeconds:           5,
		RequestTimeoutSeconds:              5,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)
	result, err := adapter.Execute(
		context.Background(),
		[]byte("secret"),
		models.TaskAPITypeImagesGenerations,
		"gpt-image-2.5-sunburst",
		[]byte(`{"model":"gpt-image-2.5-sunburst","prompt":"a cat","n":1,"size":"1024x1024"}`),
		1,
		0,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Acceptance != AcceptanceAcceptedBillable || result.FinalUsage == nil {
		t.Fatalf("result=%+v", result)
	}
	if result.FinalUsage.OutputTokens != 0 {
		t.Fatalf("OutputTokens=%d, want 0", result.FinalUsage.OutputTokens)
	}
	if result.FinalUsage.ImageOutputTokens != 272 {
		t.Fatalf("ImageOutputTokens=%d, want 272", result.FinalUsage.ImageOutputTokens)
	}
	if result.FinalUsage.InputTokens != 50 || result.FinalUsage.ImageInputTokens != 0 {
		t.Fatalf("input=%d image_input=%d", result.FinalUsage.InputTokens, result.FinalUsage.ImageInputTokens)
	}
}

func TestOpenAIImagesUsageWithOutputDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"created": 1,
			"data":[{"b64_json":"`+testPNG1B64+`"}],
			"usage":{
				"input_tokens":48,
				"output_tokens":481,
				"total_tokens":529,
				"input_tokens_details":{"text_tokens":48,"image_tokens":0},
				"output_tokens_details":{"image_tokens":272,"text_tokens":209}
			}
		}`)
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		ConnectTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		StreamIdleTimeoutSeconds:           5,
		RequestTimeoutSeconds:              5,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)
	result, err := adapter.Execute(
		context.Background(),
		[]byte("secret"),
		models.TaskAPITypeImagesGenerations,
		"gpt-image-2.5-sunburst",
		[]byte(`{"model":"gpt-image-2.5-sunburst","prompt":"a cat","n":1}`),
		1,
		0,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalUsage.ImageOutputTokens != 272 {
		t.Fatalf("ImageOutputTokens=%d", result.FinalUsage.ImageOutputTokens)
	}
	if result.FinalUsage.OutputTokens != 0 {
		t.Fatalf("OutputTokens=%d, want 0", result.FinalUsage.OutputTokens)
	}
}

func TestGoogleImagesUsageThoughtsAndImageModality(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"candidates":[{
				"content":{"parts":[
					{"thought":true,"inlineData":{"mimeType":"image/png","data":"`+testPNG1B64+`"}},
					{"inlineData":{"mimeType":"image/png","data":"`+testPNG1B64+`"}}
				]}
			}],
			"usageMetadata":{
				"promptTokenCount":12,
				"candidatesTokenCount":1300,
				"thoughtsTokenCount":40,
				"totalTokenCount":1352,
				"candidatesTokensDetails":[
					{"modality":"TEXT","tokenCount":180},
					{"modality":"IMAGE","tokenCount":1120}
				]
			}
		}`)
	}))
	defer server.Close()

	adapter := NewGoogleAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		ConnectTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		StreamIdleTimeoutSeconds:           5,
		RequestTimeoutSeconds:              5,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)
	result, err := adapter.Execute(
		context.Background(),
		[]byte("secret"),
		models.TaskAPITypeImagesGenerations,
		"gemini-3.1-flash-image",
		[]byte(`{"model":"gemini-3.1-flash-image","prompt":"a cat","n":1}`),
		1,
		0,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Acceptance != AcceptanceAcceptedBillable || result.FinalUsage == nil {
		payload, _ := json.Marshal(result)
		t.Fatalf("result=%s", payload)
	}
	if result.FinalUsage.ImageOutputTokens != 1120 {
		t.Fatalf("ImageOutputTokens=%d", result.FinalUsage.ImageOutputTokens)
	}
	if result.FinalUsage.ReasoningTokens != 40 {
		t.Fatalf("ReasoningTokens=%d", result.FinalUsage.ReasoningTokens)
	}
	if result.FinalUsage.OutputTokens != 0 {
		t.Fatalf("OutputTokens=%d", result.FinalUsage.OutputTokens)
	}
	if len(result.ImageOutputs) != 1 {
		t.Fatalf("images=%d", len(result.ImageOutputs))
	}
}
