package service

import (
	"crynux_as/config"
	"math/big"
	"testing"
)

func withPriorityBounds(t *testing.T, min, max string) {
	t.Helper()
	cfg := &config.AppConfig{}
	cfg.LLM.MinPriorityGwei = min
	cfg.LLM.MaxPriorityGwei = max
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })
}

func TestParsePriorityGweiAllowed(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	got, err := ParsePriorityGwei("34")
	if err != nil {
		t.Fatalf("ParsePriorityGwei: %v", err)
	}
	if got.Cmp(big.NewInt(34)) != 0 {
		t.Fatalf("priority = %s, want 34", got.String())
	}
}

func TestParsePriorityGweiRejected(t *testing.T) {
	withPriorityBounds(t, "10", "100")
	for _, value := range []string{"", "0", "9", "101", "1.5", "-1"} {
		if _, err := ParsePriorityGwei(value); err == nil {
			t.Fatalf("ParsePriorityGwei(%q) should fail", value)
		}
	}
}

func TestClampPriorityGwei(t *testing.T) {
	withPriorityBounds(t, "10", "100")
	low, err := ClampPriorityGwei(big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if low.Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("clamped low = %s, want 10", low.String())
	}
	high, err := ClampPriorityGwei(big.NewInt(1000))
	if err != nil {
		t.Fatal(err)
	}
	if high.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("clamped high = %s, want 100", high.String())
	}
}
