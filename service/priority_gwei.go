package service

import (
	"crynux_as/config"
	"errors"
	"fmt"
	"math/big"
)

// ParsePriorityGwei parses a decimal integer Gwei string and validates it against
// configured min_priority_gwei and max_priority_gwei inclusive bounds.
func ParsePriorityGwei(value string) (*big.Int, error) {
	if value == "" {
		return nil, errors.New("priority_gwei must be a positive decimal integer")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return nil, errors.New("priority_gwei must be a positive decimal integer")
		}
	}
	priority, ok := new(big.Int).SetString(value, 10)
	if !ok || priority.Sign() <= 0 {
		return nil, errors.New("priority_gwei must be a positive decimal integer")
	}
	if err := ValidatePriorityGwei(priority); err != nil {
		return nil, err
	}
	return priority, nil
}

// ValidatePriorityGwei checks priority is within configured inclusive bounds.
func ValidatePriorityGwei(priority *big.Int) error {
	if priority == nil || priority.Sign() <= 0 {
		return errors.New("priority_gwei must be positive")
	}
	cfg := config.GetConfig()
	if cfg == nil {
		return errors.New("llm priority bounds are not configured")
	}
	minPriority, err := cfg.ParseMinPriorityGwei()
	if err != nil {
		return fmt.Errorf("llm.min_priority_gwei is invalid: %w", err)
	}
	maxPriority, err := cfg.ParseMaxPriorityGwei()
	if err != nil {
		return fmt.Errorf("llm.max_priority_gwei is invalid: %w", err)
	}
	if priority.Cmp(minPriority) < 0 || priority.Cmp(maxPriority) > 0 {
		return fmt.Errorf(
			"priority_gwei must be between %s and %s",
			minPriority.String(),
			maxPriority.String(),
		)
	}
	return nil
}

// ClampPriorityGwei clamps priority into [min_priority_gwei, max_priority_gwei].
func ClampPriorityGwei(priority *big.Int) (*big.Int, error) {
	if priority == nil {
		return nil, errors.New("priority_gwei is required")
	}
	cfg := config.GetConfig()
	if cfg == nil {
		return nil, errors.New("llm priority bounds are not configured")
	}
	minPriority, err := cfg.ParseMinPriorityGwei()
	if err != nil {
		return nil, fmt.Errorf("llm.min_priority_gwei is invalid: %w", err)
	}
	maxPriority, err := cfg.ParseMaxPriorityGwei()
	if err != nil {
		return nil, fmt.Errorf("llm.max_priority_gwei is invalid: %w", err)
	}
	out := new(big.Int).Set(priority)
	if out.Cmp(minPriority) < 0 {
		out.Set(minPriority)
	}
	if out.Cmp(maxPriority) > 0 {
		out.Set(maxPriority)
	}
	return out, nil
}

// InitialProjectPriorityGwei returns the Queue Median Hint clamped to configured bounds.
func InitialProjectPriorityGwei() (*big.Int, error) {
	median, err := ResolveQueueMedianHint()
	if err != nil {
		return nil, err
	}
	return ClampPriorityGwei(median)
}
