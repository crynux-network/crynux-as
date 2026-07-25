package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"time"

	log "github.com/sirupsen/logrus"
)

// StartLoadedModelsRefresh refreshes the in-memory LLM loaded-models cache on
// the configured interval. A failed refresh keeps the previous snapshot and
// retries at the next tick.
func StartLoadedModelsRefresh(ctx context.Context) {
	interval := time.Duration(config.GetConfig().LLM.LoadedModelsRefreshInterval) * time.Second
	log.Infof("loaded models refresh task started with interval %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Infoln("loaded models refresh task stopped")
			return
		case <-ticker.C:
			if err := service.RefreshLoadedModels(ctx); err != nil {
				log.Errorf("loaded models refresh failed: %v", err)
			}
		}
	}
}
