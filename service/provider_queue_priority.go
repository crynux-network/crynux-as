package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"math/big"
	"sync"
	"time"

	"gorm.io/gorm"
)

type ProviderQueuePrioritySnapshot struct {
	ObservedAt time.Time
	HasJobs    bool
	Lowest     *big.Int
	Highest    *big.Int
}

var (
	providerQueuePriorityMu       sync.RWMutex
	providerQueuePrioritySnapshot ProviderQueuePrioritySnapshot
)

func GetProviderQueuePrioritySnapshot() ProviderQueuePrioritySnapshot {
	providerQueuePriorityMu.RLock()
	defer providerQueuePriorityMu.RUnlock()
	return providerQueuePrioritySnapshot
}

func RefreshProviderQueuePrioritySnapshot(ctx context.Context, db *gorm.DB) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	sources := []models.ExecutionSource{
		models.ExecutionSourceOpenAI,
		models.ExecutionSourceAnthropic,
		models.ExecutionSourceGoogle,
	}

	var lowestJob models.TaskJob
	err := db.WithContext(dbCtx).
		Where("execution_source IN ? AND status = ? AND phase = ?", sources, models.TaskJobStatusPendingSubmit, "queued").
		Order("sortable_priority ASC, id ASC").
		Limit(1).
		First(&lowestJob).Error
	snapshot := ProviderQueuePrioritySnapshot{ObservedAt: now}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		providerQueuePriorityMu.Lock()
		providerQueuePrioritySnapshot = snapshot
		providerQueuePriorityMu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	if lowestJob.CreatedAt.Add(time.Duration(lowestJob.QueueTimeout) * time.Second).Before(now) {
		providerQueuePriorityMu.Lock()
		providerQueuePrioritySnapshot = snapshot
		providerQueuePriorityMu.Unlock()
		return nil
	}

	var highestJob models.TaskJob
	if err := db.WithContext(dbCtx).
		Where("execution_source IN ? AND status = ? AND phase = ?", sources, models.TaskJobStatusPendingSubmit, "queued").
		Order("sortable_priority DESC, id DESC").
		Limit(1).
		First(&highestJob).Error; err != nil {
		return err
	}
	lowest, ok := new(big.Int).SetString(lowestJob.PriorityGwei.String(), 10)
	if !ok {
		lowest = big.NewInt(0)
	}
	highest, ok := new(big.Int).SetString(highestJob.PriorityGwei.String(), 10)
	if !ok {
		highest = big.NewInt(0)
	}
	snapshot.HasJobs = true
	snapshot.Lowest = lowest
	snapshot.Highest = highest
	providerQueuePriorityMu.Lock()
	providerQueuePrioritySnapshot = snapshot
	providerQueuePriorityMu.Unlock()
	_ = config.GetConfig()
	return nil
}
