package service

import (
	"bytes"
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"math/big"
	"strings"

	_ "golang.org/x/image/webp"
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
	PriorityGwei          *big.Int
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
	Project       *models.Project
	TaskArgs      string
	Query         relay.ExecutionTimeQuery
	PriorityGwei  *big.Int
	EffectiveVram uint64
}

// ErrTaskFeeBelowMinimum is returned when the admission task fee is below
// llm.min_task_price_cnx and cannot be raised under the project's Cost Level rules.
var ErrTaskFeeBelowMinimum = errors.New("Task fee is below the minimum for this provider; increase the project Cost Level")

// ErrAutoMaxCreditsInsufficient is returned when Auto Max Credits per task cannot
// cover the provider minimum fee for the request workload.
var ErrAutoMaxCreditsInsufficient = errors.New("Task fee is below the minimum for this provider; increase Max Credits per task")

// ErrMaxCreditsExceeded is returned when Static Max Credits per task is below
// the estimated Credits at the project's fixed Cost Level.
var ErrMaxCreditsExceeded = errors.New("Estimated Credits exceed Max Credits per task; lower the Cost Level or increase Max Credits per task")

// EnforceAdmissionMinTaskFee compares the post-max-cap fee at the resolved
// priority against llm.min_task_price_cnx.<providerID>.
//
// Static mode rejects when estimated Credits exceed auto_max_credits_per_task
// (ErrMaxCreditsExceeded) without changing priority, then rejects below the
// minimum with ErrTaskFeeBelowMinimum.
// Auto mode first lowers priority so estimated Credits stay within
// auto_max_credits_per_task, then raises priority within that Credits budget
// when needed to meet the minimum fee. When the Credits budget cannot meet the
// minimum, it returns ErrAutoMaxCreditsInsufficient.
// feeAtPriority MUST return the post-max-cap billable/task fee for one priority.
func EnforceAdmissionMinTaskFee(
	providerID string,
	project *models.Project,
	priority *big.Int,
	feeAtPriority func(priority *big.Int) (*big.Int, error),
) (*big.Int, *big.Int, error) {
	appCfg := config.GetConfig()
	if appCfg == nil {
		return nil, nil, errors.New("config is not initialized")
	}
	minFee, err := appCfg.MinTaskFeeGwei(providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("min_task_price_cnx.%s: %w", providerID, err)
	}
	return enforceAdmissionMinTaskFee(project, priority, minFee, feeAtPriority)
}

// scaleAdmissionMinTaskFee scales a configured base minimum by request_units / baseline_units,
// rounding down to integer Gwei.
func scaleAdmissionMinTaskFee(baseMinFeeGwei *big.Int, requestUnits, baselineUnits uint64) (*big.Int, error) {
	if baseMinFeeGwei == nil || baseMinFeeGwei.Sign() < 0 {
		return nil, errors.New("minimum task fee must be non-negative")
	}
	if requestUnits == 0 {
		return nil, errors.New("request units must be positive")
	}
	if baselineUnits == 0 {
		return nil, errors.New("baseline units must be positive")
	}
	scaled := new(big.Int).Mul(baseMinFeeGwei, new(big.Int).SetUint64(requestUnits))
	return scaled.Quo(scaled, new(big.Int).SetUint64(baselineUnits)), nil
}

const (
	imageAdmissionBaselineWidth     uint64 = 1024
	imageAdmissionBaselineHeight    uint64 = 1024
	imageAdmissionBaselineSteps     uint64 = 30
	imageAdmissionBaselineNumImages uint64 = 1
)

const (
	defaultVideoWidth     uint64 = 832
	defaultVideoHeight    uint64 = 480
	defaultVideoNumFrames uint64 = 81
	defaultVideoSteps     uint64 = 30
)

func videoAdmissionBaselineUnits() uint64 {
	return defaultVideoWidth * defaultVideoHeight * defaultVideoNumFrames * defaultVideoSteps
}

func imageAdmissionBaselineUnits() uint64 {
	return imageAdmissionBaselineNumImages *
		imageAdmissionBaselineWidth *
		imageAdmissionBaselineHeight *
		imageAdmissionBaselineSteps
}

