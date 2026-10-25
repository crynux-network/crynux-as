package service

import (
	"net/http"
	"testing"
	"time"
)

func TestResolveTaskJobWaitTimeout(t *testing.T) {
	if got := ResolveTaskJobWaitTimeout(http.Header{}); got != DefaultTaskJobWaitTimeout {
		t.Fatalf("empty headers = %s, want %s", got, DefaultTaskJobWaitTimeout)
	}

	h := http.Header{}
	h.Set("X-Stainless-Timeout", "30")
	if got := ResolveTaskJobWaitTimeout(h); got != 30*time.Second+ClientTimeoutBuffer {
		t.Fatalf("overall timeout = %s, want %s", got, 30*time.Second+ClientTimeoutBuffer)
	}

	h = http.Header{}
	h.Set("X-Stainless-Read-Timeout", "45.5")
	want := time.Duration(45.5*float64(time.Second)) + ClientTimeoutBuffer
	if got := ResolveTaskJobWaitTimeout(h); got != want {
		t.Fatalf("read timeout = %s, want %s", got, want)
	}

	h = http.Header{}
	h.Set("X-Stainless-Timeout", "10")
	h.Set("X-Stainless-Read-Timeout", "99")
	if got := ResolveTaskJobWaitTimeout(h); got != 10*time.Second+ClientTimeoutBuffer {
		t.Fatalf("prefer overall timeout = %s", got)
	}

	h = http.Header{}
	h.Set("X-Stainless-Timeout", "0")
	if got := ResolveTaskJobWaitTimeout(h); got != DefaultTaskJobWaitTimeout {
		t.Fatalf("non-positive timeout = %s, want fallback", got)
	}
}
