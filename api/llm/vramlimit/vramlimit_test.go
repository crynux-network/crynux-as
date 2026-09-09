package vramlimit

import "testing"

func TestResolveUserVramLimitFromBody(t *testing.T) {
	bodyVramLimit := uint64(80)
	userVram, err := ResolveUserVramLimit(&bodyVramLimit, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userVram == nil || *userVram != 80 {
		t.Fatalf("expected body VRAM limit 80, got %v", userVram)
	}
}

func TestResolveUserVramLimitPathOverridesBody(t *testing.T) {
	bodyVramLimit := uint64(24)
	userVram, err := ResolveUserVramLimit(&bodyVramLimit, "80")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userVram == nil || *userVram != 80 {
		t.Fatalf("expected path VRAM limit 80, got %v", userVram)
	}
}

func TestResolveUserVramLimitNeitherSet(t *testing.T) {
	userVram, err := ResolveUserVramLimit(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userVram != nil {
		t.Fatalf("expected nil user VRAM limit, got %v", userVram)
	}
}

func TestResolveUserVramLimitInvalidPath(t *testing.T) {
	_, err := ResolveUserVramLimit(nil, "invalid")
	if err == nil {
		t.Fatalf("expected invalid path VRAM limit error")
	}
}
