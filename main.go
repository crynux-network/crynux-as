package main

import (
	"context"
	"crynux_as/api"
	"crynux_as/blockchain"
	"crynux_as/config"
	"crynux_as/migrate"
	"crynux_as/relay"
	"crynux_as/service"
	"crynux_as/tasks"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
)

func main() {
	if err := config.InitConfig(""); err != nil {
		print("Error reading config file")
		print(err.Error())
		os.Exit(1)
	}

	conf := config.GetConfig()

	if err := config.InitLog(conf); err != nil {
		print("Error initializing log")
		print(err.Error())
		os.Exit(1)
	}

	if err := config.InitDB(conf); err != nil {
		log.Errorln(err.Error())
		os.Exit(1)
	}

	startDBMigration()

	if err := blockchain.Init(context.Background()); err != nil {
		log.Fatalln(err)
	}

	service.StartBlockchainProcessors(context.Background())
	tasks.StartUsageStatsWorkers(context.Background())

	relayClient := relay.NewClient(conf.Relay.BaseURL)
	service.InitLoadedModelsCache(relayClient)
	service.InitQueuedPriorityCache(relayClient)
	service.InitExecutionTimeCache(relayClient, conf.LLM.ExecutionTimeCacheTTL)
	if err := service.RefreshLoadedModels(context.Background()); err != nil {
		log.Errorf("initial loaded models refresh failed: %v", err)
	}
	if err := service.RefreshQueuedPriority(context.Background()); err != nil {
		log.Errorf("initial queued priority refresh failed: %v", err)
	}
	go tasks.StartLoadedModelsRefresh(context.Background())
	go tasks.StartQueuedPriorityRefresh(context.Background())
	tasks.StartLLMJobWorker(context.Background())
	tasks.StartLLMJobRetentionCleanup(context.Background())

	startServer()
}

func startServer() {
	conf := config.GetConfig()

	app := api.GetHttpApplication(conf)
	address := fmt.Sprintf("%s:%s", conf.Http.Host, conf.Http.Port)

	log.Infoln("Starting application server...")

	if err := app.Run(address); err != nil {
		log.Errorln(err.Error())
		os.Exit(1)
	}
}

func startDBMigration() {

	migrate.InitMigration(config.GetDB())

	if err := migrate.Migrate(); err != nil {
		log.Errorln(err.Error())
		if err = migrate.Rollback(); err != nil {
			log.Errorln(err.Error())
		}
		os.Exit(1)
	}

	log.Infoln("DB migrations are done!")
}
