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
	ReferencePriorityGwei *big.Int
	TokenRatioStored      uint
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

// CalcBillableGwei computes shared billable_gwei from fixed reference priority,
// token ratio, estimated node seconds, and VRAM weight.
func CalcBillableGwei(in CalcBillableInput) (*CalcBillableResult, error) {
	if in.ReferencePriorityGwei == nil {
		return nil, errors.New("reference_priority_gwei is required")
	}
	if in.ReferencePriorityGwei.Sign() <= 0 {
		return nil, errors.New("reference_priority_gwei must be positive")
	}
	if in.TokenRatioStored == 0 {
		return nil, errors.New("token_ratio is required")
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
		in.ReferencePriorityGwei,
		in.TokenRatioStored,
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
	referencePriorityGwei *big.Int,
	tokenRatioStored uint,
	estimatedNodeSeconds float64,
	vramWeight float64,
) (*big.Float, *big.Int, error) {
	if math.IsNaN(estimatedNodeSeconds) || math.IsInf(estimatedNodeSeconds, 0) || estimatedNodeSeconds < 0 {
		return nil, nil, errors.New("estimated_node_seconds must be finite and non-negative")
	}
	if math.IsNaN(vramWeight) || math.IsInf(vramWeight, 0) || vramWeight <= 0 {
		return nil, nil, errors.New("vram_weight must be a positive finite number")
	}

	tokenRatioDisplay := DisplayTokenRatio(tokenRatioStored)
	product := new(big.Float).SetInt(referencePriorityGwei)
	product.Mul(product, big.NewFloat(tokenRatioDisplay))
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

// CalcCreditsFromBillable returns floor(billable_gwei * credits_per_gwei).
func CalcCreditsFromBillable(billableGwei *big.Float, creditsPerGwei uint64) (*big.Int, error) {
	if billableGwei == nil {
		return nil, errors.New("billable_gwei is required")
	}
	if creditsPerGwei == 0 {
		return nil, errors.New("credits_per_gwei must be positive")
	}
	if billableGwei.Sign() < 0 {
		return nil, errors.New("billable_gwei must be non-negative")
	}
	scaled := new(big.Float).Mul(billableGwei, new(big.Float).SetUint64(creditsPerGwei))
	credits, _ := scaled.Int(nil)
	if credits == nil {
		credits = big.NewInt(0)
	}
	return credits, nil
}

// CalcCredits computes Credits from usage tokens, persisted coefficients, and
// persisted vram_weight using the shared billable_gwei formula.
func CalcCredits(
	promptTokens, completionTokens uint64,
	tokenRatioStored uint,
	vramWeight float64,
	constantSeconds, secondsPerInputToken, secondsPerOutputToken float64,
	referencePriorityGwei *big.Int,
	creditsPerGwei uint64,
) (*big.Int, error) {
	if referencePriorityGwei == nil {
		return nil, errors.New("reference_priority_gwei is required")
	}
	if referencePriorityGwei.Sign() <= 0 {
		return nil, errors.New("reference_priority_gwei must be positive")
	}
	if tokenRatioStored == 0 {
		return nil, errors.New("token_ratio is required")
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
		referencePriorityGwei,
		tokenRatioStored,
		estimatedNodeSeconds,
		vramWeight,
	)
	if err != nil {
		return nil, err
	}
	return CalcCreditsFromBillable(billableGwei, creditsPerGwei)
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
	tokenRatioStored uint,
	estimatedPromptTokens uint64,
	maxCompletionTokens uint64,
) (*CalcTaskFeeResult, error) {
	appCfg := config.GetConfig()
	referencePriority, err := appCfg.ParseReferencePriorityGwei()
	if err != nil {
		return nil, fmt.Errorf("parse reference priority: %w", err)
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
		ReferencePriorityGwei: referencePriority,
		TokenRatioStored:      tokenRatioStored,
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

	credits, err := CalcCreditsFromBillable(billable.BillableGwei, appCfg.LLM.CreditsPerGwei)
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
