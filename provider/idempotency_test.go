package provider

import "testing"

func TestProviderIdempotencyKey(t *testing.T) {
	got := ProviderIdempotencyKey(42, 0)
	if got != "crynux-as-42-0" {
		t.Fatalf("ProviderIdempotencyKey(42, 0) = %q, want crynux-as-42-0", got)
	}
	got = ProviderIdempotencyKey(7, 3)
	if got != "crynux-as-7-3" {
		t.Fatalf("ProviderIdempotencyKey(7, 3) = %q, want crynux-as-7-3", got)
	}
}