func enforceAdmissionMinTaskFee(
	project *models.Project,
	priority *big.Int,
	minFee *big.Int,
	feeAtPriority func(priority *big.Int) (*big.Int, error),
) (*big.Int, *big.Int, error) {
	if project == nil {
		return nil, nil, errors.New("project is required")
	}
	if priority == nil || priority.Sign() <= 0 {
		return nil, nil, errors.New("priority_gwei must be positive")
	}
	if minFee == nil || minFee.Sign() < 0 {
		return nil, nil, errors.New("minimum task fee must be non-negative")
	}
	if feeAtPriority == nil {
		return nil, nil, errors.New("fee calculator is required")
	}

	mode := project.CostLevelMode
	if mode == "" {
		mode = models.CostLevelModeStatic
	}
	workingPriority := new(big.Int).Set(priority)
	if mode == models.CostLevelModeAuto {
		capped, err := CapPriorityByAutoMaxCredits(project, workingPriority, feeAtPriority)
		if err != nil {
			return nil, nil, err
		}
		workingPriority = capped
	}

	billable, err := feeAtPriority(workingPriority)
	if err != nil {
		return nil, nil, err
	}
	if mode != models.CostLevelModeAuto {
		if err := rejectIfStaticCreditsExceedMax(project, billable); err != nil {
			return nil, nil, err
		}
		if minFee.Sign() > 0 && billable.Cmp(minFee) < 0 {
			return nil, nil, ErrTaskFeeBelowMinimum
		}
		return workingPriority, billable, nil
	}
	if minFee.Sign() <= 0 || billable.Cmp(minFee) >= 0 {
		return workingPriority, billable, nil
	}
	autoMax, err := MaxPriorityWithinAutoMaxCredits(project, feeAtPriority)
	if err != nil {
		return nil, nil, err
	}
	if autoMax.Cmp(workingPriority) < 0 {
		autoMax = new(big.Int).Set(workingPriority)
	}
	feeAtMax, err := feeAtPriority(autoMax)
	if err != nil {
		return nil, nil, err
	}
	if feeAtMax.Cmp(minFee) < 0 {
		return nil, nil, ErrAutoMaxCreditsInsufficient
	}

	lo := new(big.Int).Set(workingPriority)
	hi := new(big.Int).Set(autoMax)
	for lo.Cmp(hi) < 0 {
		mid := new(big.Int).Rsh(new(big.Int).Add(lo, hi), 1)
		fee, err := feeAtPriority(mid)
		if err != nil {
			return nil, nil, err
		}
		if fee.Cmp(minFee) >= 0 {
			hi = mid
		} else {
			lo = new(big.Int).Add(mid, big.NewInt(1))
		}
	}
	billable, err = feeAtPriority(hi)
	if err != nil {
		return nil, nil, err
	}
	if billable.Cmp(minFee) < 0 {
		return nil, nil, ErrAutoMaxCreditsInsufficient
	}
	return hi, billable, nil
}

// rejectIfStaticCreditsExceedMax rejects when estimated Credits at the fixed
// Static Cost Level exceed auto_max_credits_per_task.
func rejectIfStaticCreditsExceedMax(project *models.Project, billableGwei *big.Int) error {
	if err := requireAutoMaxCreditsPerTask(project); err != nil {
		return err
	}
	if billableGwei == nil || billableGwei.Sign() < 0 {
		return errors.New("billable gwei must be non-negative")
	}
	appCfg := config.GetConfig()
	if appCfg == nil {
		return errors.New("config is not initialized")
	}
	rate, err := appCfg.ParseCreditsPerGwei()
	if err != nil {
		return fmt.Errorf("credits_per_gwei: %w", err)
	}
	credits, err := CalcCreditsFromBillable(new(big.Float).SetInt(billableGwei), rate)
	if err != nil {
		return err
	}
	if credits.Cmp(&project.AutoMaxCreditsPerTask.Int) > 0 {
		return ErrMaxCreditsExceeded
	}
	return nil
}

