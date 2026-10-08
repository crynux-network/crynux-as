package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"time"

	log "github.com/sirupsen/logrus"
)

func StartTaskJobWorker(ctx context.Context) {
	go func() {
		log.Infoln("task job worker started")
		service.RunTaskJobWorker(ctx)
	}()
}

func StartTaskJobRetentionCleanup(ctx context.Context) {
	go func() {
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
