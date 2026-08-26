package tasks

import (
	"context"
	"crynux_as/config"
	"crynux_as/service"
	"time"

	log "github.com/sirupsen/logrus"
)

// StartQueuedPriorityRefresh refreshes the in-memory queued-task priority
// snapshot on the configured interval. A failed refresh keeps the previous
// snapshot and retries at the next tick.
func StartQueuedPriorityRefresh(ctx context.Context) {
	interval := time.Duration(config.GetConfig().LLM.QueuedPriorityRefreshInterval) * time.Second
	log.Infof("queued priority refresh task started with interval %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Infoln("queued priority refresh task stopped")
			return
		case <-ticker.C:
			if err := service.RefreshQueuedPriority(ctx); err != nil {
				log.Errorf("queued priority refresh failed: %v", err)
			}
		}
	}
}
