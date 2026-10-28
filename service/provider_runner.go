package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func RunProviderJob(ctx context.Context, db *gorm.DB, jobID, sharedAPIKeyID uint) {
	cfg := config.GetConfig()
	tried := map[uint]struct{}{sharedAPIKeyID: {}}
	currentKeyID := sharedAPIKeyID
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
			streams.Publish(jobID, ProviderStreamEvent{
				Kind:     string(event.Kind),
				Payload:  append([]byte(nil), event.Payload...),
				Terminal: event.Kind == provider.StreamEventFinish || event.Kind == provider.StreamEventFinalUsage,
				Error:    event.Kind == provider.StreamEventError,
			})
		}
		result, execErr := adapter.Execute(ctx, secret, job.APIType, job.Model, []byte(job.TaskArgsJSON), attempts-1, sink)
		ClearBytes(secret)
		if execErr != nil {
			log.Errorf("provider job %d execute error: %v", jobID, execErr)
		}

		if result.HasConfirmedUsage() && len(result.FinalResultJSON) > 0 {
			_ = ClearSharedAPIKeyUnavailable(ctx, db, currentKeyID)
			var owner models.User
			if err := db.WithContext(ctx).First(&owner, key.UserID).Error; err != nil {
				log.Errorf("provider job %d load key owner failed: %v", jobID, err)
				_ = FailProviderTaskJob(ctx, db, jobID, &currentKeyID, "key owner unavailable after successful upstream")
				return
			}
			formatted := string(result.FinalResultJSON)
			raw := formatted
			for {
				err := SettleProviderTaskJob(ctx, db, job, currentKeyID, owner.ID, owner.Address, raw, formatted, *result.FinalUsage)
				if err == nil {
					streams.Publish(jobID, ProviderStreamEvent{Kind: "done", Terminal: true})
					streams.Unregister(jobID)
					return
				}
				log.Errorf("provider job %d settlement failed, retrying: %v", jobID, err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
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
			GetProviderKeySlots().Release(currentKeyID)
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
