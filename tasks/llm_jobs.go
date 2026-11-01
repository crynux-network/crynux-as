package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

func StartTaskJobWorker(intakeCtx, providerJobCtx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Infoln("job workers started")
		service.RunJobWorkers(intakeCtx, providerJobCtx)
		log.Infoln("job workers stopped")
	}()
}

func StartTaskJobRetentionCleanup(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Infoln("task job retention cleanup started")
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		runCleanup := func() {
			retentionDays := config.GetConfig().LLM.JobRetentionDays
			deleted, err := service.RunTaskJobRetentionCleanup(ctx, config.GetDB(), retentionDays)
			if err != nil {
				log.Errorf("task job retention cleanup failed: %v", err)
				return
			}
			if deleted > 0 {
				log.Infof("task job retention cleanup deleted %d jobs", deleted)
			}
		}

		runCleanup()
		for {
			select {
			case <-ctx.Done():
				log.Infoln("task job retention cleanup stopped")
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()
}

func StartCreditsLockedReconciler(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Infoln("credits locked reconciler started")
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		runReconcile := func() {
			corrected, err := service.ReconcileCreditsLocked(ctx, config.GetDB())
			if err != nil {
				log.Errorf("credits locked reconcile failed: %v", err)
				return
			}
			if corrected > 0 {
				log.Errorf("credits locked reconcile corrected %d accounts", corrected)
			}
		}

		runReconcile()
		for {
			select {
			case <-ctx.Done():
				log.Infoln("credits locked reconciler stopped")
				return
			case <-ticker.C:
				runReconcile()
			}
		}
	}()
}
