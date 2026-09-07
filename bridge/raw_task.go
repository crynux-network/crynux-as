package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
)

const llmTaskType = 1

const (
	ClientTaskStatusRunning = "running"
	ClientTaskStatusSuccess = "success"
	ClientTaskStatusFailed  = "failed"
)

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

type getRawTaskEnvelope struct {
	Message string         `json:"message"`
	Data    *RawClientTask `json:"data"`
}

type BatchCreateRawTaskRequest struct {
	Tasks []CreateRawTaskRequest `json:"tasks"`
}

type BatchCreateRawTaskItemResult struct {
	Index      int            `json:"index"`
	ClientTask *RawClientTask `json:"client_task,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type batchCreateRawTaskEnvelope struct {
	Message string                          `json:"message"`
	Data    []BatchCreateRawTaskItemResult  `json:"data"`
}

type BatchGetRawTaskStatusRequest struct {
	ClientTaskIDs []uint `json:"client_task_ids"`
}

type BatchGetRawTaskStatusItemResult struct {
	ClientTaskID uint           `json:"client_task_id"`
	ClientTask   *RawClientTask `json:"client_task,omitempty"`
	Error        string         `json:"error,omitempty"`
}

type batchGetRawTaskStatusEnvelope struct {
	Message string                             `json:"message"`
	Data    []BatchGetRawTaskStatusItemResult  `json:"data"`
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

func (c *RawTaskClient) CreateLLMTasks(ctx context.Context, tasks []CreateRawTaskRequest) ([]BatchCreateRawTaskItemResult, error) {
	body, err := json.Marshal(BatchCreateRawTaskRequest{Tasks: tasks})
	if err != nil {
		return nil, err
	}

	resp, err := c.post(ctx, "/v1/inference_tasks/batch", body)
	if err != nil {
		return nil, err
	}
	defer resp.Close()

	respBody, err := resp.ReadAll()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bridge batch create task failed: %s", string(respBody))
	}

	var envelope batchCreateRawTaskEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("bridge batch create task returned empty data")
	}
	return envelope.Data, nil
}

func (c *RawTaskClient) GetTaskStatus(ctx context.Context, clientTaskID uint) (*RawClientTask, error) {
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

func (c *RawTaskClient) GetTaskStatuses(ctx context.Context, clientTaskIDs []uint) ([]BatchGetRawTaskStatusItemResult, error) {
	body, err := json.Marshal(BatchGetRawTaskStatusRequest{ClientTaskIDs: clientTaskIDs})
	if err != nil {
		return nil, err
	}

	url := c.baseURL + "/v1/inference_tasks/batch/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
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
		return nil, fmt.Errorf("bridge batch get task status failed: %s", string(respBody))
	}

	var envelope batchGetRawTaskStatusEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("bridge batch get task status returned empty data")
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

func IsBridgeTaskTerminal(status string) bool {
	return status == ClientTaskStatusSuccess || status == ClientTaskStatusFailed
}

func IsBridgeTaskSuccess(status string) bool {
	return status == ClientTaskStatusSuccess
}
