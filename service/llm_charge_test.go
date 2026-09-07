package service

import (
	"math/big"
	"testing"
)

func TestCalcCredits(t *testing.T) {
	cases := []struct {
		name                  string
		promptTokens          uint64
		completionTokens      uint64
		tokenRatio            uint
		vramWeight            float64
		constantSeconds       float64
		secondsPerInputToken  float64
		secondsPerOutputToken float64
		referencePriority     int64
		creditsPerGwei        uint64
		expected              int64
	}{
		{
			name:             "basic",
			promptTokens:     1000,
			completionTokens: 100,
			tokenRatio:       10,
			vramWeight:       1,
			constantSeconds:  30,
			secondsPerInputToken: 0.001,
			secondsPerOutputToken: 0.01,
			referencePriority: 10,
			creditsPerGwei:    1,
			// estimated = 32, billable = 320
			expected: 320,
		},
		{
			name:             "credits_per_gwei scales",
			promptTokens:     0,
			completionTokens: 0,
			tokenRatio:       10,
			vramWeight:       2,
			constantSeconds:  5,
			referencePriority: 4,
			creditsPerGwei:    3,
			// billable = 4 * 1 * 5 * 2 = 40; credits = 120
			expected: 120,
		},
		{
			name:             "truncates toward zero",
			promptTokens:     0,
			completionTokens: 0,
			tokenRatio:       1,
			vramWeight:       1,
			constantSeconds:  2.9,
			referencePriority: 1,
			creditsPerGwei:    1,
			expected:          0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalcCredits(
				tc.promptTokens,
				tc.completionTokens,
				tc.tokenRatio,
				tc.vramWeight,
				tc.constantSeconds,
				tc.secondsPerInputToken,
				tc.secondsPerOutputToken,
				big.NewInt(tc.referencePriority),
				tc.creditsPerGwei,
			)
			if err != nil {
				t.Fatalf("CalcCredits: %v", err)
			}
			if got.Cmp(big.NewInt(tc.expected)) != 0 {
				t.Fatalf("CalcCredits = %s, want %d", got.String(), tc.expected)
			}
		})
	}
}
