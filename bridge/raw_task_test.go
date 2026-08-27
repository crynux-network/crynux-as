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

func TestBridgeTaskTerminalStatus(t *testing.T) {
	tests := []struct {
		status   int
		terminal bool
		success  bool
	}{
		{status: BridgeTaskEndAborted, terminal: true},
		{status: 8},
		{status: BridgeTaskEndInvalidated, terminal: true},
		{status: BridgeTaskResultDownloaded, terminal: true, success: true},
		{status: 2},
	}
	for _, tt := range tests {
		if got := IsBridgeTaskTerminal(tt.status); got != tt.terminal {
			t.Errorf("status %d terminal = %t, want %t", tt.status, got, tt.terminal)
		}
		if got := IsBridgeTaskSuccess(tt.status); got != tt.success {
			t.Errorf("status %d success = %t, want %t", tt.status, got, tt.success)
		}
	}
}
