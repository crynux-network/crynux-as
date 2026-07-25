package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type LoadedModel struct {
	ModelID           string `json:"model_id"`
	ModelType         string `json:"model_type"`
	MinVRAM           uint64 `json:"min_vram"`
	InMemoryNodeCount int64  `json:"in_memory_node_count"`
	OnDiskNodeCount   int64  `json:"on_disk_node_count"`
}

type getLoadedModelsResponse struct {
	Message string        `json:"message"`
	Data    []LoadedModel `json:"data"`
}

// GetLoadedModels fetches the loaded models from the public Relay API
// GET /v2/loaded-models.
func (c *Client) GetLoadedModels(ctx context.Context) ([]LoadedModel, error) {
	url := c.baseURL + "/v2/loaded-models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay loaded-models request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var parsed getLoadedModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse relay loaded-models response: %w", err)
	}
	return parsed.Data, nil
}
