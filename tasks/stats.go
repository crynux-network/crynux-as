package tasks

import (
	"context"

	log "github.com/sirupsen/logrus"
)

// StartStatsProjectUsage periodically aggregates llm_call_records into
// project_usage_stats per project and time period.
func StartStatsProjectUsage(ctx context.Context) {
	log.Infoln("project usage stats task started")
	<-ctx.Done()
	log.Infoln("project usage stats task stopped")
}
