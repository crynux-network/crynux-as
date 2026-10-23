package models

import (
	"strings"
	"testing"
)

func TestValidateImageTaskArgsImagesSizeEmptyPasses(t *testing.T) {
	if err := ValidateImageTaskArgsImagesSize(`{"prompt":"p"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateImageTaskArgsImagesSize(`{"prompt":"p","images":[]}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateImageTaskArgsImagesSizeWithinLimitPasses(t *testing.T) {
	image := "data:image/png;base64," + strings.Repeat("A", 1000)
	payload := `{"prompt":"p","images":["` + image + `"]}`
	if err := ValidateImageTaskArgsImagesSize(payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateImageTaskArgsImagesSizeOverLimitFails(t *testing.T) {
	image := "data:image/png;base64," + strings.Repeat("A", MaxImageTaskArgsImagesBytes)
	payload := `{"prompt":"p","images":["` + image + `"]}`
	err := ValidateImageTaskArgsImagesSize(payload)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "task_args.images total size exceeds the 1mb limit" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateImageTaskArgsImagesSizeSumsEntries(t *testing.T) {
	half := strings.Repeat("B", MaxImageTaskArgsImagesBytes/2+1)
	payload := `{"prompt":"p","images":["` + half + `","` + half + `"]}`
	if err := ValidateImageTaskArgsImagesSize(payload); err == nil {
		t.Fatal("expected summed size to exceed the limit")
	}
}
