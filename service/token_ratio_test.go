package service

import (
	"math/big"
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

func TestCalcCredits(t *testing.T) {
	prices := LLMPrices{PromptCreditsPerToken: 1, CompletionCreditsPerToken: 2}
	// (100*10*1 + 50*10*2) / 10 = (1000 + 1000) / 10 = 200
	got := CalcCredits(100, 50, 10, prices)
	if got.Cmp(big.NewInt(200)) != 0 {
		t.Fatalf("got %s want 200", got.String())
	}
	// ratio 0.5 stored as 5: (100*5*1 + 50*5*2) / 10 = (500 + 500) / 10 = 100
	got = CalcCredits(100, 50, 5, prices)
	if got.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("got %s want 100", got.String())
	}
}
