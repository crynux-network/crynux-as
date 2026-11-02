package models

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateCrynuxTaskArgsSizeAtLimitPasses(t *testing.T) {
	payload := `{"prompt":"` + strings.Repeat("A", 100) + `"}`
	if err := ValidateCrynuxTaskArgsSize(payload, uint64(len(payload))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCrynuxTaskArgsSizeOverLimitFails(t *testing.T) {
	payload := `{"prompt":"` + strings.Repeat("A", 100) + `"}`
	limit := len(payload) - 1
	err := ValidateCrynuxTaskArgsSize(payload, uint64(limit))
	if err == nil {
		t.Fatal("expected error")
	}
	want := "task_args size exceeds the " + strconv.Itoa(limit) + "-byte limit"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestValidateCrynuxTaskArgsSizeCountsAllFields(t *testing.T) {
	image := "data:image/png;base64," + strings.Repeat("A", 400)
	imagesOnly := `{"images":["` + image + `"]}`
	full := `{"prompt":"` + strings.Repeat("p", 200) + `","images":["` + image + `"]}`
	limit := uint64(len(imagesOnly) + 10)
	if err := ValidateCrynuxTaskArgsSize(imagesOnly, limit); err != nil {
		t.Fatalf("images-only payload rejected: %v", err)
	}
	if err := ValidateCrynuxTaskArgsSize(full, limit); err == nil {
		t.Fatal("expected non-image fields to count toward the limit")
	}
}
