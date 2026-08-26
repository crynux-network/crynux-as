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

type executionTimeCache struct {
	mu     sync.RWMutex
	client *relay.Client
	ttl    time.Duration
	now    func() time.Time
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

// GetLLMExecutionTime returns cached coefficients for (model, effectiveVRAM),
// fetching from Relay when missing or expired.
func GetLLMExecutionTime(ctx context.Context, model string, effectiveVRAM uint64) (*relay.LLMExecutionTime, error) {
	modelKey := strings.ToLower(strings.TrimSpace(model))
	if modelKey == "" {
		return nil, errors.New("model is required")
	}
	if effectiveVRAM == 0 {
		return nil, errors.New("effective_vram must be a positive integer")
	}
	key := executionTimeCacheKey(modelKey, effectiveVRAM)

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

	fetched, err := client.GetLLMExecutionTime(ctx, model, effectiveVRAM)
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

func executionTimeCacheKey(model string, effectiveVRAM uint64) string {
	return fmt.Sprintf("%s|%d", model, effectiveVRAM)
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
