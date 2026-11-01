package service

import (
	"testing"
	"time"
)

func TestProviderStreamRegistryDetachAll(t *testing.T) {
	r := NewProviderStreamRegistry(4)
	ch1 := r.Register(1)
	ch2 := r.Register(2)
	if got := r.SubscriberCount(); got != 2 {
		t.Fatalf("subscriber count = %d, want 2", got)
	}

	r.Publish(1, ProviderStreamEvent{Kind: "content_delta", Payload: []byte("a")})
	select {
	case ev := <-ch1:
		if string(ev.Payload) != "a" {
			t.Fatalf("unexpected payload %q", ev.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first event")
	}

	r.DetachAll()
	if got := r.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count after DetachAll = %d, want 0", got)
	}

	select {
	case _, ok := <-ch1:
		if ok {
			t.Fatal("ch1 should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ch1 close")
	}
	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatal("ch2 should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ch2 close")
	}

	r.Publish(1, ProviderStreamEvent{Kind: "content_delta", Payload: []byte("b")})
	r.DetachAll()
	if got := r.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count after empty DetachAll = %d, want 0", got)
	}
}
