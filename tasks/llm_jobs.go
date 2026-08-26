package tasks

import (
	"context"
	"crynux_as/service"

	log "github.com/sirupsen/logrus"
)

func StartLLMJobWorker(ctx context.Context) {
	go func() {
		log.Infoln("llm job worker started")
		service.RunLLMJobWorker(ctx)
	}()
}
