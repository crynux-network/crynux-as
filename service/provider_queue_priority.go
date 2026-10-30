package service

import (
	"context"
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
	return copyProviderQueuePrioritySnapshot(providerQueuePrioritySnapshot)
}

func copyProviderQueuePrioritySnapshot(src ProviderQueuePrioritySnapshot) ProviderQueuePrioritySnapshot {
	dst := ProviderQueuePrioritySnapshot{
		ObservedAt: src.ObservedAt,
		HasJobs:    src.HasJobs,
	}
	if src.Lowest != nil {
		dst.Lowest = new(big.Int).Set(src.Lowest)
	}
	if src.Highest != nil {
		dst.Highest = new(big.Int).Set(src.Highest)
	}
	return dst
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

	lowestJob, err := firstNonExpiredProviderQueuedJob(
		db.WithContext(dbCtx),
		sources,
		now,
		"sortable_priority ASC, id ASC",
		true,
	)
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

	highestJob, err := firstNonExpiredProviderQueuedJob(
		db.WithContext(dbCtx),
		sources,
		now,
		"sortable_priority DESC, id DESC",
		false,
	)
	if err != nil {
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
	return nil
}

func firstNonExpiredProviderQueuedJob(
	db *gorm.DB,
	sources []models.ExecutionSource,
	now time.Time,
	order string,
	ascending bool,
) (*models.TaskJob, error) {
	const batchSize = 100
	var lastPriority string
	var lastID uint
	haveCursor := false

	for {
		query := db.
			Where("execution_source IN ? AND status = ? AND phase = ?", sources, models.TaskJobStatusPendingSubmit, "queued")
		if haveCursor {
			if ascending {
				query = query.Where(
					"(sortable_priority > ?) OR (sortable_priority = ? AND id > ?)",
					lastPriority, lastPriority, lastID,
				)
			} else {
				query = query.Where(
					"(sortable_priority < ?) OR (sortable_priority = ? AND id < ?)",
					lastPriority, lastPriority, lastID,
				)
			}
		}
		var jobs []models.TaskJob
		if err := query.Order(order).Limit(batchSize).Find(&jobs).Error; err != nil {
			return nil, err
		}
		if len(jobs) == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		for i := range jobs {
			job := &jobs[i]
			if !job.CreatedAt.Add(time.Duration(job.QueueTimeout) * time.Second).Before(now) {
				return job, nil
			}
			lastPriority = job.SortablePriority
			lastID = job.ID
			haveCursor = true
		}
		if len(jobs) < batchSize {
			return nil, gorm.ErrRecordNotFound
		}
	}
}