// CapPriorityByAutoMaxCredits lowers priority so estimated Credits stay within
// the project's auto_max_credits_per_task. Static projects are unchanged.
func CapPriorityByAutoMaxCredits(
	project *models.Project,
	priority *big.Int,
	feeAtPriority func(priority *big.Int) (*big.Int, error),
) (*big.Int, error) {
	if project == nil {
		return nil, errors.New("project is required")
	}
	if priority == nil || priority.Sign() <= 0 {
		return nil, errors.New("priority_gwei must be positive")
	}
	maxWithin, err := MaxPriorityWithinAutoMaxCredits(project, feeAtPriority)
	if err != nil {
		return nil, err
	}
	if priority.Cmp(maxWithin) > 0 {
		return maxWithin, nil
	}
	return new(big.Int).Set(priority), nil
}

// MaxPriorityWithinAutoMaxCredits returns the highest Cost Level whose estimated
// Credits are at most auto_max_credits_per_task.
func MaxPriorityWithinAutoMaxCredits(
	project *models.Project,
	feeAtPriority func(priority *big.Int) (*big.Int, error),
) (*big.Int, error) {
	if err := requireAutoMaxCreditsPerTask(project); err != nil {
		return nil, err
	}
	if feeAtPriority == nil {
		return nil, errors.New("fee calculator is required")
	}
	appCfg := config.GetConfig()
	if appCfg == nil {
		return nil, errors.New("config is not initialized")
	}
	rate, err := appCfg.ParseCreditsPerGwei()
	if err != nil {
		return nil, fmt.Errorf("credits_per_gwei: %w", err)
	}
	minPriority, err := appCfg.ParseMinPriorityGwei()
	if err != nil {
		return nil, fmt.Errorf("llm.min_priority_gwei is invalid: %w", err)
	}
	maxPriority, err := appCfg.ParseMaxPriorityGwei()
	if err != nil {
		return nil, fmt.Errorf("llm.max_priority_gwei is invalid: %w", err)
	}
	maxCredits := &project.AutoMaxCreditsPerTask.Int

	creditsForPriority := func(priority *big.Int) (*big.Int, error) {
		fee, err := feeAtPriority(priority)
		if err != nil {
			return nil, err
		}
		return CalcCreditsFromBillable(new(big.Float).SetInt(fee), rate)
	}

	minCredits, err := creditsForPriority(minPriority)
	if err != nil {
		return nil, err
	}
	if minCredits.Cmp(maxCredits) > 0 {
		return nil, ErrAutoMaxCreditsInsufficient
	}

	lo := new(big.Int).Set(minPriority)
	hi := new(big.Int).Set(maxPriority)
	for lo.Cmp(hi) < 0 {
		mid := new(big.Int).Rsh(new(big.Int).Add(lo, new(big.Int).Add(hi, big.NewInt(1))), 1)
		credits, err := creditsForPriority(mid)
		if err != nil {
			return nil, err
		}
		if credits.Cmp(maxCredits) <= 0 {
			lo = mid
		} else {
			hi = new(big.Int).Sub(mid, big.NewInt(1))
		}
	}
	return lo, nil
}

