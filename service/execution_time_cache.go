package service

import (
	"context"
	"crynux_as/relay"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type executionTimeCacheEntry struct {
	coefficients relay.LLMExecutionTime
	expiresAt    time.Time
}

type sdExecutionTimeCacheEntry struct {
	coefficients relay.SDExecutionTime
	expiresAt    time.Time
}

type executionTimeCache struct {
	mu        sync.RWMutex
	client    *relay.Client
	ttl       time.Duration
	now       func() time.Time
	entries   map[string]executionTimeCacheEntry
	sdEntries map[string]sdExecutionTimeCacheEntry
}

var llmExecutionTimeCache = &executionTimeCache{
	now:       time.Now,
	entries:   map[string]executionTimeCacheEntry{},
	sdEntries: map[string]sdExecutionTimeCacheEntry{},
}

// InitExecutionTimeCache sets the Relay client and TTL used by on-demand
// execution-time fetches. It must be called once at startup.
func InitExecutionTimeCache(client *relay.Client, ttlSeconds uint64) {
	llmExecutionTimeCache.mu.Lock()
	defer llmExecutionTimeCache.mu.Unlock()
	llmExecutionTimeCache.client = client
	llmExecutionTimeCache.ttl = time.Duration(ttlSeconds) * time.Second
	llmExecutionTimeCache.entries = map[string]executionTimeCacheEntry{}
	llmExecutionTimeCache.sdEntries = map[string]sdExecutionTimeCacheEntry{}
}

func GetSDExecutionTime(ctx context.Context, query relay.ExecutionTimeQuery) (*relay.SDExecutionTime, error) {
	key, err := sdExecutionTimeCacheKey(query)
	if err != nil {
		return nil, err
	}
	llmExecutionTimeCache.mu.RLock()
	client := llmExecutionTimeCache.client
	ttl := llmExecutionTimeCache.ttl
	nowFn := llmExecutionTimeCache.now
	entry, ok := llmExecutionTimeCache.sdEntries[key]
	llmExecutionTimeCache.mu.RUnlock()
	if client == nil {
		return nil, errors.New("execution time cache is not initialized")
	}
	if ttl <= 0 {
		return nil, errors.New("execution time cache ttl is not set")
	}
	now := nowFn()
	if ok && now.Before(entry.expiresAt) {
		copied := entry.coefficients
		return &copied, nil
	}
	fetched, err := client.GetSDExecutionTime(ctx, query)
	if err != nil {
		return nil, err
	}
	llmExecutionTimeCache.mu.Lock()
	llmExecutionTimeCache.sdEntries[key] = sdExecutionTimeCacheEntry{
		coefficients: *fetched,
		expiresAt:    nowFn().Add(ttl),
	}
	llmExecutionTimeCache.mu.Unlock()
	copied := *fetched
	return &copied, nil
}

func sdExecutionTimeCacheKey(query relay.ExecutionTimeQuery) (string, error) {
	model := strings.ToLower(strings.TrimSpace(query.Model))
	if model == "" {
		return "", errors.New("model is required")
	}
	minVRAM, gpuVRAM := uint64(0), uint64(0)
	if query.MinVRAM != nil {
		minVRAM = *query.MinVRAM
	}
	if query.GPUVRAM != nil {
		gpuVRAM = *query.GPUVRAM
	}
	quantizeBits := uint64(0)
	if query.QuantizeBits != nil {
		quantizeBits = *query.QuantizeBits
	}
	return fmt.Sprintf(
		"%s|%s|%d|%s|%d|%s|%d",
		model,
		strings.ToLower(strings.TrimSpace(query.Variant)),
		quantizeBits,
		strings.ToLower(strings.TrimSpace(query.Dtype)),
		minVRAM,
		strings.ToLower(strings.TrimSpace(query.GPUName)),
		gpuVRAM,
	), nil
}

// GetLLMExecutionTime returns cached coefficients for (model, effectiveVRAM),
// fetching from Relay when missing or expired.
func GetLLMExecutionTime(ctx context.Context, model string, effectiveVRAM uint64) (*relay.LLMExecutionTime, error) {
	query := relay.ExecutionTimeQuery{Model: model, MinVRAM: &effectiveVRAM}
	return GetLLMExecutionTimeForQuery(ctx, query)
}

func GetLLMExecutionTimeForQuery(ctx context.Context, query relay.ExecutionTimeQuery) (*relay.LLMExecutionTime, error) {
	queryKey, err := sdExecutionTimeCacheKey(query)
	if err != nil {
		return nil, err
	}
	key := "llm|" + queryKey
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

	now := nowFn()
	if ok && now.Before(entry.expiresAt) {
		copied := entry.coefficients
		return &copied, nil
	}

	fetched, err := client.GetLLMExecutionTimeWithQuery(ctx, query)
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

func resetExecutionTimeCacheForTest() {
	llmExecutionTimeCache.mu.Lock()
	defer llmExecutionTimeCache.mu.Unlock()
	llmExecutionTimeCache.client = nil
	llmExecutionTimeCache.ttl = 0
	llmExecutionTimeCache.now = time.Now
	llmExecutionTimeCache.entries = map[string]executionTimeCacheEntry{}
	llmExecutionTimeCache.sdEntries = map[string]sdExecutionTimeCacheEntry{}
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
