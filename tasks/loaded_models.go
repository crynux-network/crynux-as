package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// StartLoadedModelsRefresh refreshes the in-memory LLM loaded-models cache on
// the configured interval. After each successful refresh it rebuilds the public
// models catalog so Crynux rows track the new Relay snapshot. A failed loaded
// refresh keeps the previous snapshots and retries at the next tick.
func StartLoadedModelsRefresh(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
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
					continue
				}
				if err := service.RefreshModelsCatalog(ctx, config.GetDB()); err != nil {
					log.Errorf("models catalog refresh after loaded models failed: %v", err)
				}
			}
		}
	}()
}
