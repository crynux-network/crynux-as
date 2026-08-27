package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
)

const llmTaskType = 1

type RawTaskClient struct {
	*Client
}

func NewRawTaskClient(baseURL, apiKey string) *RawTaskClient {
	return &RawTaskClient{Client: NewClient(baseURL, apiKey)}
}

type CreateRawTaskRequest struct {
	TaskArgs string `json:"task_args"`
	TaskType int    `json:"task_type"`
	MinVram  uint64 `json:"min_vram,omitempty"`
	TaskFee  string `json:"task_fee"`
}

type RawClientTask struct {
	ID        uint   `json:"id"`
	ClientID  uint   `json:"client_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type createRawTaskEnvelope struct {
	Message string         `json:"message"`
	Data    *RawClientTask `json:"data"`
}

type InferenceTaskStatus struct {
	Status      int    `json:"status"`
	TaskType    int    `json:"task_type"`
	AbortReason int    `json:"abort_reason"`
	TaskError   int    `json:"task_error"`
	TaskID      string `json:"task_id"`
}

type getRawTaskEnvelope struct {
	Message string               `json:"message"`
	Data    *InferenceTaskStatus `json:"data"`
}

func (c *RawTaskClient) CreateLLMTask(ctx context.Context, taskArgs string, minVram uint64, taskFeeWei *big.Int) (*RawClientTask, error) {
	if taskFeeWei == nil {
		return nil, fmt.Errorf("task fee is required")
	}
	if taskFeeWei.Sign() < 0 {
		return nil, fmt.Errorf("task fee must be non-negative")
	}
	body, err := json.Marshal(CreateRawTaskRequest{
		TaskArgs: taskArgs,
		TaskType: llmTaskType,
		MinVram:  minVram,
		TaskFee:  taskFeeWei.String(),
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.post(ctx, "/v1/inference_tasks", body)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	respBody, err := resp.ReadAll()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bridge create task failed: %s", string(respBody))
	}

	var envelope createRawTaskEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("bridge create task returned empty data")
	}
	return envelope.Data, nil
}

func (c *RawTaskClient) GetTaskStatus(ctx context.Context, clientTaskID uint) (*InferenceTaskStatus, error) {
	url := fmt.Sprintf("%s/v1/inference_tasks/%d", c.baseURL, clientTaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bridge get task failed: %s", string(respBody))
	}

	var envelope getRawTaskEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("bridge get task returned empty data")
	}
	return envelope.Data, nil
}

func (c *RawTaskClient) DownloadLLMResult(ctx context.Context, clientTaskID uint) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/inference_tasks/%d/llm", c.baseURL, clientTaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bridge download result failed: %s", string(respBody))
	}
	return respBody, nil
}

const (
	BridgeTaskResultDownloaded = 11
	BridgeTaskEndAborted       = 7
	BridgeTaskEndInvalidated   = 9
)

func IsBridgeTaskTerminal(status int) bool {
	return status == BridgeTaskResultDownloaded ||
		status == BridgeTaskEndAborted ||
		status == BridgeTaskEndInvalidated
}

func IsBridgeTaskSuccess(status int) bool {
	return status == BridgeTaskResultDownloaded
}
