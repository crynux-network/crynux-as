package service

import (
	"context"
	"crynux_as/config"
	"errors"
	"fmt"
	"math"
	"math/big"
)

type CalcBillableInput struct {
	PriorityGwei          *big.Int
	EffectiveVram         uint64
	BaseVram              uint64
	ConstantSeconds       float64
	SecondsPerInputToken  float64
	SecondsPerOutputToken float64
	PromptTokens          uint64
	CompletionTokens      uint64
}

type CalcBillableResult struct {
	BillableGwei         *big.Float
	TaskFeeGwei          *big.Int
	EstimatedNodeSeconds float64
	VramWeight           float64
}

type CalcTaskFeeResult struct {
	TaskFeeGwei           *big.Int
	Credits               *big.Int
	MedianPriorityGwei    *big.Int
	EstimatedNodeSeconds  float64
	VramWeight            float64
	ConstantSeconds       float64
	SecondsPerInputToken  float64
	SecondsPerOutputToken float64
}

// CalcBillableGwei computes shared billable_gwei from project Cost Level
// priority_gwei, estimated node seconds, and VRAM weight.
func CalcBillableGwei(in CalcBillableInput) (*CalcBillableResult, error) {
	if in.PriorityGwei == nil {
		return nil, errors.New("priority_gwei is required")
	}
	if in.PriorityGwei.Sign() <= 0 {
		return nil, errors.New("priority_gwei must be positive")
	}
	if in.BaseVram == 0 {
		return nil, errors.New("base_vram must be a positive integer")
	}
	if in.EffectiveVram == 0 {
		return nil, errors.New("effective_vram must be a positive integer")
	}
	if err := validateExecutionTimeCoefficients(
		in.ConstantSeconds,
		in.SecondsPerInputToken,
		in.SecondsPerOutputToken,
	); err != nil {
		return nil, err
	}

	estimatedNodeSeconds := in.ConstantSeconds +
		in.SecondsPerInputToken*float64(in.PromptTokens) +
		in.SecondsPerOutputToken*float64(in.CompletionTokens)

	vramDemand := in.EffectiveVram
	if vramDemand < in.BaseVram {
		vramDemand = in.BaseVram
	}
	vramWeight := float64(vramDemand) / float64(in.BaseVram)

	billableGwei, taskFeeGwei, err := billableFromParts(
		in.PriorityGwei,
		estimatedNodeSeconds,
		vramWeight,
	)
	if err != nil {
		return nil, err
	}

	return &CalcBillableResult{
		BillableGwei:         billableGwei,
		TaskFeeGwei:          taskFeeGwei,
		EstimatedNodeSeconds: estimatedNodeSeconds,
		VramWeight:           vramWeight,
	}, nil
}

func billableFromParts(
	priorityGwei *big.Int,
	estimatedNodeSeconds float64,
	vramWeight float64,
) (*big.Float, *big.Int, error) {
	if math.IsNaN(estimatedNodeSeconds) || math.IsInf(estimatedNodeSeconds, 0) || estimatedNodeSeconds < 0 {
		return nil, nil, errors.New("estimated_node_seconds must be finite and non-negative")
	}
	if math.IsNaN(vramWeight) || math.IsInf(vramWeight, 0) || vramWeight <= 0 {
		return nil, nil, errors.New("vram_weight must be a positive finite number")
	}

	product := new(big.Float).SetInt(priorityGwei)
	product.Mul(product, big.NewFloat(estimatedNodeSeconds*vramWeight))
	if product.Sign() < 0 {
		return nil, nil, errors.New("billable_gwei must be non-negative")
	}

	fee, _ := product.Int(nil)
	if fee == nil {
		fee = big.NewInt(0)
	}
	return product, fee, nil
}

// ParseCreditsPerGwei parses a positive decimal string into a *big.Rat.
func ParseCreditsPerGwei(value string) (*big.Rat, error) {
	return config.ParsePositiveDecimalRate(value)
}

// CalcCreditsFromBillable returns max(1, floor(billable_gwei * credits_per_gwei)).
func CalcCreditsFromBillable(billableGwei *big.Float, creditsPerGwei *big.Rat) (*big.Int, error) {
	if billableGwei == nil {
		return nil, errors.New("billable_gwei is required")
	}
	if creditsPerGwei == nil || creditsPerGwei.Sign() <= 0 {
		return nil, errors.New("credits_per_gwei must be positive")
	}
	if billableGwei.Sign() < 0 {
		return nil, errors.New("billable_gwei must be non-negative")
	}

	billableRat, _ := billableGwei.Rat(nil)
	if billableRat == nil {
		return nil, errors.New("billable_gwei is invalid")
	}
	product := new(big.Rat).Mul(billableRat, creditsPerGwei)
	if product.Sign() < 0 {
		return nil, errors.New("credits must be non-negative")
	}
	credits := new(big.Int).Quo(product.Num(), product.Denom())
	if credits.Sign() == 0 {
		credits = big.NewInt(1)
	}
	return credits, nil
}

