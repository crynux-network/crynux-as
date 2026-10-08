package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
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

type QueuedTaskPriority struct {
	AsOf                int64
	QueuedTaskCount     int64
	HighestPriorityGwei *big.Int
	MedianPriorityGwei  *big.Int
	LowestPriorityGwei  *big.Int
}

type queuedTaskPriorityData struct {
	AsOf                int64   `json:"as_of"`
	QueuedTaskCount     int64   `json:"queued_task_count"`
	HighestPriorityGwei *string `json:"highest_priority_gwei"`
	MedianPriorityGwei  *string `json:"median_priority_gwei"`
	LowestPriorityGwei  *string `json:"lowest_priority_gwei"`
}

type getQueuedTaskPriorityResponse struct {
	Message string                 `json:"message"`
	Data    queuedTaskPriorityData `json:"data"`
}

// GetQueuedTaskPriority fetches the queued-task priority snapshot from
// GET /v2/tasks/queued/priority.
func (c *Client) GetQueuedTaskPriority(ctx context.Context) (*QueuedTaskPriority, error) {
	reqURL := c.baseURL + "/v2/tasks/queued/priority"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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
		return nil, fmt.Errorf("relay queued-priority request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var parsed getQueuedTaskPriorityResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse relay queued-priority response: %w", err)
	}

	result := &QueuedTaskPriority{
		AsOf:            parsed.Data.AsOf,
		QueuedTaskCount: parsed.Data.QueuedTaskCount,
	}
	if result.HighestPriorityGwei, err = parseOptionalBigIntString(parsed.Data.HighestPriorityGwei); err != nil {
		return nil, fmt.Errorf("invalid highest_priority_gwei: %w", err)
	}
	if result.MedianPriorityGwei, err = parseOptionalBigIntString(parsed.Data.MedianPriorityGwei); err != nil {
		return nil, fmt.Errorf("invalid median_priority_gwei: %w", err)
	}
	if result.LowestPriorityGwei, err = parseOptionalBigIntString(parsed.Data.LowestPriorityGwei); err != nil {
		return nil, fmt.Errorf("invalid lowest_priority_gwei: %w", err)
	}
	return result, nil
}

type LLMExecutionTime struct {
	ConstantSeconds       float64 `json:"constant_seconds"`
	SecondsPerInputToken  float64 `json:"seconds_per_input_token"`
	SecondsPerOutputToken float64 `json:"seconds_per_output_token"`
	ModelSwitchSeconds    float64 `json:"model_switch_seconds"`
	SecondsPerImage       float64 `json:"seconds_per_image"`
	SecondsPerMegapixel   float64 `json:"seconds_per_megapixel"`
}

type SDExecutionTime struct {
	OverheadSeconds       float64 `json:"overhead_seconds"`
	SecondsPerSDPixelStep float64 `json:"seconds_per_sd_pixel_step"`
}

type ExecutionTimeQuery struct {
	Model        string
	Dtype        string
	QuantizeBits *uint64
	Variant      string
	MinVRAM      *uint64
	GPUName      string
	GPUVRAM      *uint64
}

type getLLMExecutionTimeResponse struct {
	Message string           `json:"message"`
	Data    LLMExecutionTime `json:"data"`
}

type getSDExecutionTimeResponse struct {
	Message string          `json:"message"`
	Data    SDExecutionTime `json:"data"`
}

// GetLLMExecutionTime fetches LLM execution-time coefficients from
// GET /v2/models/llm/execution-time with model and min_vram selection.
func (c *Client) GetLLMExecutionTime(ctx context.Context, model string, minVRAM uint64) (*LLMExecutionTime, error) {
	query := ExecutionTimeQuery{Model: model, MinVRAM: &minVRAM}
	return c.GetLLMExecutionTimeWithQuery(ctx, query)
}

func (c *Client) GetLLMExecutionTimeWithQuery(ctx context.Context, query ExecutionTimeQuery) (*LLMExecutionTime, error) {
	reqURL, err := c.executionTimeURL("/v2/models/llm/execution-time", query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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
		return nil, fmt.Errorf("relay llm execution-time request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var parsed getLLMExecutionTimeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse relay llm execution-time response: %w", err)
	}
	return &parsed.Data, nil
}

func (c *Client) GetSDExecutionTime(ctx context.Context, query ExecutionTimeQuery) (*SDExecutionTime, error) {
	reqURL, err := c.executionTimeURL("/v2/models/sd/execution-time", query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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
		return nil, fmt.Errorf("relay sd execution-time request failed with status %d: %s", resp.StatusCode, string(body))
	}
	var parsed getSDExecutionTimeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse relay sd execution-time response: %w", err)
	}
	return &parsed.Data, nil
}

func (c *Client) executionTimeURL(path string, query ExecutionTimeQuery) (string, error) {
	if strings.TrimSpace(query.Model) == "" {
		return "", fmt.Errorf("model is required")
	}
	hasMinVRAM := query.MinVRAM != nil
	hasGPUName := strings.TrimSpace(query.GPUName) != ""
	hasGPUVRAM := query.GPUVRAM != nil
	if hasMinVRAM == (hasGPUName || hasGPUVRAM) {
		return "", fmt.Errorf("provide either min_vram or gpu_name and gpu_vram")
	}
	if hasGPUName != hasGPUVRAM {
		return "", fmt.Errorf("gpu_name and gpu_vram must be provided together")
	}
	values := url.Values{}
	values.Set("model", query.Model)
	if query.Dtype != "" {
		values.Set("dtype", query.Dtype)
	}
	if query.QuantizeBits != nil {
		values.Set("quantize_bits", strconv.FormatUint(*query.QuantizeBits, 10))
	}
	if query.Variant != "" {
		values.Set("variant", query.Variant)
	}
	if hasMinVRAM {
		if *query.MinVRAM == 0 {
			return "", fmt.Errorf("min_vram must be a positive integer")
		}
		values.Set("min_vram", strconv.FormatUint(*query.MinVRAM, 10))
	} else {
		if *query.GPUVRAM == 0 {
			return "", fmt.Errorf("gpu_vram must be a positive integer")
		}
		values.Set("gpu_name", query.GPUName)
		values.Set("gpu_vram", strconv.FormatUint(*query.GPUVRAM, 10))
	}
	return c.baseURL + path + "?" + values.Encode(), nil
}

func parseOptionalBigIntString(raw *string) (*big.Int, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty string")
	}
	value, ok := new(big.Int).SetString(trimmed, 10)
	if !ok {
		return nil, fmt.Errorf("not a valid integer: %s", trimmed)
	}
	if value.Sign() < 0 {
		return nil, fmt.Errorf("must be non-negative: %s", trimmed)
	}
	return value, nil
}
