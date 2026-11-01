package provider

import (
	"context"
	"testing"
	"time"
)

func TestCancelledExecuteResultUsesShutdownClass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := cancelledExecuteResult(ctx)
	if result.ErrorClass != ErrorCancelledByShutdown {
		t.Fatalf("ErrorClass = %s, want %s", result.ErrorClass, ErrorCancelledByShutdown)
	}
	if result.Acceptance != AcceptanceUncertain {
		t.Fatalf("Acceptance = %s, want %s", result.Acceptance, AcceptanceUncertain)
	}
	if !result.AcceptanceKnown {
		t.Fatal("AcceptanceKnown = false, want true")
	}
}

func TestCancelledExecuteResultDeadlineRemainsUncertain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	result := cancelledExecuteResult(ctx)
	if result.ErrorClass != ErrorAcceptanceUncertain {
		t.Fatalf("ErrorClass = %s, want %s", result.ErrorClass, ErrorAcceptanceUncertain)
	}
}
