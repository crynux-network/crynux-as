package provider

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGoogleAdapterSendsAPIKeyInHeader(t *testing.T) {
	const secret = "google-secret-key-value"
	var sawValidate, sawExecute bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "" {
			t.Errorf("request URL must not contain key query: %s", r.URL.String())
		}
		if got := r.Header.Get("x-goog-api-key"); got != secret {
			t.Errorf("x-goog-api-key = %q, want %q", got, secret)
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/v1beta/models"):
			sawValidate = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"models":[]}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, ":generateContent"):
			sawExecute = true
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{
				"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP"}],
				"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}
			}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	adapter := NewGoogleAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		RequestTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		CredentialValidationTimeoutSeconds: 5,
	}, 2048)
	result, err := adapter.ValidateCredential(context.Background(), []byte(secret))
	if err != nil {
		t.Fatalf("ValidateCredential error: %v", err)
	}
	if result != CredentialValid {
		t.Fatalf("ValidateCredential result = %v, want valid", result)
	}
	if !sawValidate {
		t.Fatal("expected credential validation request")
	}

	exec, err := adapter.Execute(
		context.Background(),
		[]byte(secret),
		models.TaskAPITypeChatCompletions,
		"gemini-2.0-flash",
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if exec.ErrorMessage != "" {
		t.Fatalf("Execute ErrorMessage = %q, want empty", exec.ErrorMessage)
	}
	if !sawExecute {
		t.Fatal("expected generateContent request")
	}
}

func TestGoogleAdapterTransportErrorOmitsSecret(t *testing.T) {
	const secret = "google-secret-key-value"
	adapter := NewGoogleAdapter(config.ProviderServiceConfig{
		BaseURL:                            "http://127.0.0.1:1",
		RequestTimeoutSeconds:              1,
		ResponseHeaderTimeoutSeconds:       1,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)

	result, err := adapter.ValidateCredential(context.Background(), []byte(secret))
	if err != nil {
		t.Fatalf("ValidateCredential must not return transport error: %v", err)
	}
	if result != CredentialTemporarilyUnavailable {
		t.Fatalf("ValidateCredential result = %v, want temporarily unavailable", result)
	}

	exec, err := adapter.Execute(
		context.Background(),
		[]byte(secret),
		models.TaskAPITypeChatCompletions,
		"gemini-2.0-flash",
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.Contains(exec.ErrorMessage, secret) {
		t.Fatalf("ErrorMessage must not contain secret: %q", exec.ErrorMessage)
	}
	if strings.Contains(exec.ErrorMessage, "key=") {
		t.Fatalf("ErrorMessage must not contain key query: %q", exec.ErrorMessage)
	}
	if exec.ErrorMessage != "provider connection failed" {
		t.Fatalf("ErrorMessage = %q, want provider connection failed", exec.ErrorMessage)
	}
}

func TestGoogleAdapterClientTimeoutOmitsSecret(t *testing.T) {
	const secret = "google-secret-key-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "" {
			t.Errorf("request URL must not contain key query: %s", r.URL.String())
		}
		if got := r.Header.Get("x-goog-api-key"); got != secret {
			t.Errorf("x-goog-api-key = %q, want %q", got, secret)
		}
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewGoogleAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		RequestTimeoutSeconds:              0,
		ResponseHeaderTimeoutSeconds:       0,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)
	adapter.client.Timeout = 50 * time.Millisecond

	exec, err := adapter.Execute(
		context.Background(),
		[]byte(secret),
		models.TaskAPITypeChatCompletions,
		"gemini-2.0-flash",
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.Contains(exec.ErrorMessage, secret) {
		t.Fatalf("ErrorMessage must not contain secret: %q", exec.ErrorMessage)
	}
	if exec.ErrorMessage != "provider connection failed" {
		t.Fatalf("ErrorMessage = %q, want provider connection failed", exec.ErrorMessage)
	}
}
