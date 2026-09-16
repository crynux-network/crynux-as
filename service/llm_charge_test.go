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
		priorityGwei          int64
		vramWeight            float64
		constantSeconds       float64
		secondsPerInputToken  float64
		secondsPerOutputToken float64
		creditsPerGwei        string
		expected              int64
	}{
		{
			name:                  "basic",
			promptTokens:          1000,
			completionTokens:      100,
			priorityGwei:          10,
			vramWeight:            1,
			constantSeconds:       30,
			secondsPerInputToken:  0.001,
			secondsPerOutputToken: 0.01,
			creditsPerGwei:        "1",
			// estimated = 32, billable = 320
			expected: 320,
		},
		{
			name:             "credits_per_gwei scales",
			promptTokens:     0,
			completionTokens: 0,
			priorityGwei:     4,
			vramWeight:       2,
			constantSeconds:  5,
			creditsPerGwei:   "3",
			// billable = 4 * 5 * 2 = 40; credits = 120
			expected: 120,
		},
		{
			name:             "minimum one credit when floor is zero",
			promptTokens:     0,
			completionTokens: 0,
			priorityGwei:     1,
			vramWeight:       1,
			constantSeconds:  0.29,
			creditsPerGwei:   "1",
			expected:         1,
		},
		{
			name:             "decimal credits_per_gwei",
			promptTokens:     0,
			completionTokens: 0,
			priorityGwei:     1_000_000_000,
			vramWeight:       1,
			constantSeconds:  1,
			creditsPerGwei:   "0.000001",
			// billable = 1e9; credits = 1000
			expected: 1000,
		},
		{
			name:             "minimum one credit with tiny credits_per_gwei",
			promptTokens:     0,
			completionTokens: 0,
			priorityGwei:     273,
			vramWeight:       3,
			constantSeconds:  455.5,
			creditsPerGwei:   "0.000001",
			// billable ≈ 373083; floor(billable * G) = 0; credits = 1
			expected: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalcCredits(
				tc.promptTokens,
				tc.completionTokens,
				big.NewInt(tc.priorityGwei),
				tc.vramWeight,
				tc.constantSeconds,
				tc.secondsPerInputToken,
				tc.secondsPerOutputToken,
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
