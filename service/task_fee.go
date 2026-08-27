package service

import (
	"context"
	"crynux_as/config"
	"errors"
	"fmt"
	"math/big"
)

type CalcTaskFeeInput struct {
	MedianPriorityGwei    *big.Int
	TokenRatioStored      uint
	EffectiveVram         uint64
	BaseVram              uint64
	ConstantSeconds       float64
	SecondsPerInputToken  float64
	SecondsPerOutputToken float64
	EstimatedPromptTokens uint64
	MaxCompletionTokens   uint64
}

type CalcTaskFeeResult struct {
	TaskFeeGwei          *big.Int
	MedianPriorityGwei   *big.Int
	EstimatedNodeSeconds float64
	VramWeight           float64
}

// CalcTaskFee computes task_fee_gwei from median priority, token ratio, VRAM
// weight, and text-only execution-time coefficients.
func CalcTaskFee(in CalcTaskFeeInput) (*CalcTaskFeeResult, error) {
	if in.MedianPriorityGwei == nil {
		return nil, errors.New("median_priority_gwei is required")
	}
	if in.MedianPriorityGwei.Sign() < 0 {
		return nil, errors.New("median_priority_gwei must be non-negative")
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

	estimatedNodeSeconds := in.ConstantSeconds +
		in.SecondsPerInputToken*float64(in.EstimatedPromptTokens) +
		in.SecondsPerOutputToken*float64(in.MaxCompletionTokens)

	vramDemand := in.EffectiveVram
	if vramDemand < in.BaseVram {
		vramDemand = in.BaseVram
	}
	vramWeight := float64(vramDemand) / float64(in.BaseVram)

	tokenRatioDisplay := DisplayTokenRatio(in.TokenRatioStored)
	targetPriority := new(big.Float).SetInt(in.MedianPriorityGwei)
	targetPriority.Mul(targetPriority, big.NewFloat(tokenRatioDisplay))

	product := new(big.Float).Mul(targetPriority, big.NewFloat(estimatedNodeSeconds*vramWeight))
	if product.Sign() < 0 {
		return nil, errors.New("task_fee_gwei must be non-negative")
	}
	fee, _ := product.Int(nil)
	if fee == nil {
		fee = big.NewInt(0)
	}

	return &CalcTaskFeeResult{
		TaskFeeGwei:          fee,
		MedianPriorityGwei:   new(big.Int).Set(in.MedianPriorityGwei),
		EstimatedNodeSeconds: estimatedNodeSeconds,
		VramWeight:           vramWeight,
	}, nil
}

// EstimateTaskFee resolves median priority and execution-time coefficients, then
// computes the task fee for an LLM request.
func EstimateTaskFee(
	ctx context.Context,
	model string,
	effectiveVram uint64,
	tokenRatioStored uint,
	estimatedPromptTokens uint64,
	maxCompletionTokens uint64,
) (*CalcTaskFeeResult, error) {
	appCfg := config.GetConfig()
	median, ok := ResolveMedianPriorityGwei()
	if !ok {
		var err error
		median, err = appCfg.ParseEmptyQueueMedianPriorityGwei()
		if err != nil {
			return nil, fmt.Errorf("parse empty queue median priority: %w", err)
		}
	}

	coefficients, err := GetLLMExecutionTime(ctx, model, effectiveVram)
	if err != nil {
		return nil, fmt.Errorf("fetch llm execution-time: %w", err)
	}

	return CalcTaskFee(CalcTaskFeeInput{
		MedianPriorityGwei:    median,
		TokenRatioStored:      tokenRatioStored,
		EffectiveVram:         effectiveVram,
		BaseVram:              appCfg.LLM.BaseVRAM,
		ConstantSeconds:       coefficients.ConstantSeconds,
		SecondsPerInputToken:  coefficients.SecondsPerInputToken,
		SecondsPerOutputToken: coefficients.SecondsPerOutputToken,
		EstimatedPromptTokens: estimatedPromptTokens,
		MaxCompletionTokens:   maxCompletionTokens,
	})
}
