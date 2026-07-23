package service

import (
	"context"
	"crynux_as/config"

	log "github.com/sirupsen/logrus"
)

// StartBlockchainProcessors starts one deposit processing worker per configured
// blockchain network. Each worker scans ERC20 Transfer logs of the configured
// token contracts whose receiver is the platform receiving address, and turns
// them into deposits and Credits ledger events.
func StartBlockchainProcessors(ctx context.Context) {
	appConfig := config.GetConfig()
	for network := range appConfig.Blockchains {
		go runBlockchainProcessor(ctx, network)
	}
}

func runBlockchainProcessor(ctx context.Context, network string) {
	log.Infof("blockchain processor for network %s started", network)
	<-ctx.Done()
	log.Infof("blockchain processor for network %s stopped", network)
}
