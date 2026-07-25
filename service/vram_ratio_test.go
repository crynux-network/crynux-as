package service

import (
	"crynux_as/config"
	"testing"
)

func TestSelectVramRatio(t *testing.T) {
	tiers := []config.VramRatioConfig{
		{MaxVram: 24, Ratio: 0.5},
		{MaxVram: 96, Ratio: 1.5},
	}

	cases := []struct {
		name     string
		vram     uint64
		expected uint
	}{
		{name: "zero matches first tier", vram: 0, expected: 5},
		{name: "first tier upper bound inclusive", vram: 24, expected: 5},
		{name: "second tier lower bound", vram: 25, expected: 15},
		{name: "second tier upper bound inclusive", vram: 96, expected: 15},
		{name: "above last tier uses last tier", vram: 200, expected: 15},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectVramRatio(tiers, tc.vram)
			if got != tc.expected {
				t.Fatalf("SelectVramRatio(%d) = %d, want %d", tc.vram, got, tc.expected)
			}
		})
	}
}
