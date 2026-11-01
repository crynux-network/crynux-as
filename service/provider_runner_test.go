package service

import (
	"context"
	"crynux_as/models"
	"crynux_as/provider"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSettleProviderJobWithRetryFinalAttemptAfterCancel(t *testing.T) {
	previous := settleProviderTaskJobFn
	t.Cleanup(func() {
		settleProviderTaskJobFn = previous
	})

	var calls atomic.Int32
	settleProviderTaskJobFn = func(
		ctx context.Context,
		db *gorm.DB,
		job *models.TaskJob,
		sharedAPIKeyID uint,
		keyOwnerUserID uint,
		keyOwnerAddress string,
		rawResultJSON string,
		formattedResultJSON string,
		usage provider.NormalizedUsage,
	) error {
		n := calls.Add(1)
		if n == 1 {
			return errors.New("transient settle failure")
		}
		if ctx.Err() != nil {
			t.Fatalf("final settle context already cancelled: %v", ctx.Err())
		}
		return nil
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	settleCtx := context.WithoutCancel(jobCtx)
	job := &models.TaskJob{ID: 42}

	errCh := make(chan error, 1)
	go func() {
		errCh <- settleProviderJobWithRetry(
			jobCtx,
			settleCtx,
			nil,
			job,
			1,
			2,
			"0xabc",
			`{}`,
			`{}`,
			provider.NormalizedUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("settleProviderJobWithRetry returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for settle retry")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("settle calls = %d, want 2", got)
	}
}

func TestSettleProviderJobWithRetryKeepsTryingWhileJobCtxAlive(t *testing.T) {
	previous := settleProviderTaskJobFn
	t.Cleanup(func() {
		settleProviderTaskJobFn = previous
	})

	var calls atomic.Int32
	settleProviderTaskJobFn = func(
		ctx context.Context,
		db *gorm.DB,
		job *models.TaskJob,
		sharedAPIKeyID uint,
		keyOwnerUserID uint,
		keyOwnerAddress string,
		rawResultJSON string,
		formattedResultJSON string,
		usage provider.NormalizedUsage,
	) error {
		if calls.Add(1) < 3 {
			return errors.New("transient settle failure")
		}
		return nil
	}

	jobCtx := context.Background()
	err := settleProviderJobWithRetry(
		jobCtx,
		jobCtx,
		nil,
		&models.TaskJob{ID: 7},
		1,
		2,
		"0xabc",
		`{}`,
		`{}`,
		provider.NormalizedUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
	)
	if err != nil {
		t.Fatalf("settleProviderJobWithRetry returned %v, want nil", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("settle calls = %d, want 3", got)
	}
}
