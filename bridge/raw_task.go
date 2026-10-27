package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
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
	TaskArgs        string  `json:"task_args"`
	TaskType        int     `json:"task_type"`
	TaskVersion     *string `json:"task_version,omitempty"`
	MinVram         uint64  `json:"min_vram,omitempty"`
	RequiredGPU     string  `json:"required_gpu,omitempty"`
	RequiredGPUVram uint64  `json:"required_gpu_vram,omitempty"`
	TaskFee         string  `json:"task_fee"`
	QueueTimeout    *uint64 `json:"queue_timeout,omitempty"`
}

type RawClientTask struct {
	ID                    uint    `json:"id"`
	ClientID              uint    `json:"client_id"`
	Status                string  `json:"status"`
	Phase                 string  `json:"phase"`
	EstimatedCompletionAt *string `json:"estimated_completion_at,omitempty"`
	ErrorMessage          string  `json:"error_message,omitempty"`
	CreatedAt             string  `json:"created_at"`
	UpdatedAt             string  `json:"updated_at"`
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
	Message string                         `json:"message"`
	Data    []BatchCreateRawTaskItemResult `json:"data"`
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
	Message string                            `json:"message"`
	Data    []BatchGetRawTaskStatusItemResult `json:"data"`
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
		TaskType: 1,
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

func (c *RawTaskClient) CreateTasks(ctx context.Context, tasks []CreateRawTaskRequest) ([]BatchCreateRawTaskItemResult, error) {
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

func (c *RawTaskClient) CreateLLMTasks(ctx context.Context, tasks []CreateRawTaskRequest) ([]BatchCreateRawTaskItemResult, error) {
	return c.CreateTasks(ctx, tasks)
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

type DownloadedResult struct {
	Body               []byte
	ContentType        string
	ContentDisposition string
}

func (c *RawTaskClient) DownloadImageResult(ctx context.Context, clientTaskID uint, index uint64) (*DownloadedResult, error) {
	url := fmt.Sprintf("%s/v1/inference_tasks/%d/images/%d", c.baseURL, clientTaskID, index)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "image/png")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bridge download image failed: %s", string(body))
	}
	return &DownloadedResult{
		Body:               body,
		ContentType:        resp.Header.Get("Content-Type"),
		ContentDisposition: resp.Header.Get("Content-Disposition"),
	}, nil
}

func IsBridgeTaskTerminal(status string) bool {
	return status == ClientTaskStatusSuccess || status == ClientTaskStatusFailed
}

func IsBridgeTaskSuccess(status string) bool {
	return status == ClientTaskStatusSuccess
}

var (
	ErrAbortNotCancellable = errors.New("not_cancellable")
	ErrAbortTemporary      = errors.New("temporary_error")
	ErrAbortNotFound       = errors.New("not_found")
)

type BatchAbortRawTaskRequest struct {
	ClientTaskIDs []uint `json:"client_task_ids"`
}

type BatchAbortRawTaskItemResult struct {
	ClientTaskID uint           `json:"client_task_id"`
	ClientTask   *RawClientTask `json:"client_task,omitempty"`
	Error        string         `json:"error,omitempty"`
}

type batchAbortRawTaskEnvelope struct {
	Message string                         `json:"message"`
	Data    []BatchAbortRawTaskItemResult  `json:"data"`
}

func (c *RawTaskClient) AbortTask(ctx context.Context, clientTaskID uint) (*RawClientTask, error) {
	url := fmt.Sprintf("%s/v1/inference_tasks/%d/abort", c.baseURL, clientTaskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbortTemporary, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbortTemporary, err)
	}
	if err := mapBridgeAbortStatus(resp.StatusCode, respBody); err != nil {
		return nil, err
	}

	var envelope getRawTaskEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbortTemporary, err)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("%w: bridge abort returned empty data", ErrAbortTemporary)
	}
	return envelope.Data, nil
}

func (c *RawTaskClient) AbortTasks(ctx context.Context, clientTaskIDs []uint) ([]BatchAbortRawTaskItemResult, error) {
	body, err := json.Marshal(BatchAbortRawTaskRequest{ClientTaskIDs: clientTaskIDs})
	if err != nil {
		return nil, err
	}

	url := c.baseURL + "/v1/inference_tasks/batch/abort"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbortTemporary, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbortTemporary, err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: bridge batch abort failed: %s", ErrAbortTemporary, string(respBody))
	}

	var envelope batchAbortRawTaskEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbortTemporary, err)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("%w: bridge batch abort returned empty data", ErrAbortTemporary)
	}
	return envelope.Data, nil
}

func mapBridgeAbortStatus(statusCode int, body []byte) error {
	if statusCode < 400 {
		return nil
	}
	msg := strings.ToLower(string(body))
	switch statusCode {
	case http.StatusConflict:
		return ErrAbortNotCancellable
	case http.StatusServiceUnavailable:
		return ErrAbortTemporary
	case http.StatusNotFound, http.StatusBadRequest:
		if strings.Contains(msg, "not found") {
			return ErrAbortNotFound
		}
		if strings.Contains(msg, "not_cancellable") {
			return ErrAbortNotCancellable
		}
		if strings.Contains(msg, "temporary_error") {
			return ErrAbortTemporary
		}
	}
	if strings.Contains(msg, "not_cancellable") {
		return ErrAbortNotCancellable
	}
	if strings.Contains(msg, "temporary_error") {
		return ErrAbortTemporary
	}
	return fmt.Errorf("%w: bridge abort failed: %s", ErrAbortTemporary, string(body))
}
