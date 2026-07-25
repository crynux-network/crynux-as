package service

import (
	"crynux_as/config"
)

// SelectVramRatio maps an effective VRAM value in GB to the stored VRAM tier
// ratio (display * 10). Tiers are ordered by ascending max_vram and the first
// tier with vram <= max_vram matches. VRAM greater than the last tier's
// max_vram uses the last tier as the catch-all.
func SelectVramRatio(tiers []config.VramRatioConfig, vram uint64) uint {
	selected := tiers[len(tiers)-1]
	for _, tier := range tiers {
		if vram <= tier.MaxVram {
			selected = tier
			break
		}
	}
	stored, err := config.VramRatioStored(selected.Ratio)
	if err != nil {
		// Config validation rejects invalid ratios at startup.
		panic(err)
	}
	return stored
}
