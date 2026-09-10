package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"time"

	log "github.com/sirupsen/logrus"
)

func StartLLMJobWorker(ctx context.Context) {
	go func() {
		log.Infoln("llm job worker started")
		service.RunLLMJobWorker(ctx)
	}()
}

func StartLLMJobRetentionCleanup(ctx context.Context) {
	go func() {
		log.Infoln("llm job retention cleanup started")
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		runCleanup := func() {
			retentionDays := config.GetConfig().LLM.JobRetentionDays
			deleted, err := service.RunLLMJobRetentionCleanup(ctx, config.GetDB(), retentionDays)
			if err != nil {
				log.Errorf("llm job retention cleanup failed: %v", err)
				return
			}
			if deleted > 0 {
				log.Infof("llm job retention cleanup deleted %d jobs", deleted)
			}
		}

		runCleanup()
		for {
			select {
			case <-ctx.Done():
				log.Infoln("llm job retention cleanup stopped")
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()
}
