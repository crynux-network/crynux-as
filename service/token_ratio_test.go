package service

import (
	"testing"
)

func TestParseTokenRatioAllowed(t *testing.T) {
	cases := []float64{0.1, 0.5, 1, 1.0, 2, 10}
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
	cases := []float64{0, 0.15, 1.5, 1.1, 11, -1}
	for _, display := range cases {
		if _, err := ParseTokenRatio(display); err == nil {
			t.Fatalf("ParseTokenRatio(%v) should fail", display)
		}
	}
}