type imageTaskArgs struct {
	TaskConfig struct {
		NumImagesPerPrompt *uint64 `json:"num_images_per_prompt"`
		ImageWidth         uint64  `json:"image_width"`
		ImageHeight        uint64  `json:"image_height"`
		Steps              uint64  `json:"steps"`
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
	return estimateWorkloadTaskFee(in, seconds, units, imageAdmissionBaselineUnits())
}

// EstimateVideoTaskFee estimates a Video task fee from Relay video
// execution-time coefficients and the TaskArgs video workload units.
func EstimateVideoTaskFee(ctx context.Context, in CalcImageTaskFeeInput) (*CalcTaskFeeResult, error) {
	units, err := videoTaskUnits(in.TaskArgs)
	if err != nil {
		return nil, err
	}
	coefficients, err := GetVideoExecutionTime(ctx, in.Query)
	if err != nil {
		return nil, fmt.Errorf("fetch video execution-time: %w", err)
	}
	if math.IsNaN(coefficients.OverheadSeconds) ||
		math.IsInf(coefficients.OverheadSeconds, 0) ||
		coefficients.OverheadSeconds < 0 ||
		math.IsNaN(coefficients.SecondsPerVideoUnit) ||
		math.IsInf(coefficients.SecondsPerVideoUnit, 0) ||
		coefficients.SecondsPerVideoUnit < 0 {
		return nil, errors.New("video execution-time coefficients must be finite and non-negative")
	}
	seconds := coefficients.OverheadSeconds + float64(units)*coefficients.SecondsPerVideoUnit
	return estimateWorkloadTaskFee(in, seconds, units, videoAdmissionBaselineUnits())
}

// estimateWorkloadTaskFee prices a task whose estimated node seconds are fixed
// at admission. The minimum task fee is scaled by units / baselineUnits.
func estimateWorkloadTaskFee(in CalcImageTaskFeeInput, seconds float64, units, baselineUnits uint64) (*CalcTaskFeeResult, error) {
	if seconds < 1 {
		seconds = 1
	}
	appCfg := config.GetConfig()
	feeAtPriority := func(priority *big.Int) (*big.Int, error) {
		got, err := CalcBillableGwei(CalcBillableInput{
			PriorityGwei:    priority,
			EffectiveVram:   in.EffectiveVram,
			BaseVram:        appCfg.LLM.BaseVRAM,
			ConstantSeconds: seconds,
		})
		if err != nil {
			return nil, err
		}
		return got.TaskFeeGwei, nil
	}
	baseMinFeeGwei, err := appCfg.MinTaskFeeGwei(config.MaxTaskPriceProviderCrynux)
	if err != nil {
		return nil, fmt.Errorf("min_task_price_cnx.%s: %w", config.MaxTaskPriceProviderCrynux, err)
	}
	minFeeGwei, err := scaleAdmissionMinTaskFee(baseMinFeeGwei, units, baselineUnits)
	if err != nil {
		return nil, err
	}
	adjustedPriority, _, err := enforceAdmissionMinTaskFee(
		in.Project,
		in.PriorityGwei,
		minFeeGwei,
		feeAtPriority,
	)
	if err != nil {
		return nil, err
	}
	billable, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:    adjustedPriority,
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
		PriorityGwei:         adjustedPriority,
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
	numImages := uint64(1)
	if cfg.NumImagesPerPrompt != nil {
		numImages = *cfg.NumImagesPerPrompt
	}
	if numImages == 0 || cfg.ImageWidth == 0 || cfg.ImageHeight == 0 || cfg.Steps == 0 {
		return 0, errors.New("num_images_per_prompt, image_width, image_height, and steps must be positive")
	}
	units := numImages
	for _, factor := range []uint64{cfg.ImageWidth, cfg.ImageHeight, cfg.Steps} {
		if units > math.MaxUint64/factor {
			return 0, errors.New("image pixel-step units overflow uint64")
		}
		units *= factor
	}
	return units, nil
}

type videoTaskArgs struct {
	Image     *string `json:"image"`
	LastImage *string `json:"last_image"`
	Keyframes []struct {
		Image string `json:"image"`
	} `json:"keyframes"`
	References []struct {
		Type string `json:"type"`
		Data string `json:"data"`
	} `json:"references"`
	TaskConfig *struct {
		Width     *uint64 `json:"width"`
		Height    *uint64 `json:"height"`
		NumFrames *uint64 `json:"num_frames"`
		Steps     *uint64 `json:"steps"`
	} `json:"task_config"`
}

// firstVideoConditionImage returns the first conditioning image data URL in
// the order image, last_image, keyframes[0].image, first image reference.
func (args *videoTaskArgs) firstVideoConditionImage() string {
	if args.Image != nil && *args.Image != "" {
		return *args.Image
	}
	if args.LastImage != nil && *args.LastImage != "" {
		return *args.LastImage
	}
	if len(args.Keyframes) > 0 && args.Keyframes[0].Image != "" {
		return args.Keyframes[0].Image
	}
	for _, ref := range args.References {
		if ref.Type == "image" && ref.Data != "" {
			return ref.Data
		}
	}
	return ""
}

func decodeImageDataURLDimensions(dataURL string) (uint64, uint64, error) {
	header, payload, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.Contains(header, ";base64") {
		return 0, 0, errors.New("video image input must be a base64 image data URL")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return 0, 0, fmt.Errorf("decode video image base64: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		return 0, 0, fmt.Errorf("read video image dimensions: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, errors.New("video image dimensions must be positive")
	}
	return uint64(cfg.Width), uint64(cfg.Height), nil
}

type videoTaskDimensions struct {
	Width     uint64
	Height    uint64
	NumFrames uint64
	Steps     uint64
}

// resolveVideoTaskDimensions returns the billed video dimensions. An
// unspecified width or height is taken from the first conditioning image,
// else 832x480.
func resolveVideoTaskDimensions(taskArgs string) (videoTaskDimensions, error) {
	var args videoTaskArgs
	if err := json.Unmarshal([]byte(taskArgs), &args); err != nil {
		return videoTaskDimensions{}, fmt.Errorf("parse video task args: %w", err)
	}
	width, height := uint64(0), uint64(0)
	numFrames, steps := defaultVideoNumFrames, defaultVideoSteps
	if cfg := args.TaskConfig; cfg != nil {
		if cfg.Width != nil {
			width = *cfg.Width
		}
		if cfg.Height != nil {
			height = *cfg.Height
		}
		if cfg.NumFrames != nil {
			numFrames = *cfg.NumFrames
		}
		if cfg.Steps != nil {
			steps = *cfg.Steps
		}
	}
	if width == 0 || height == 0 {
		imgWidth, imgHeight := defaultVideoWidth, defaultVideoHeight
		if dataURL := args.firstVideoConditionImage(); dataURL != "" {
			var err error
			if imgWidth, imgHeight, err = decodeImageDataURLDimensions(dataURL); err != nil {
				return videoTaskDimensions{}, err
			}
		}
		if width == 0 {
			width = imgWidth
		}
		if height == 0 {
			height = imgHeight
		}
	}
	if numFrames == 0 || steps == 0 {
		return videoTaskDimensions{}, errors.New("video width, height, num_frames, and steps must be positive")
	}
	return videoTaskDimensions{Width: width, Height: height, NumFrames: numFrames, Steps: steps}, nil
}

// videoTaskUnits returns width * height * num_frames * steps.
func videoTaskUnits(taskArgs string) (uint64, error) {
	dims, err := resolveVideoTaskDimensions(taskArgs)
	if err != nil {
		return 0, err
	}
	units := dims.Width
	for _, factor := range []uint64{dims.Height, dims.NumFrames, dims.Steps} {
		if units > math.MaxUint64/factor {
			return 0, errors.New("video workload units overflow uint64")
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

// applyMaxTaskFeeCap clamps billable_gwei and task_fee_gwei to llm.max_task_price_cnx.crynux.
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
	maxFeeGwei, err := appCfg.MaxTaskFeeGwei(config.MaxTaskPriceProviderCrynux)
	if err != nil {
		return nil, nil, fmt.Errorf("max_task_price_cnx.%s: %w", config.MaxTaskPriceProviderCrynux, err)
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

func EstimateLLMTaskFeeWithQuery(
	ctx context.Context,
	project *models.Project,
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

	coefficients, err := GetLLMExecutionTime(ctx, query)
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

	feeAtPriority := func(priority *big.Int) (*big.Int, error) {
		got, err := CalcBillableGwei(CalcBillableInput{
			PriorityGwei:          priority,
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
		return got.TaskFeeGwei, nil
	}
	baseMinFeeGwei, err := appCfg.MinTaskFeeGwei(config.MaxTaskPriceProviderCrynux)
	if err != nil {
		return nil, fmt.Errorf("min_task_price_cnx.%s: %w", config.MaxTaskPriceProviderCrynux, err)
	}
	minFeeGwei, err := scaleAdmissionMinTaskFee(
		baseMinFeeGwei,
		maxCompletionTokens,
		appCfg.LLM.DefaultMaxTokens,
	)
	if err != nil {
		return nil, err
	}
	adjustedPriority, _, err := enforceAdmissionMinTaskFee(
		project,
		priorityGwei,
		minFeeGwei,
		feeAtPriority,
	)
	if err != nil {
		return nil, err
	}

	billable, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:          adjustedPriority,
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
		PriorityGwei:          adjustedPriority,
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
