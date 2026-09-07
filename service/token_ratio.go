package service

import (
	"crynux_as/config"
	"errors"
	"fmt"
	"math"
)

const DefaultTokenRatioStored uint = 10

// DisplayTokenRatio converts the stored integer (ratio * 10) to the API float.
func DisplayTokenRatio(stored uint) float64 {
	return float64(stored) / 10.0
}

func configuredMaxTokenRatio() uint64 {
	cfg := config.GetConfig()
	if cfg == nil {
		return 0
	}
	return cfg.LLM.MaxTokenRatio
}

// ParseTokenRatio converts an API float to the stored integer and validates the
// allowed set (0.1..1.0 step 0.1, then 2..max_token_ratio step 1).
func ParseTokenRatio(display float64) (uint, error) {
	if math.IsNaN(display) || math.IsInf(display, 0) {
		return 0, errors.New("token_ratio is invalid")
	}
	maxRatio := configuredMaxTokenRatio()
	if maxRatio < 2 {
		return 0, errors.New("llm.max_token_ratio is not configured")
	}
	stored := uint(math.Round(display * 10))
	if !IsAllowedTokenRatio(stored) {
		return 0, fmt.Errorf(
			"token_ratio must be one of 0.1..1.0 step 0.1 or 2..%d step 1",
			maxRatio,
		)
	}
	if math.Abs(DisplayTokenRatio(stored)-display) > 1e-9 {
		return 0, fmt.Errorf(
			"token_ratio must be one of 0.1..1.0 step 0.1 or 2..%d step 1",
			maxRatio,
		)
	}
	return stored, nil
}

func IsAllowedTokenRatio(stored uint) bool {
	maxRatio := configuredMaxTokenRatio()
	if maxRatio < 2 {
		return false
	}
	if stored >= 1 && stored <= 10 {
		return true
	}
	if stored%10 != 0 {
		return false
	}
	display := stored / 10
	return display >= 2 && uint64(display) <= maxRatio
}
