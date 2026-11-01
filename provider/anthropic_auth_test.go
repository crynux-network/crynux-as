package provider

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"strings"
	"testing"
	"time"
)

func TestAnthropicAdapterTransportErrorOmitsSecret(t *testing.T) {
	const secret = "anthropic-secret-key-value"
	adapter := NewAnthropicAdapter(config.ProviderServiceConfig{
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
		"claude-sonnet-4-5",
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

func TestAnthropicAdapterClientTimeoutOmitsSecret(t *testing.T) {
	const secret = "anthropic-secret-key-value"
	server := slowAuthHeaderServer(t, "x-api-key", secret)
	defer server.Close()

	adapter := NewAnthropicAdapter(config.ProviderServiceConfig{
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
		"claude-sonnet-4-5",
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
