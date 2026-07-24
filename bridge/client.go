package bridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client forwards LLM requests to the Crynux Bridge using the platform API key.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// ChatCompletions POSTs to /v1/llm/chat/completions.
func (c *Client) ChatCompletions(ctx context.Context, body []byte) (*Response, error) {
	return c.post(ctx, "/v1/llm/chat/completions", body)
}

// Completions POSTs to /v1/llm/completions.
func (c *Client) Completions(ctx context.Context, body []byte) (*Response, error) {
	return c.post(ctx, "/v1/llm/completions", body)
}

func (c *Client) post(ctx context.Context, path string, body []byte) (*Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       resp.Body,
	}, nil
}

func (r *Response) ReadAll() ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func (r *Response) Close() error {
	if r == nil || r.Body == nil {
		return nil
	}
	return r.Body.Close()
}

func (r *Response) ErrorMessage(body []byte) string {
	if len(body) == 0 {
		return fmt.Sprintf("bridge request failed with status %d", r.StatusCode)
	}
	return string(body)
}
