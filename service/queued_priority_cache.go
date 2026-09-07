package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/relay"
	"errors"
	"math/big"
	"sync"

	log "github.com/sirupsen/logrus"
)

type QueuedPrioritySnapshot struct {
	AsOf                int64
	QueuedTaskCount     int64
	HighestPriorityGwei *big.Int
	MedianPriorityGwei  *big.Int
	LowestPriorityGwei  *big.Int
}

type queuedPriorityCache struct {
	mu                 sync.RWMutex
	client             *relay.Client
	snapshot           QueuedPrioritySnapshot
	lastNonEmptyMedian *big.Int
}

var priorityCache = &queuedPriorityCache{}

// InitQueuedPriorityCache sets the Relay client used by priority refreshes.
// It must be called once at startup before RefreshQueuedPriority.
func InitQueuedPriorityCache(client *relay.Client) {
	priorityCache.mu.Lock()
	defer priorityCache.mu.Unlock()
	priorityCache.client = client
}

// RefreshQueuedPriority fetches the queued-task priority snapshot from Relay
// and replaces the in-memory snapshot. On failure the previous snapshot is kept.
func RefreshQueuedPriority(ctx context.Context) error {
	priorityCache.mu.RLock()
	client := priorityCache.client
	priorityCache.mu.RUnlock()
	if client == nil {
		return errors.New("queued priority cache is not initialized")
	}

	fetched, err := client.GetQueuedTaskPriority(ctx)
	if err != nil {
		return err
	}

	snapshot := QueuedPrioritySnapshot{
		AsOf:            fetched.AsOf,
		QueuedTaskCount: fetched.QueuedTaskCount,
	}
	if fetched.HighestPriorityGwei != nil {
		snapshot.HighestPriorityGwei = new(big.Int).Set(fetched.HighestPriorityGwei)
	}
	if fetched.MedianPriorityGwei != nil {
		snapshot.MedianPriorityGwei = new(big.Int).Set(fetched.MedianPriorityGwei)
	}
	if fetched.LowestPriorityGwei != nil {
		snapshot.LowestPriorityGwei = new(big.Int).Set(fetched.LowestPriorityGwei)
	}

	priorityCache.mu.Lock()
	priorityCache.snapshot = snapshot
	if snapshot.MedianPriorityGwei != nil && snapshot.QueuedTaskCount > 0 {
		priorityCache.lastNonEmptyMedian = new(big.Int).Set(snapshot.MedianPriorityGwei)
	}
	priorityCache.mu.Unlock()

	log.Infof(
		"queued priority cache refreshed: as_of=%d queued_task_count=%d",
		snapshot.AsOf, snapshot.QueuedTaskCount,
	)
	return nil
}

// GetQueuedPrioritySnapshot returns a copy of the current priority snapshot.
func GetQueuedPrioritySnapshot() QueuedPrioritySnapshot {
	priorityCache.mu.RLock()
	defer priorityCache.mu.RUnlock()
	return copyQueuedPrioritySnapshot(priorityCache.snapshot)
}

// ResolveMedianPriorityGwei returns the Queue Median Hint when a live or
// remembered non-empty median exists. ok is false only when neither exists.
func ResolveMedianPriorityGwei() (median *big.Int, ok bool) {
	priorityCache.mu.RLock()
	defer priorityCache.mu.RUnlock()
	if priorityCache.snapshot.MedianPriorityGwei != nil && priorityCache.snapshot.QueuedTaskCount > 0 {
		return new(big.Int).Set(priorityCache.snapshot.MedianPriorityGwei), true
	}
	if priorityCache.lastNonEmptyMedian != nil {
		return new(big.Int).Set(priorityCache.lastNonEmptyMedian), true
	}
	return nil, false
}

// ResolveQueueMedianHint returns the Queue Median Hint, falling back to
// empty_queue_median_priority_gwei when no live or remembered median exists.
func ResolveQueueMedianHint() (*big.Int, error) {
	if median, ok := ResolveMedianPriorityGwei(); ok {
		return median, nil
	}
	return config.GetConfig().ParseEmptyQueueMedianPriorityGwei()
}

// ResolveQueuePriorityBounds returns the current non-empty queue highest and
// lowest priorities. ok is false when the live snapshot has no usable bounds.
func ResolveQueuePriorityBounds() (highest, lowest *big.Int, ok bool) {
	priorityCache.mu.RLock()
	defer priorityCache.mu.RUnlock()
	if priorityCache.snapshot.QueuedTaskCount <= 0 {
		return nil, nil, false
	}
	if priorityCache.snapshot.HighestPriorityGwei == nil || priorityCache.snapshot.LowestPriorityGwei == nil {
		return nil, nil, false
	}
	return new(big.Int).Set(priorityCache.snapshot.HighestPriorityGwei),
		new(big.Int).Set(priorityCache.snapshot.LowestPriorityGwei),
		true
}

func copyQueuedPrioritySnapshot(src QueuedPrioritySnapshot) QueuedPrioritySnapshot {
	dst := QueuedPrioritySnapshot{
		AsOf:            src.AsOf,
		QueuedTaskCount: src.QueuedTaskCount,
	}
	if src.HighestPriorityGwei != nil {
		dst.HighestPriorityGwei = new(big.Int).Set(src.HighestPriorityGwei)
	}
	if src.MedianPriorityGwei != nil {
		dst.MedianPriorityGwei = new(big.Int).Set(src.MedianPriorityGwei)
	}
	if src.LowestPriorityGwei != nil {
		dst.LowestPriorityGwei = new(big.Int).Set(src.LowestPriorityGwei)
	}
	return dst
}

func resetQueuedPriorityCacheForTest() {
	priorityCache.mu.Lock()
	defer priorityCache.mu.Unlock()
	priorityCache.client = nil
	priorityCache.snapshot = QueuedPrioritySnapshot{}
	priorityCache.lastNonEmptyMedian = nil
}

func setQueuedPriorityCacheStateForTest(snapshot QueuedPrioritySnapshot, lastNonEmpty *big.Int) {
	priorityCache.mu.Lock()
	defer priorityCache.mu.Unlock()
	priorityCache.snapshot = copyQueuedPrioritySnapshot(snapshot)
	if lastNonEmpty != nil {
		priorityCache.lastNonEmptyMedian = new(big.Int).Set(lastNonEmpty)
	} else {
		priorityCache.lastNonEmptyMedian = nil
	}
}
