package service

import (
	"errors"
	"math"
)

const DefaultTokenRatioStored uint = 10

var allowedTokenRatios = map[uint]struct{}{
	1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}, 9: {}, 10: {},
	20: {}, 30: {}, 40: {}, 50: {}, 60: {}, 70: {}, 80: {}, 90: {}, 100: {},
}

// DisplayTokenRatio converts the stored integer (ratio * 10) to the API float.
func DisplayTokenRatio(stored uint) float64 {
	return float64(stored) / 10.0
}

// ParseTokenRatio converts an API float to the stored integer and validates the
// allowed 19-tier set (0.1..1.0 step 0.1, then 2..10 step 1).
func ParseTokenRatio(display float64) (uint, error) {
	if math.IsNaN(display) || math.IsInf(display, 0) {
		return 0, errors.New("token_ratio is invalid")
	}
	stored := uint(math.Round(display * 10))
	if !IsAllowedTokenRatio(stored) {
		return 0, errors.New("token_ratio must be one of 0.1..1.0 step 0.1 or 2..10 step 1")
	}
	if math.Abs(DisplayTokenRatio(stored)-display) > 1e-9 {
		return 0, errors.New("token_ratio must be one of 0.1..1.0 step 0.1 or 2..10 step 1")
	}
	return stored, nil
}

func IsAllowedTokenRatio(stored uint) bool {
	_, ok := allowedTokenRatios[stored]
	return ok
}
