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

func TestNewProviderHTTPClientUsesConnectAndHeaderTimeouts(t *testing.T) {
	cfg := config.ProviderServiceConfig{
		ConnectTimeoutSeconds:        7,
		ResponseHeaderTimeoutSeconds: 11,
	}
	client := newProviderHTTPClient(cfg)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != 11*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want 11s", transport.ResponseHeaderTimeout)
	}
	if client.Timeout != 0 {
		t.Fatalf("Client.Timeout = %v, want 0 so streaming is not capped by total request timeout", client.Timeout)
	}
	if transport.TLSHandshakeTimeout != 7*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want 7s", transport.TLSHandshakeTimeout)
	}
	dialer := providerDialer(cfg)
	if dialer.Timeout != 7*time.Second {
		t.Fatalf("Dialer.Timeout = %v, want 7s", dialer.Timeout)
	}
}

func TestStreamIdleTimeoutClosesHungBody(t *testing.T) {
	pr, pw := io.Pipe()
	reader, stop := withStreamIdleTimeoutDuration(pr, 50*time.Millisecond)
	defer stop()
	defer pw.Close()

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := reader.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected idle close error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("idle timeout did not unblock read")
	}
}

func TestStreamIdleTimeoutResetsOnRead(t *testing.T) {
	pr, pw := io.Pipe()
	reader, stop := withStreamIdleTimeoutDuration(pr, 80*time.Millisecond)
	defer stop()

	go func() {
		_, _ = pw.Write([]byte("a"))
		time.Sleep(40 * time.Millisecond)
		_, _ = pw.Write([]byte("b"))
		time.Sleep(40 * time.Millisecond)
		_, _ = pw.Write([]byte("c"))
		_ = pw.Close()
	}()

	buf := make([]byte, 8)
	var got strings.Builder
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}
	if got.String() != "abc" {
		t.Fatalf("got %q, want abc", got.String())
	}
}

func TestOpenAIAdapterNonStreamRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		ConnectTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		StreamIdleTimeoutSeconds:           5,
		RequestTimeoutSeconds:              1,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)

	started := time.Now()
	exec, err := adapter.Execute(
		context.Background(),
		[]byte("secret"),
		models.TaskAPITypeChatCompletions,
		"gpt-4o-mini",
		[]byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		1,
		0,
		nil,
	)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if exec.ErrorClass != ErrorAcceptanceUncertain {
		t.Fatalf("ErrorClass = %s, want %s", exec.ErrorClass, ErrorAcceptanceUncertain)
	}
	if exec.ErrorMessage != "request cancelled or timed out after send" {
		t.Fatalf("ErrorMessage = %q", exec.ErrorMessage)
	}
	if elapsed < 900*time.Millisecond || elapsed > 1800*time.Millisecond {
		t.Fatalf("elapsed = %v, want about 1s request timeout", elapsed)
	}
}

func TestOpenAIAdapterStreamIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "upstream-1")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter is not a Flusher")
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n")
		flusher.Flush()
		time.Sleep(3 * time.Second)
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		ConnectTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		StreamIdleTimeoutSeconds:           1,
		RequestTimeoutSeconds:              30,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)

	started := time.Now()
	exec, err := adapter.Execute(
		context.Background(),
		[]byte("secret"),
		models.TaskAPITypeChatCompletions,
		"gpt-4o-mini",
		[]byte(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`),
		1,
		0,
		nil,
	)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if exec.ErrorClass != ErrorStreamInterrupted {
		t.Fatalf("ErrorClass = %s, want %s msg=%q", exec.ErrorClass, ErrorStreamInterrupted, exec.ErrorMessage)
	}
	if exec.Acceptance != AcceptanceUncertain {
		t.Fatalf("Acceptance = %s, want %s", exec.Acceptance, AcceptanceUncertain)
	}
	if !exec.PublicOutputBegan {
		t.Fatal("PublicOutputBegan = false, want true after content delta")
	}
	if elapsed < 900*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Fatalf("elapsed = %v, want about 1s stream idle timeout", elapsed)
	}
}

func TestOpenAIAdapterStreamIgnoresTotalRequestTimeoutWhileActive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"A"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"B"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			_, _ = io.WriteString(w, chunk+"\n\n")
			flusher.Flush()
			time.Sleep(400 * time.Millisecond)
		}
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		ConnectTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		StreamIdleTimeoutSeconds:           2,
		RequestTimeoutSeconds:              1,
		CredentialValidationTimeoutSeconds: 1,
	}, 2048)

	exec, err := adapter.Execute(
		context.Background(),
		[]byte("secret"),
		models.TaskAPITypeChatCompletions,
		"gpt-4o-mini",
		[]byte(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`),
		1,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if exec.Acceptance != AcceptanceAcceptedBillable {
		t.Fatalf("Acceptance = %s msg=%q class=%s", exec.Acceptance, exec.ErrorMessage, exec.ErrorClass)
	}
}
