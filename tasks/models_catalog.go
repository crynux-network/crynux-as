package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// StartModelsCatalogRefresh refreshes the public models API catalog on the
// configured interval. A failed refresh keeps the previous snapshot and retries
// at the next tick.
func StartModelsCatalogRefresh(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		interval := time.Duration(config.GetConfig().LLM.ModelsCatalogRefreshInterval) * time.Second
		log.Infof("models catalog refresh task started with interval %s", interval)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Infoln("models catalog refresh task stopped")
				return
			case <-ticker.C:
				if err := service.RefreshModelsCatalog(ctx, config.GetDB()); err != nil {
					log.Errorf("models catalog refresh failed: %v", err)
				}
			}
		}
	}()
}
