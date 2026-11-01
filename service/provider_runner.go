package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func RunProviderJob(ctx context.Context, db *gorm.DB, jobID, sharedAPIKeyID uint) {
	cfg := config.GetConfig()
	tried := map[uint]struct{}{sharedAPIKeyID: {}}
	currentKeyID := sharedAPIKeyID
	heldKeyID := sharedAPIKeyID
	defer func() {
		if heldKeyID != 0 {
			GetProviderKeySlots().Release(heldKeyID)
		}
	}()
	attempts := 0

	for {
		attempts++
		job, err := GetTaskJobByID(ctx, db, jobID)
		if err != nil {
			log.Errorf("provider job %d load failed: %v", jobID, err)
			return
		}
		if job.IsTerminal() {
			return
		}
		if time.Now().After(job.CreatedAt.Add(time.Duration(job.QueueTimeout) * time.Second)) {
			_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, "provider queue timeout")
			return
		}
		var key models.SharedAPIKey
		if err := db.WithContext(ctx).First(&key, currentKeyID).Error; err != nil {
			_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, "shared api key unavailable")
			return
		}
		reg := GetProviderAdapterRegistry()
		if reg == nil {
			_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, "provider adapters not initialized")
			return
		}
		adapter, err := reg.Get(key.ServiceProvider)
		if err != nil {
			_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, err.Error())
			return
		}
		secret, err := DecryptSharedAPIKeySecret(&key)
		if err != nil {
			_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, "failed to decrypt shared api key")
			return
		}
		streams := GetProviderStreamRegistry()
		sink := func(event provider.StreamEvent) {
			publishProviderJobExecuteStreamEvent(streams, jobID, event)
		}
		result, execErr := adapter.Execute(ctx, secret, job.APIType, job.Model, []byte(job.TaskArgsJSON), jobID, attempts-1, sink)
		ClearBytes(secret)
		if execErr != nil {
			log.Errorf("provider job %d execute error: %v", jobID, execErr)
		}

		if result.HasConfirmedUsage() && len(result.FinalResultJSON) > 0 {
			settleCtx := context.WithoutCancel(ctx)
			_ = ClearSharedAPIKeyUnavailable(settleCtx, db, currentKeyID)
			var owner models.User
			if err := db.WithContext(settleCtx).First(&owner, key.UserID).Error; err != nil {
				log.Errorf("provider job %d load key owner failed: %v", jobID, err)
				_ = FailProviderTaskJob(settleCtx, db, jobID, &currentKeyID, "key owner unavailable after successful upstream")
				return
			}
			publicID := provider.PublicStreamResponseID(job.APIType, job.ID, job.PublicID)
			resultJSON, err := provider.RewritePublicResultID(result.FinalResultJSON, publicID)
			if err != nil {
				log.Errorf("provider job %d rewrite public result id failed: %v", jobID, err)
				_ = FailProviderTaskJob(settleCtx, db, jobID, &currentKeyID, "failed to normalize public result id")
				return
			}
			formatted := string(resultJSON)
			raw := formatted
			if err := settleProviderJobWithRetry(ctx, settleCtx, db, job, currentKeyID, owner.ID, owner.Address, raw, formatted, *result.FinalUsage); err != nil {
				log.Errorf("provider job %d settlement abandoned after confirmed usage: %v", jobID, err)
				return
			}
			publishProviderJobSettlementTerminal(streams, jobID, result.FinalUsage)
			streams.Unregister(jobID)
			return
		}

		if errors.Is(ctx.Err(), context.Canceled) || result.ErrorClass == provider.ErrorCancelledByShutdown {
			msg := "provider job cancelled by shutdown"
			if result.ErrorMessage != "" {
				msg = result.ErrorMessage
			}
			_ = FailProviderTaskJob(context.WithoutCancel(ctx), db, jobID, &currentKeyID, msg)
			streams.Unregister(jobID)
			return
		}

		canSwitch := result.AcceptanceKnown &&
			result.Acceptance == provider.AcceptanceDefinitelyUnaccepted &&
			result.KeySpecific &&
			!result.PublicOutputBegan &&
			attempts < int(cfg.Provider.MaxAttemptsPerJob) &&
			time.Now().Before(job.CreatedAt.Add(time.Duration(job.QueueTimeout)*time.Second))

		if canSwitch {
			if err := RecordSharedAPIKeyUnavailable(ctx, db, currentKeyID); err != nil {
				log.Errorf("provider job %d record unavailable key %d: %v", jobID, currentKeyID, err)
			}
			GetProviderKeySlots().Release(heldKeyID)
			heldKeyID = 0
			keys, err := ListEligibleSharedAPIKeys(ctx, db, job.ExecutionSource.ServiceProvider())
			if err != nil {
				_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, "failed to list keys for switch")
				return
			}
			next, ok := SelectRandomEligibleKey(keys, tried, GetProviderKeySlots())
			if !ok || !GetProviderKeySlots().Reserve(next.ID) {
				_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, result.ErrorMessage)
				return
			}
			heldKeyID = next.ID
			tried[next.ID] = struct{}{}
			currentKeyID = next.ID
			continue
		}

		msg := result.ErrorMessage
		if msg == "" {
			msg = "provider execution failed"
		}
		_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, msg)
		streams.Unregister(jobID)
		return
	}
}

