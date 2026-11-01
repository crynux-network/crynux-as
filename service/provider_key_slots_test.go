package service

import "testing"

func TestProviderKeySlotSwitchFinalReleaseTargetsHeldKey(t *testing.T) {
	slots := NewProviderKeySlotManager(2)
	const keyA, keyB uint = 1, 2

	if !slots.Reserve(keyA) {
		t.Fatal("reserve keyA")
	}
	// Concurrent job still holds keyA after this job switches away.
	if !slots.Reserve(keyA) {
		t.Fatal("reserve second keyA slot")
	}

	heldKeyID := keyA
	slots.Release(heldKeyID)
	heldKeyID = 0
	if !slots.Reserve(keyB) {
		t.Fatal("reserve keyB after switch")
	}
	heldKeyID = keyB

	if heldKeyID != 0 {
		slots.Release(heldKeyID)
		heldKeyID = 0
	}

	if got := slots.ActiveCount(keyA); got != 1 {
		t.Fatalf("keyA active = %d, want 1 from the concurrent job", got)
	}
	if got := slots.ActiveCount(keyB); got != 0 {
		t.Fatalf("keyB active = %d, want 0 after final release", got)
	}
	if heldKeyID != 0 {
		t.Fatalf("heldKeyID = %d, want 0", heldKeyID)
	}
}
