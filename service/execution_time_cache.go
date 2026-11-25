package service

import (
	"context"
	"crynux_as/models"
	"crynux_as/relay"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type executionTimeCacheEntry struct {
	coefficients any
	expiresAt    time.Time
}

type executionTimeCache struct {
	mu      sync.RWMutex
	client  *relay.Client
	ttl     time.Duration
	now     func() time.Time
	entries map[string]executionTimeCacheEntry
}

var llmExecutionTimeCache = &executionTimeCache{
	now:     time.Now,
	entries: map[string]executionTimeCacheEntry{},
}

// InitExecutionTimeCache sets the Relay client and TTL used by on-demand
// execution-time fetches. It must be called once at startup.
func InitExecutionTimeCache(client *relay.Client, ttlSeconds uint64) {
	llmExecutionTimeCache.mu.Lock()
	defer llmExecutionTimeCache.mu.Unlock()
	llmExecutionTimeCache.client = client
	llmExecutionTimeCache.ttl = time.Duration(ttlSeconds) * time.Second
	llmExecutionTimeCache.entries = map[string]executionTimeCacheEntry{}
}

// GetLLMExecutionTime returns cached LLM coefficients for query, fetching from
// Relay when missing or expired. query.Dtype MUST be a concrete dtype.
func GetLLMExecutionTime(ctx context.Context, query relay.ExecutionTimeQuery) (*relay.LLMExecutionTime, error) {
	return getCachedExecutionTime(ctx, "llm", query, (*relay.Client).GetLLMExecutionTime)
}

// GetSDExecutionTime returns cached image coefficients for query, fetching from
// Relay when missing or expired. query.Dtype MUST be a concrete dtype.
func GetSDExecutionTime(ctx context.Context, query relay.ExecutionTimeQuery) (*relay.SDExecutionTime, error) {
	return getCachedExecutionTime(ctx, "sd", query, (*relay.Client).GetSDExecutionTime)
}

// GetVideoExecutionTime returns cached video coefficients for query, fetching
// from Relay when missing or expired. query.Dtype MUST be a concrete dtype.
func GetVideoExecutionTime(ctx context.Context, query relay.ExecutionTimeQuery) (*relay.VideoExecutionTime, error) {
	return getCachedExecutionTime(ctx, "video", query, (*relay.Client).GetVideoExecutionTime)
}

func getCachedExecutionTime[T any](
	ctx context.Context,
	kind string,
	query relay.ExecutionTimeQuery,
	fetch func(*relay.Client, context.Context, relay.ExecutionTimeQuery) (*T, error),
) (*T, error) {
	queryKey, err := executionTimeCacheKey(query)
	if err != nil {
		return nil, err
	}
	key := kind + "|" + queryKey
	llmExecutionTimeCache.mu.RLock()
	client := llmExecutionTimeCache.client
	ttl := llmExecutionTimeCache.ttl
	nowFn := llmExecutionTimeCache.now
	entry, ok := llmExecutionTimeCache.entries[key]
	llmExecutionTimeCache.mu.RUnlock()
	if client == nil {
		return nil, errors.New("execution time cache is not initialized")
	}
	if ttl <= 0 {
		return nil, errors.New("execution time cache ttl is not set")
	}
	if ok && nowFn().Before(entry.expiresAt) {
		cached := entry.coefficients.(T)
		return &cached, nil
	}

	fetched, err := fetch(client, ctx, query)
	if err != nil {
		return nil, err
	}
	llmExecutionTimeCache.mu.Lock()
	llmExecutionTimeCache.entries[key] = executionTimeCacheEntry{
		coefficients: *fetched,
		expiresAt:    nowFn().Add(ttl),
	}
	llmExecutionTimeCache.mu.Unlock()
	copied := *fetched
	return &copied, nil
}

func executionTimeCacheKey(query relay.ExecutionTimeQuery) (string, error) {
	model := strings.ToLower(strings.TrimSpace(query.Model))
	if model == "" {
		return "", errors.New("model is required")
	}
	dtype, err := models.ParseConcreteDType(query.Dtype)
	if err != nil {
		return "", err
	}
	minVRAM, gpuVRAM := uint64(0), uint64(0)
	if query.MinVRAM != nil {
		minVRAM = *query.MinVRAM
	}
	if query.GPUVRAM != nil {
		gpuVRAM = *query.GPUVRAM
	}
	quantKey, err := models.QuantizationConfigKey(query.QuantizationConfig)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s|%s|%s|%d|%s|%d",
		model,
		quantKey,
		dtype,
		minVRAM,
		strings.ToLower(strings.TrimSpace(query.GPUName)),
		gpuVRAM,
	), nil
}

func resetExecutionTimeCacheForTest() {
	llmExecutionTimeCache.mu.Lock()
	defer llmExecutionTimeCache.mu.Unlock()
	llmExecutionTimeCache.client = nil
	llmExecutionTimeCache.ttl = 0
	llmExecutionTimeCache.now = time.Now
	llmExecutionTimeCache.entries = map[string]executionTimeCacheEntry{}
}

func setExecutionTimeCacheNowForTest(now func() time.Time) {
	llmExecutionTimeCache.mu.Lock()
	defer llmExecutionTimeCache.mu.Unlock()
	if now == nil {
		llmExecutionTimeCache.now = time.Now
		return
	}
	llmExecutionTimeCache.now = now
}
