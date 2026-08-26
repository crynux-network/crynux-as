package service

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crynux_as/relay"
)

func TestRefreshQueuedPriorityKeepsPreviousOnFailure(t *testing.T) {
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "success",
			"data": map[string]any{
				"as_of":                  100,
				"queued_task_count":      1,
				"highest_priority_gwei":  "50",
				"median_priority_gwei":   "40",
				"lowest_priority_gwei":   "30",
			},
		})
	}))
	defer server.Close()

	InitQueuedPriorityCache(relay.NewClient(server.URL))
	if err := RefreshQueuedPriority(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failServer.Close()
	InitQueuedPriorityCache(relay.NewClient(failServer.URL))

	if err := RefreshQueuedPriority(context.Background()); err == nil {
		t.Fatal("expected refresh failure")
	}
	snapshot := GetQueuedPrioritySnapshot()
	if snapshot.AsOf != 100 || snapshot.MedianPriorityGwei == nil || snapshot.MedianPriorityGwei.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("expected previous snapshot retained, got %+v", snapshot)
	}
}

func TestResolveMedianPriorityUsesLastNonEmptyOnEmptyQueue(t *testing.T) {
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "success",
				"data": map[string]any{
					"as_of":                 1,
					"queued_task_count":     1,
					"highest_priority_gwei": "20",
					"median_priority_gwei":  "15",
					"lowest_priority_gwei":  "10",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "success",
			"data": map[string]any{
				"as_of":                 2,
				"queued_task_count":     0,
				"highest_priority_gwei": nil,
				"median_priority_gwei":  nil,
				"lowest_priority_gwei":  nil,
			},
		})
	}))
	defer server.Close()

	InitQueuedPriorityCache(relay.NewClient(server.URL))
	if err := RefreshQueuedPriority(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if err := RefreshQueuedPriority(context.Background()); err != nil {
		t.Fatalf("second refresh: %v", err)
	}

	median, ok := ResolveMedianPriorityGwei()
	if !ok {
		t.Fatal("expected last non-empty median")
	}
	if median.Cmp(big.NewInt(15)) != 0 {
		t.Fatalf("median = %s, want 15", median.String())
	}
	snapshot := GetQueuedPrioritySnapshot()
	if snapshot.QueuedTaskCount != 0 || snapshot.MedianPriorityGwei != nil {
		t.Fatalf("expected empty current snapshot, got %+v", snapshot)
	}
}

func TestResolveMedianPriorityEmptyWithoutHistory(t *testing.T) {
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)

	setQueuedPriorityCacheStateForTest(QueuedPrioritySnapshot{AsOf: 1, QueuedTaskCount: 0}, nil)
	if _, ok := ResolveMedianPriorityGwei(); ok {
		t.Fatal("expected no median when empty and no history")
	}
}

func TestGetLLMExecutionTimeTTL(t *testing.T) {
	resetExecutionTimeCacheForTest()
	t.Cleanup(resetExecutionTimeCacheForTest)

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "success",
			"data": map[string]any{
				"constant_seconds":         float64(calls),
				"seconds_per_input_token":  0.1,
				"seconds_per_output_token": 0.2,
				"model_switch_seconds":     0,
				"seconds_per_image":        0,
				"seconds_per_megapixel":    0,
			},
		})
	}))
	defer server.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	InitExecutionTimeCache(relay.NewClient(server.URL), 10)
	setExecutionTimeCacheNowForTest(func() time.Time { return now })

	first, err := GetLLMExecutionTime(context.Background(), "qwen/qwen3", 24)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.ConstantSeconds != 1 {
		t.Fatalf("ConstantSeconds = %v, want 1", first.ConstantSeconds)
	}

	now = now.Add(5 * time.Second)
	second, err := GetLLMExecutionTime(context.Background(), "qwen/qwen3", 24)
	if err != nil {
		t.Fatalf("cached fetch: %v", err)
	}
	if second.ConstantSeconds != 1 {
		t.Fatalf("expected cached value, got %v", second.ConstantSeconds)
	}
	if calls != 1 {
		t.Fatalf("expected 1 relay call before expiry, got %d", calls)
	}

	now = now.Add(6 * time.Second)
	third, err := GetLLMExecutionTime(context.Background(), "qwen/qwen3", 24)
	if err != nil {
		t.Fatalf("expired fetch: %v", err)
	}
	if third.ConstantSeconds != 2 {
		t.Fatalf("ConstantSeconds = %v, want 2", third.ConstantSeconds)
	}
	if calls != 2 {
		t.Fatalf("expected 2 relay calls after expiry, got %d", calls)
	}
}
