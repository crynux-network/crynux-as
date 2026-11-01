package main

import (
	"context"
	"crynux_as/api"
	"crynux_as/blockchain"
	"crynux_as/config"
	"crynux_as/migrate"
	"crynux_as/provider"
	"crynux_as/relay"
	"crynux_as/service"
	"crynux_as/tasks"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

func main() {
	if err := config.InitConfig(""); err != nil {
		print("Error reading config file")
		print(err.Error())
		os.Exit(1)
	}

	conf := config.GetConfig()

	encryptionKey, err := service.LoadProviderEncryptionKeyFile(conf.Provider.KeyEncryption.EncryptionKeyFile)
	if err != nil {
		print("Error reading provider encryption key file")
		print(err.Error())
		os.Exit(1)
	}

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

	if err := service.InitProviderCatalog(conf); err != nil {
		log.Fatalln(err)
	}
	reg, err := provider.NewRegistry(conf)
	if err != nil {
		log.Fatalln(err)
	}
	service.SetProviderAdapterRegistry(reg)
	service.InitSharedAPIKeyCredentialValidator()
	service.InitProviderKeySlots(conf.Provider.MaxConcurrencyPerKey)
	service.InitProviderStreamRegistry(int(conf.Provider.StreamSubscriberBuffer))

	enc, err := service.NewProviderKeyEncryption(encryptionKey)
	if err != nil {
		log.Fatalln(err)
	}
	service.SetProviderKeyEncryption(enc)
	service.ClearBytes(encryptionKey)
	if err := os.Remove(conf.Provider.KeyEncryption.EncryptionKeyFile); err != nil {
		log.Errorf("failed to remove provider encryption key file: %v", err)
		os.Exit(1)
	}

	if err := service.FailPriorProcessProviderJobs(context.Background(), config.GetDB()); err != nil {
		log.Fatalln(err)
	}

	intakeCtx, intakeCancel := context.WithCancel(context.Background())
	defer intakeCancel()
	providerJobCtx, providerJobCancel := context.WithCancel(context.Background())
	defer providerJobCancel()

	var workerWG sync.WaitGroup
	service.StartBlockchainProcessors(intakeCtx, &workerWG)
	tasks.StartUsageStatsWorkers(intakeCtx, &workerWG)

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
	tasks.StartLoadedModelsRefresh(intakeCtx, &workerWG)
	tasks.StartQueuedPriorityRefresh(intakeCtx, &workerWG)
	tasks.StartTaskJobWorker(intakeCtx, providerJobCtx, &workerWG)
	tasks.StartTaskJobRetentionCleanup(intakeCtx, &workerWG)
	tasks.StartCreditsLockedReconciler(intakeCtx, &workerWG)

	srv := newHTTPServer(conf)
	serverErr := make(chan error, 1)
	go func() {
		log.Infoln("Starting application server...")
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			log.Errorln(err.Error())
			intakeCancel()
			providerJobCancel()
			workerWG.Wait()
			os.Exit(1)
		}
	case sig := <-sigCh:
		log.Infof("received signal %s; starting graceful shutdown", sig)
		gracefulShutdown(srv, conf, intakeCancel, providerJobCancel, &workerWG)
	}
}

func newHTTPServer(conf *config.AppConfig) *http.Server {
	app := api.GetHttpApplication(conf)
	address := fmt.Sprintf("%s:%s", conf.Http.Host, conf.Http.Port)
	return &http.Server{
		Addr:    address,
		Handler: app,
	}
}

func gracefulShutdown(
	srv *http.Server,
	conf *config.AppConfig,
	intakeCancel context.CancelFunc,
	providerJobCancel context.CancelFunc,
	workerWG *sync.WaitGroup,
) {
	deadline := time.Now().Add(time.Duration(conf.Provider.ShutdownTimeoutSeconds) * time.Second)
	intakeCancel()

	shutdownCtx, shutdownCancel := context.WithDeadline(context.Background(), deadline)
	defer shutdownCancel()

	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Errorf("http server shutdown: %v", err)
			return
		}
		log.Infoln("http server shutdown complete")
	}()

	workersDone := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(workersDone)
	}()

	allDone := make(chan struct{})
	go func() {
		<-httpDone
		<-workersDone
		close(allDone)
	}()

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()

	select {
	case <-allDone:
		providerJobCancel()
		log.Infoln("graceful shutdown complete")
		return
	case <-timer.C:
		log.Warnln("shutdown timeout reached; detaching streams and cancelling active provider jobs")
		service.GetProviderStreamRegistry().DetachAll()
		providerJobCancel()
		forceCtx, forceCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer forceCancel()
		select {
		case <-allDone:
		case <-forceCtx.Done():
		}
		log.Infoln("graceful shutdown finished after timeout")
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
