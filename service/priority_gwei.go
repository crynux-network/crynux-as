package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"fmt"
	"math/big"
)

const DefaultAutoQueuePosition = 50

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

// ParseAutoMaxCreditsPerTask parses a positive decimal integer Credits string.
func ParseAutoMaxCreditsPerTask(value string) (*big.Int, error) {
	if value == "" {
		return nil, errors.New("auto_max_credits_per_task must be a positive decimal integer")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return nil, errors.New("auto_max_credits_per_task must be a positive decimal integer")
		}
	}
	credits, ok := new(big.Int).SetString(value, 10)
	if !ok || credits.Sign() <= 0 {
		return nil, errors.New("auto_max_credits_per_task must be a positive decimal integer")
	}
	return credits, nil
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

// ParseCostLevelMode validates cost_level_mode.
func ParseCostLevelMode(value string) (string, error) {
	switch value {
	case models.CostLevelModeStatic, models.CostLevelModeAuto:
		return value, nil
	default:
		return "", errors.New("cost_level_mode must be static or auto")
	}
}

// ParseAutoQueuePosition validates auto_queue_position in [0, 100].
func ParseAutoQueuePosition(value int) (int, error) {
	if value < 0 || value > 100 {
		return 0, errors.New("auto_queue_position must be between 0 and 100")
	}
	return value, nil
}

// InitialProjectPriorityGwei returns the Queue Median Hint clamped to configured bounds.
func InitialProjectPriorityGwei() (*big.Int, error) {
	median, err := ResolveQueueMedianHint()
	if err != nil {
		return nil, err
	}
	return ClampPriorityGwei(median)
}

// DefaultAutoMaxCreditsPerTask returns llm.default_auto_max_credits_per_task.
func DefaultAutoMaxCreditsPerTask() (*big.Int, error) {
	cfg := config.GetConfig()
	if cfg == nil {
		return nil, errors.New("llm default auto max credits is not configured")
	}
	return cfg.ParseDefaultAutoMaxCreditsPerTask()
}

// ResolveEffectivePriorityGwei resolves the request Cost Level from the project mode.
// Auto mode returns the queue-position target without applying Max Credits; that
// cap is applied later during task-fee estimation for the concrete workload.
func ResolveEffectivePriorityGwei(project *models.Project) (*big.Int, error) {
	if project == nil {
		return nil, errors.New("project is required")
	}
	mode := project.CostLevelMode
	if mode == "" {
		mode = models.CostLevelModeStatic
	}
	switch mode {
	case models.CostLevelModeStatic:
		if project.PriorityGwei.Sign() <= 0 {
			return nil, errors.New("priority_gwei must be positive")
		}
		return new(big.Int).Set(&project.PriorityGwei.Int), nil
	case models.CostLevelModeAuto:
		return resolveAutoPriorityGwei(project)
	default:
		return nil, fmt.Errorf("invalid cost_level_mode: %s", mode)
	}
}

func resolveAutoPriorityGwei(project *models.Project) (*big.Int, error) {
	if err := requireAutoMaxCreditsPerTask(project); err != nil {
		return nil, err
	}
	position := DefaultAutoQueuePosition
	if project.AutoQueuePosition != nil {
		parsed, err := ParseAutoQueuePosition(*project.AutoQueuePosition)
		if err != nil {
			return nil, err
		}
		position = parsed
	}

	var effective *big.Int
	highest, lowest, ok := ResolveQueuePriorityBounds()
	// Relay may report lowest_priority_gwei as 0 when the queue floor is
	// below one Gwei after integer division. Zero lowest is usable for the
	// linear map; only a non-positive highest makes bounds unusable.
	if ok &&
		highest != nil &&
		lowest != nil &&
		highest.Sign() > 0 &&
		lowest.Sign() >= 0 &&
		highest.Cmp(lowest) >= 0 {
		diff := new(big.Int).Sub(highest, lowest)
		diff.Mul(diff, big.NewInt(int64(position)))
		diff.Div(diff, big.NewInt(100))
		effective = new(big.Int).Add(lowest, diff)
	} else {
		cfg := config.GetConfig()
		if cfg == nil {
			return nil, errors.New("llm priority bounds are not configured")
		}
		minPriority, err := cfg.ParseMinPriorityGwei()
		if err != nil {
			return nil, fmt.Errorf("llm.min_priority_gwei is invalid: %w", err)
		}
		effective = minPriority
	}

	return ClampPriorityGwei(effective)
}

func requireAutoMaxCreditsPerTask(project *models.Project) error {
	if project.AutoMaxCreditsPerTask == nil || project.AutoMaxCreditsPerTask.Sign() <= 0 {
		return errors.New("auto_max_credits_per_task is required for auto mode")
	}
	return nil
}
