package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"time"

	log "github.com/sirupsen/logrus"
)

const usageStatsInterval = time.Minute

// StartUsageStatsWorkers starts the base aggregation worker and the snapshot worker.
func StartUsageStatsWorkers(ctx context.Context) {
	go startUsageStatsBaseWorker(ctx)
	go startUsageStatsSnapshotWorker(ctx)
}

func startUsageStatsBaseWorker(ctx context.Context) {
	log.Infoln("usage stats base worker started")
	ticker := time.NewTicker(usageStatsInterval)
	defer ticker.Stop()

	runUsageStatsBaseOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Infoln("usage stats base worker stopped")
			return
		case <-ticker.C:
			runUsageStatsBaseOnce(ctx)
		}
	}
}

func startUsageStatsSnapshotWorker(ctx context.Context) {
	log.Infoln("usage stats snapshot worker started")
	ticker := time.NewTicker(usageStatsInterval)
	defer ticker.Stop()

	runUsageStatsSnapshotOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Infoln("usage stats snapshot worker stopped")
			return
		case <-ticker.C:
			runUsageStatsSnapshotOnce(ctx)
		}
	}
}

func runUsageStatsBaseOnce(ctx context.Context) {
	db := config.GetDB()
	for {
		processed, err := service.RunUsageStatsBaseAggregation(ctx, db)
		if err != nil {
			log.Errorf("usage stats base aggregation failed: %v", err)
			return
		}
		if processed == 0 {
			return
		}
	}
}

func runUsageStatsSnapshotOnce(ctx context.Context) {
	db := config.GetDB()
	claimed, err := service.ClaimDirtyProjectsForSnapshot(ctx, db, 20)
	if err != nil {
		log.Errorf("usage stats snapshot claim failed: %v", err)
		return
	}
	for _, item := range claimed {
		if err := service.RefreshProjectUsageSnapshots(ctx, db, item.ProjectID, item.ClaimedGeneration); err != nil {
			log.Errorf("usage stats snapshot refresh failed for project %d: %v", item.ProjectID, err)
		}
	}
}