// publishProviderJobExecuteStreamEvent forwards Execute-time stream events to
// subscribers. FinalUsage is withheld until settlement commits.
func publishProviderJobExecuteStreamEvent(streams *ProviderStreamRegistry, jobID uint, event provider.StreamEvent) {
	if event.Kind == provider.StreamEventFinalUsage {
		return
	}
	streams.Publish(jobID, ProviderStreamEvent{
		Kind:     string(event.Kind),
		Payload:  append([]byte(nil), event.Payload...),
		Terminal: event.Kind == provider.StreamEventFinish,
		Error:    event.Kind == provider.StreamEventError,
	})
}

// publishProviderJobSettlementTerminal emits FinalUsage then done after settlement commits.
func publishProviderJobSettlementTerminal(streams *ProviderStreamRegistry, jobID uint, usage *provider.NormalizedUsage) {
	provider.EmitFinalUsage(func(event provider.StreamEvent) {
		streams.Publish(jobID, ProviderStreamEvent{
			Kind:     string(event.Kind),
			Payload:  append([]byte(nil), event.Payload...),
			Terminal: true,
		})
	}, usage)
	streams.Publish(jobID, ProviderStreamEvent{Kind: "done", Terminal: true})
}

type providerSettleFunc func(
	ctx context.Context,
	db *gorm.DB,
	job *models.TaskJob,
	sharedAPIKeyID uint,
	keyOwnerUserID uint,
	keyOwnerAddress string,
	rawResultJSON string,
	formattedResultJSON string,
	usage provider.NormalizedUsage,
) error

var settleProviderTaskJobFn providerSettleFunc = SettleProviderTaskJob

// settleProviderJobWithRetry retries settlement until it commits. jobCtx cancellation
// triggers one final short-timeout attempt, then returns the last error.
func settleProviderJobWithRetry(
	jobCtx context.Context,
	settleCtx context.Context,
	db *gorm.DB,
	job *models.TaskJob,
	sharedAPIKeyID uint,
	keyOwnerUserID uint,
	keyOwnerAddress string,
	rawResultJSON string,
	formattedResultJSON string,
	usage provider.NormalizedUsage,
) error {
	for {
		err := settleProviderTaskJobFn(settleCtx, db, job, sharedAPIKeyID, keyOwnerUserID, keyOwnerAddress, rawResultJSON, formattedResultJSON, usage)
		if err == nil {
			return nil
		}
		log.Errorf("provider job %d settlement failed, retrying: %v", job.ID, err)
		select {
		case <-jobCtx.Done():
			finalCtx, cancel := context.WithTimeout(settleCtx, 15*time.Second)
			finalErr := settleProviderTaskJobFn(finalCtx, db, job, sharedAPIKeyID, keyOwnerUserID, keyOwnerAddress, rawResultJSON, formattedResultJSON, usage)
			cancel()
			if finalErr == nil {
				return nil
			}
			return finalErr
		case <-time.After(time.Second):
		}
	}
}
