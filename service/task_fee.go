package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/relay"
	"encoding/json"
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

type CalcImageTaskFeeInput struct {
	TaskArgs      string
	Query         relay.ExecutionTimeQuery
	PriorityGwei  *big.Int
	EffectiveVram uint64
}

type imageTaskArgs struct {
	TaskConfig struct {
		NumImages   uint64 `json:"num_images"`
		ImageWidth  uint64 `json:"image_width"`
		ImageHeight uint64 `json:"image_height"`
		Steps       uint64 `json:"steps"`
	} `json:"task_config"`
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
	billableGwei, taskFeeGwei, err = applyMaxTaskFeeCap(billableGwei, taskFeeGwei)
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

func EstimateImageTaskFee(ctx context.Context, in CalcImageTaskFeeInput) (*CalcTaskFeeResult, error) {
	units, err := imagePixelStepUnits(in.TaskArgs)
	if err != nil {
		return nil, err
	}
	coefficients, err := GetSDExecutionTime(ctx, in.Query)
	if err != nil {
		return nil, fmt.Errorf("fetch image execution-time: %w", err)
	}
	if math.IsNaN(coefficients.OverheadSeconds) ||
		math.IsInf(coefficients.OverheadSeconds, 0) ||
		coefficients.OverheadSeconds < 0 ||
		math.IsNaN(coefficients.SecondsPerSDPixelStep) ||
		math.IsInf(coefficients.SecondsPerSDPixelStep, 0) ||
		coefficients.SecondsPerSDPixelStep < 0 {
		return nil, errors.New("image execution-time coefficients must be finite and non-negative")
	}
	seconds := coefficients.OverheadSeconds + float64(units)*coefficients.SecondsPerSDPixelStep
	if seconds < 1 {
		seconds = 1
	}
	appCfg := config.GetConfig()
	billable, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:    in.PriorityGwei,
		EffectiveVram:   in.EffectiveVram,
		BaseVram:        appCfg.LLM.BaseVRAM,
		ConstantSeconds: seconds,
	})
	if err != nil {
		return nil, err
	}
	rate, err := appCfg.ParseCreditsPerGwei()
	if err != nil {
		return nil, err
	}
	credits, err := CalcCreditsFromBillable(billable.BillableGwei, rate)
	if err != nil {
		return nil, err
	}
	median, err := ResolveQueueMedianHint()
	if err != nil {
		return nil, err
	}
	return &CalcTaskFeeResult{
		TaskFeeGwei:          billable.TaskFeeGwei,
		Credits:              credits,
		MedianPriorityGwei:   median,
		EstimatedNodeSeconds: seconds,
		VramWeight:           billable.VramWeight,
	}, nil
}

func imagePixelStepUnits(taskArgs string) (uint64, error) {
	var args imageTaskArgs
	if err := json.Unmarshal([]byte(taskArgs), &args); err != nil {
		return 0, fmt.Errorf("parse image task args: %w", err)
	}
	cfg := args.TaskConfig
	if cfg.NumImages == 0 || cfg.ImageWidth == 0 || cfg.ImageHeight == 0 || cfg.Steps == 0 {
		return 0, errors.New("num_images, image_width, image_height, and steps must be positive")
	}
	units := cfg.NumImages
	for _, factor := range []uint64{cfg.ImageWidth, cfg.ImageHeight, cfg.Steps} {
		if units > math.MaxUint64/factor {
			return 0, errors.New("image pixel-step units overflow uint64")
		}
		units *= factor
	}
	return units, nil
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

	billableGwei, taskFeeGwei, err := billableFromParts(
		priorityGwei,
		estimatedNodeSeconds,
		vramWeight,
	)
	if err != nil {
		return nil, err
	}
	billableGwei, _, err = applyMaxTaskFeeCap(billableGwei, taskFeeGwei)
	if err != nil {
		return nil, err
	}
	return CalcCreditsFromBillable(billableGwei, rate)
}

// applyMaxTaskFeeCap clamps billable_gwei and task_fee_gwei to llm.max_task_price_cnx.
func applyMaxTaskFeeCap(billableGwei *big.Float, taskFeeGwei *big.Int) (*big.Float, *big.Int, error) {
	if billableGwei == nil {
		return nil, nil, errors.New("billable_gwei is required")
	}
	if taskFeeGwei == nil {
		return nil, nil, errors.New("task_fee_gwei is required")
	}
	appCfg := config.GetConfig()
	if appCfg == nil {
		return nil, nil, errors.New("config is not initialized")
	}
	maxFeeGwei, err := appCfg.MaxTaskFeeGwei()
	if err != nil {
		return nil, nil, fmt.Errorf("max_task_price_cnx: %w", err)
	}
	if taskFeeGwei.Cmp(maxFeeGwei) <= 0 {
		return billableGwei, taskFeeGwei, nil
	}
	return new(big.Float).SetInt(maxFeeGwei), new(big.Int).Set(maxFeeGwei), nil
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
	query := relay.ExecutionTimeQuery{Model: model, MinVRAM: &effectiveVram}
	return EstimateLLMTaskFeeWithQuery(
		ctx,
		query,
		effectiveVram,
		priorityGwei,
		estimatedPromptTokens,
		maxCompletionTokens,
	)
}

func EstimateLLMTaskFeeWithQuery(
	ctx context.Context,
	query relay.ExecutionTimeQuery,
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

	coefficients, err := GetLLMExecutionTimeForQuery(ctx, query)
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
