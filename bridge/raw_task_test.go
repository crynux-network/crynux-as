package bridge

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateLLMTaskSendsLargeFinalFeeAsWeiString(t *testing.T) {
	taskFeeWei, ok := new(big.Int).SetString("20000000000000000000", 10)
	if !ok {
		t.Fatal("parse task fee")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inference_tasks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if _, ok := body["request_id"]; ok {
			t.Error("request contains request_id")
		}
		if _, ok := body["client_id"]; ok {
			t.Error("request contains client_id")
		}
		if got := string(body["task_fee"]); got != `"20000000000000000000"` {
			t.Errorf("task_fee JSON = %s, want a decimal string", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":1,"status":"running"}}`))
	}))
	defer server.Close()

	client := NewRawTaskClient(server.URL, "key")
	task, err := client.CreateLLMTask(context.Background(), `{"model":"test"}`, 24, taskFeeWei)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != 1 {
		t.Fatalf("task id = %d, want 1", task.ID)
	}
	if task.Status != ClientTaskStatusRunning {
		t.Fatalf("status = %q, want %q", task.Status, ClientTaskStatusRunning)
	}
}

func TestCreateRawTaskRequestTaskFeeJSONType(t *testing.T) {
	body, err := json.Marshal(CreateRawTaskRequest{TaskFee: "0"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if got, ok := fields["task_fee"]; !ok || string(got) != `"0"` {
		t.Fatalf("task_fee = %s, present = %t", got, ok)
	}
}

func TestGetTaskStatusParsesClientTaskStringStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inference_tasks/42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":42,"status":"success"}}`))
	}))
	defer server.Close()

	client := NewRawTaskClient(server.URL, "key")
	task, err := client.GetTaskStatus(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != ClientTaskStatusSuccess {
		t.Fatalf("status = %q, want %q", task.Status, ClientTaskStatusSuccess)
	}
}

func TestCreateLLMTasksUsesBatchEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inference_tasks/batch" || r.Method != http.MethodPost {
			t.Errorf("method/path = %s %q", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"client_task":{"id":7,"status":"running"}}]}`))
	}))
	defer server.Close()

	client := NewRawTaskClient(server.URL, "key")
	results, err := client.CreateLLMTasks(context.Background(), []CreateRawTaskRequest{{
		TaskArgs: `{"model":"test"}`,
		TaskType: llmTaskType,
		MinVram:  24,
		TaskFee:  "1000",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ClientTask == nil || results[0].ClientTask.ID != 7 {
		t.Fatalf("results = %+v", results)
	}
}

func TestGetTaskStatusesUsesBatchEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/inference_tasks/batch/status" || r.Method != http.MethodPost {
			t.Errorf("method/path = %s %q", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"client_task_id":9,"client_task":{"id":9,"status":"failed"}}]}`))
	}))
	defer server.Close()

	client := NewRawTaskClient(server.URL, "key")
	results, err := client.GetTaskStatuses(context.Background(), []uint{9})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ClientTask == nil || results[0].ClientTask.Status != ClientTaskStatusFailed {
		t.Fatalf("results = %+v", results)
	}
}

func TestBridgeTaskTerminalStatus(t *testing.T) {
	tests := []struct {
		status   string
		terminal bool
		success  bool
	}{
		{status: ClientTaskStatusRunning},
		{status: ClientTaskStatusSuccess, terminal: true, success: true},
		{status: ClientTaskStatusFailed, terminal: true},
		{status: ""},
		{status: "unknown"},
	}
	for _, tt := range tests {
		if got := IsBridgeTaskTerminal(tt.status); got != tt.terminal {
			t.Errorf("status %q terminal = %t, want %t", tt.status, got, tt.terminal)
		}
		if got := IsBridgeTaskSuccess(tt.status); got != tt.success {
			t.Errorf("status %q success = %t, want %t", tt.status, got, tt.success)
		}
	}
}
