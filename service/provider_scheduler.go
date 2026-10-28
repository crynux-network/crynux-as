package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var providerAdapterRegistry *provider.Registry

func SetProviderAdapterRegistry(reg *provider.Registry) {
	providerAdapterRegistry = reg
}

func GetProviderAdapterRegistry() *provider.Registry {
	return providerAdapterRegistry
}

// RunJobWorkers supervises the Bridge poller and Provider scheduler independently.
func RunJobWorkers(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("bridge job poller panicked: %v", r)
			}
		}()
		RunTaskJobWorker(ctx)
	}()
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("provider scheduler panicked: %v", r)
			}
		}()
		RunProviderScheduler(ctx)
	}()
	wg.Wait()
}

func RunProviderScheduler(ctx context.Context) {
	cfg := config.GetConfig()
	interval := time.Duration(cfg.Provider.SchedulerIntervalMilliseconds) * time.Millisecond
	db := config.GetDB()
	var active sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			log.Infoln("provider scheduler stopping; waiting for active jobs")
			active.Wait()
			log.Infoln("provider scheduler stopped")
			return
		default:
		}

		if err := RefreshProviderQueuePrioritySnapshot(ctx, db); err != nil {
			log.Errorf("refresh provider queue priority failed: %v", err)
		}
		if err := claimAndStartProviderJobs(ctx, db, &active); err != nil {
			log.Errorf("provider scheduler iteration failed: %v", err)
		}

		select {
		case <-ctx.Done():
			active.Wait()
			return
		case <-time.After(interval):
		}
	}
}

func claimAndStartProviderJobs(ctx context.Context, db *gorm.DB, active *sync.WaitGroup) error {
	cfg := config.GetConfig()
	limit := int(cfg.Provider.SchedulerBatchSize)
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var ids []uint
	if err := db.WithContext(dbCtx).Model(&models.TaskJob{}).
		Where("execution_source IN ? AND status = ? AND phase = ?",
			[]models.ExecutionSource{models.ExecutionSourceOpenAI, models.ExecutionSourceAnthropic, models.ExecutionSourceGoogle},
			models.TaskJobStatusPendingSubmit,
			"queued",
		).
		Order("sortable_priority DESC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, jobID := range ids {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		var job models.TaskJob
		if err := db.WithContext(ctx).First(&job, jobID).Error; err != nil {
			continue
		}
		if time.Now().After(job.CreatedAt.Add(time.Duration(job.QueueTimeout) * time.Second)) {
			_ = FailProviderTaskJob(ctx, db, job.ID, nil, "provider queue timeout")
			continue
		}
		keys, err := ListEligibleSharedAPIKeys(ctx, db, job.ExecutionSource.ServiceProvider())
		if err != nil {
			return err
		}
		slots := GetProviderKeySlots()
		selected, ok := SelectRandomEligibleKey(keys, nil, slots)
		if !ok {
			continue
		}
		if !slots.Reserve(selected.ID) {
			continue
		}
		claimed, err := claimProviderJob(ctx, db, job.ID)
		if err != nil || !claimed {
			slots.Release(selected.ID)
			continue
		}
		keyID := selected.ID
		active.Add(1)
		go func(jobID, sharedAPIKeyID uint) {
			defer active.Done()
			defer slots.Release(sharedAPIKeyID)
			RunProviderJob(ctx, db, jobID, sharedAPIKeyID)
		}(job.ID, keyID)
	}
	return nil
}

func claimProviderJob(ctx context.Context, db *gorm.DB, jobID uint) (bool, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var claimed bool
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var job models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, jobID).Error; err != nil {
			return err
		}
		if job.Status != models.TaskJobStatusPendingSubmit {
			return nil
		}
		if time.Now().After(job.CreatedAt.Add(time.Duration(job.QueueTimeout) * time.Second)) {
			return nil
		}
		now := time.Now()
		startedAt := job.StartedAt
		if startedAt == nil {
			startedAt = &now
		}
		if err := tx.Model(&job).Updates(map[string]interface{}{
			"status":     models.TaskJobStatusSubmitted,
			"started_at": startedAt,
			"phase":      "running",
		}).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}
