package service

import (
	"crynux_as/config"
	"testing"
)

func withMaxTokenRatio(t *testing.T, max uint64) {
	t.Helper()
	cfg := &config.AppConfig{}
	cfg.LLM.MaxTokenRatio = max
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
	})
}

func TestParseTokenRatioAllowed(t *testing.T) {
	withMaxTokenRatio(t, 30)
	cases := []float64{0.1, 0.5, 1, 1.0, 2, 10, 20, 30}
	for _, display := range cases {
		stored, err := ParseTokenRatio(display)
		if err != nil {
			t.Fatalf("ParseTokenRatio(%v): %v", display, err)
		}
		if DisplayTokenRatio(stored) != float64(stored)/10.0 {
			t.Fatalf("display mismatch for %v", display)
		}
	}
}

func TestParseTokenRatioRejected(t *testing.T) {
	withMaxTokenRatio(t, 30)
	cases := []float64{0, 0.15, 1.5, 1.1, 31, 40, -1}
	for _, display := range cases {
		if _, err := ParseTokenRatio(display); err == nil {
			t.Fatalf("ParseTokenRatio(%v) should fail", display)
		}
	}
}
