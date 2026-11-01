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

func TestPublishProviderJobStreamWithholdsFinalUsageUntilSettlement(t *testing.T) {
	streams := NewProviderStreamRegistry(8)
	const jobID uint = 99
	ch := streams.Register(jobID)
	t.Cleanup(func() { streams.Unregister(jobID) })

	provider.EmitContentDelta(func(event provider.StreamEvent) {
		publishProviderJobExecuteStreamEvent(streams, jobID, event)
	}, "hi")
	provider.EmitFinish(func(event provider.StreamEvent) {
		publishProviderJobExecuteStreamEvent(streams, jobID, event)
	}, "stop")
	provider.EmitFinalUsage(func(event provider.StreamEvent) {
		publishProviderJobExecuteStreamEvent(streams, jobID, event)
	}, &provider.NormalizedUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3})

	got := drainProviderStreamEvents(t, ch, 2)
	if got[0].Kind != string(provider.StreamEventContentDelta) {
		t.Fatalf("event 0 kind = %q, want content_delta", got[0].Kind)
	}
	if got[1].Kind != string(provider.StreamEventFinish) {
		t.Fatalf("event 1 kind = %q, want finish", got[1].Kind)
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected pre-settlement event %#v", ev)
	default:
	}

	publishProviderJobSettlementTerminal(streams, jobID, &provider.NormalizedUsage{
		InputTokens: 1, OutputTokens: 2, TotalTokens: 3,
	})

	got = drainProviderStreamEvents(t, ch, 2)
	if got[0].Kind != string(provider.StreamEventFinalUsage) {
		t.Fatalf("post-settle event 0 kind = %q, want final_usage", got[0].Kind)
	}
	if !got[0].Terminal {
		t.Fatal("final_usage must be terminal")
	}
	if got[1].Kind != "done" {
		t.Fatalf("post-settle event 1 kind = %q, want done", got[1].Kind)
	}
	if !got[1].Terminal {
		t.Fatal("done must be terminal")
	}
}

func drainProviderStreamEvents(t *testing.T, ch <-chan ProviderStreamEvent, n int) []ProviderStreamEvent {
	t.Helper()
	out := make([]ProviderStreamEvent, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d events, want %d", len(out), n)
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("timed out after %d events, want %d", len(out), n)
		}
	}
	return out
}
