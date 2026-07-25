package service

import (
	"math/big"
	"testing"
)

func TestCalcCredits(t *testing.T) {
	prices := LLMPrices{
		PromptCreditsPerToken:     2,
		CompletionCreditsPerToken: 3,
	}

	cases := []struct {
		name             string
		promptTokens     uint64
		completionTokens uint64
		tokenRatio       uint
		vramRatio        uint
		expected         int64
	}{
		{
			name:         "unit ratios",
			promptTokens: 100, completionTokens: 50,
			tokenRatio: 10, vramRatio: 10,
			// (100*10*10*2 + 50*10*10*3) / 100 = (20000 + 15000) / 100
			expected: 350,
		},
		{
			name:         "half vram ratio",
			promptTokens: 100, completionTokens: 50,
			tokenRatio: 10, vramRatio: 5,
			expected: 175,
		},
		{
			name:         "scaled vram and token ratio",
			promptTokens: 100, completionTokens: 50,
			tokenRatio: 20, vramRatio: 15,
			expected: 1050,
		},
		{
			name:         "truncates toward zero",
			promptTokens: 1, completionTokens: 0,
			tokenRatio: 1, vramRatio: 5,
			// 1*1*5*2 / 100 = 10 / 100 = 0
			expected: 0,
		},
		{
			name:         "zero tokens",
			promptTokens: 0, completionTokens: 0,
			tokenRatio: 10, vramRatio: 10,
			expected: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CalcCredits(tc.promptTokens, tc.completionTokens, tc.tokenRatio, tc.vramRatio, prices)
			if got.Cmp(big.NewInt(tc.expected)) != 0 {
				t.Fatalf("CalcCredits = %s, want %d", got.String(), tc.expected)
			}
		})
	}
}