// CalcCredits computes Credits from usage tokens, persisted coefficients, and
// persisted vram_weight using the shared billable_gwei formula.
func CalcCredits(
	promptTokens, completionTokens uint64,
	priorityGwei *big.Int,
	vramWeight float64,
	constantSeconds, secondsPerInputToken, secondsPerOutputToken float64,
	creditsPerGwei string,
) (*big.Int, error) {
	if priorityGwei == nil {
		return nil, errors.New("priority_gwei is required")
	}
	if priorityGwei.Sign() <= 0 {
		return nil, errors.New("priority_gwei must be positive")
	}
	rate, err := ParseCreditsPerGwei(creditsPerGwei)
	if err != nil {
		return nil, err
	}
	if err := validateExecutionTimeCoefficients(
		constantSeconds,
		secondsPerInputToken,
		secondsPerOutputToken,
	); err != nil {
		return nil, err
	}

	estimatedNodeSeconds := constantSeconds +
		secondsPerInputToken*float64(promptTokens) +
		secondsPerOutputToken*float64(completionTokens)

	billableGwei, _, err := billableFromParts(
		priorityGwei,
		estimatedNodeSeconds,
		vramWeight,
	)
	if err != nil {
		return nil, err
	}
	return CalcCreditsFromBillable(billableGwei, rate)
}

func validateExecutionTimeCoefficients(constantSeconds, secondsPerInputToken, secondsPerOutputToken float64) error {
	for _, value := range []float64{constantSeconds, secondsPerInputToken, secondsPerOutputToken} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return errors.New("execution-time coefficients must be finite and non-negative")
		}
	}
	return nil
}

// EstimateTaskFee fetches execution-time coefficients, computes shared
// billable_gwei for Credits precheck and task fee, and resolves a queue median
// hint snapshot that does not enter billable_gwei.
func EstimateTaskFee(
	ctx context.Context,
	model string,
	effectiveVram uint64,
	priorityGwei *big.Int,
	estimatedPromptTokens uint64,
	maxCompletionTokens uint64,
) (*CalcTaskFeeResult, error) {
	appCfg := config.GetConfig()
	rate, err := appCfg.ParseCreditsPerGwei()
	if err != nil {
		return nil, fmt.Errorf("parse credits_per_gwei: %w", err)
	}

	coefficients, err := GetLLMExecutionTime(ctx, model, effectiveVram)
	if err != nil {
		return nil, fmt.Errorf("fetch llm execution-time: %w", err)
	}
	if err := validateExecutionTimeCoefficients(
		coefficients.ConstantSeconds,
		coefficients.SecondsPerInputToken,
		coefficients.SecondsPerOutputToken,
	); err != nil {
		return nil, err
	}

	billable, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:          priorityGwei,
		EffectiveVram:         effectiveVram,
		BaseVram:              appCfg.LLM.BaseVRAM,
		ConstantSeconds:       coefficients.ConstantSeconds,
		SecondsPerInputToken:  coefficients.SecondsPerInputToken,
		SecondsPerOutputToken: coefficients.SecondsPerOutputToken,
		PromptTokens:          estimatedPromptTokens,
		CompletionTokens:      maxCompletionTokens,
	})
	if err != nil {
		return nil, err
	}

	credits, err := CalcCreditsFromBillable(billable.BillableGwei, rate)
	if err != nil {
		return nil, err
	}

	median, err := ResolveQueueMedianHint()
	if err != nil {
		return nil, fmt.Errorf("resolve queue median hint: %w", err)
	}

	return &CalcTaskFeeResult{
		TaskFeeGwei:           billable.TaskFeeGwei,
		Credits:               credits,
		MedianPriorityGwei:    median,
		EstimatedNodeSeconds:  billable.EstimatedNodeSeconds,
		VramWeight:            billable.VramWeight,
		ConstantSeconds:       coefficients.ConstantSeconds,
		SecondsPerInputToken:  coefficients.SecondsPerInputToken,
		SecondsPerOutputToken: coefficients.SecondsPerOutputToken,
	}, nil
}
