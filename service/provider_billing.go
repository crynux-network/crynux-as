package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ProviderUsageComponents holds non-negative integer usage components for settlement.
type ProviderUsageComponents struct {
	InputTokens               uint64
	OutputTokens              uint64
	CachedInputTokens         uint64
	CacheWriteInputTokens     uint64
	ReasoningTokens           uint64
	ToolUsePromptTokens       uint64
	ImageInputTokens          uint64
	ImageOutputTokens         uint64
	TotalTokens               uint64
	ReasoningIncludedInOutput bool
}

func parseRationalWeight(value string) (*big.Rat, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("usage weight is empty")
	}
	rat := new(big.Rat)
	if _, ok := rat.SetString(trimmed); !ok {
		return nil, errors.New("usage weight is invalid")
	}
	if rat.Sign() < 0 {
		return nil, errors.New("usage weight must be non-negative")
	}
	return rat, nil
}

func maxWeight(weights map[string]string, keys ...string) (*big.Rat, error) {
	max := big.NewRat(0, 1)
	found := false
	for _, key := range keys {
		raw, ok := weights[key]
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		w, err := parseRationalWeight(raw)
		if err != nil {
			return nil, err
		}
		if !found || w.Cmp(max) > 0 {
			max = w
			found = true
		}
	}
	if !found {
		return big.NewRat(0, 1), nil
	}
	return max, nil
}

// ProviderInputTokenEstimate is the admission upper bound of Provider input tokens.
type ProviderInputTokenEstimate struct {
	TextTokens  uint64
	ImageTokens uint64
}

// Total returns the combined text and image input-token estimate.
func (e ProviderInputTokenEstimate) Total() uint64 {
	return e.TextTokens + e.ImageTokens
}

// EstimateProviderTextInputTokens returns the UTF-8 byte count of text fields in
// a Provider request body as the text input-token upper bound. The image_url
// value of image_url and input_image content parts is excluded.
func EstimateProviderTextInputTokens(requestJSON []byte) uint64 {
	if len(requestJSON) == 0 {
		return 0
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(requestJSON, &generic); err != nil {
		return uint64(len(requestJSON))
	}
	var total int
	var walk func(v json.RawMessage)
	walk = func(v json.RawMessage) {
		if len(v) == 0 || string(v) == "null" {
			return
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			total += len(s)
			return
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(v, &arr); err == nil {
			for _, item := range arr {
				walk(item)
			}
			return
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(v, &obj); err == nil {
			skipImageURL := isImageContentPart(obj)
			for key, item := range obj {
				if skipImageURL && key == "image_url" {
					continue
				}
				walk(item)
			}
		}
	}
	walk(requestJSON)
	if total == 0 {
		return uint64(len(requestJSON))
	}
	return uint64(total)
}

func isImageContentPart(obj map[string]json.RawMessage) bool {
	var typ string
	if err := json.Unmarshal(obj["type"], &typ); err != nil {
		return false
	}
	return typ == "image_url" || typ == "input_image"
}

// EstimateProviderInputTokens validates Provider image inputs and returns the
// admission input-token upper bound for one request. Every returned error is
// caused by the request content: invalid JSON or images, images above
// maxImageBytes in total, or images sent to a model without a known image token
// rule.
func EstimateProviderInputTokens(route ProviderModelRoute, apiType models.TaskAPIType, requestJSON []byte, maxImageBytes uint64) (ProviderInputTokenEstimate, error) {
	switch apiType {
	case models.TaskAPITypeImagesGenerations, models.TaskAPITypeImagesEdits:
		return estimateProviderImagesInputTokens(route, apiType, requestJSON, maxImageBytes)
	}
	canonical, err := provider.ParseCanonicalRequest(requestJSON, route.ModelName, apiType, 0)
	if err != nil {
		return ProviderInputTokenEstimate{}, err
	}
	images, err := provider.ValidateCanonicalImages(canonical, maxImageBytes)
	if err != nil {
		return ProviderInputTokenEstimate{}, err
	}
	imageTokens, err := provider.EstimateImageInputTokens(route.ServiceProvider, route.ModelName, images)
	if err != nil {
		return ProviderInputTokenEstimate{}, err
	}
	return ProviderInputTokenEstimate{
		TextTokens:  EstimateProviderTextInputTokens(requestJSON),
		ImageTokens: imageTokens,
	}, nil
}

func estimateProviderImagesInputTokens(route ProviderModelRoute, apiType models.TaskAPIType, requestJSON []byte, maxImageBytes uint64) (ProviderInputTokenEstimate, error) {
	req, err := provider.ParseCanonicalImagesJSON(requestJSON, apiType)
	if err != nil {
		return ProviderInputTokenEstimate{}, err
	}
	req.Model = route.ModelName
	var totalBytes uint64
	infos := make([]provider.CanonicalImageInfo, 0, len(req.Images))
	for _, img := range req.Images {
		decoded, err := base64.StdEncoding.DecodeString(img.Base64)
		if err != nil {
			return ProviderInputTokenEstimate{}, fmt.Errorf("image base64 is invalid")
		}
		totalBytes += uint64(len(decoded))
		if maxImageBytes > 0 && totalBytes > maxImageBytes {
			return ProviderInputTokenEstimate{}, fmt.Errorf("total image size exceeds the %d-byte limit", maxImageBytes)
		}
		infos = append(infos, provider.CanonicalImageInfo{
			MIMEType:     img.MIMEType,
			Width:        img.Width,
			Height:       img.Height,
			DecodedBytes: len(decoded),
		})
	}
	imageTokens, err := provider.EstimateProviderImagesEditInputTokens(route.ServiceProvider, route.ModelName, infos, req.InputFidelity)
	if err != nil {
		return ProviderInputTokenEstimate{}, err
	}
	return ProviderInputTokenEstimate{
		TextTokens:  uint64(len(req.Prompt)),
		ImageTokens: imageTokens,
	}, nil
}

func CalculateProviderUnits(usage ProviderUsageComponents, weights map[string]string) (*big.Rat, error) {
	units := big.NewRat(0, 1)
	add := func(component uint64, key string) error {
		if component == 0 {
			return nil
		}
		raw, ok := weights[key]
		if !ok || strings.TrimSpace(raw) == "" {
			return errors.New("missing usage weight for " + key)
		}
		w, err := parseRationalWeight(raw)
		if err != nil {
			return err
		}
		part := new(big.Rat).SetUint64(component)
		part.Mul(part, w)
		units.Add(units, part)
		return nil
	}
	// Detail fields replace portions of parent totals for pricing rather than stacking
	// when the adapter reports them as subsets. Google thoughts are billed in addition
	// to candidates output and must not be subtracted from OutputTokens.
	billableInput := usage.InputTokens
	if usage.CachedInputTokens > 0 || usage.CacheWriteInputTokens > 0 || usage.ToolUsePromptTokens > 0 {
		detail := usage.CachedInputTokens + usage.CacheWriteInputTokens + usage.ToolUsePromptTokens
		if billableInput >= detail {
			billableInput -= detail
		}
	}
	billableOutput := usage.OutputTokens
	if usage.ReasoningIncludedInOutput && usage.ReasoningTokens > 0 && billableOutput >= usage.ReasoningTokens {
		billableOutput -= usage.ReasoningTokens
	}
	if err := add(billableInput, "input_tokens"); err != nil {
		return nil, err
	}
	if err := add(billableOutput, "output_tokens"); err != nil {
		return nil, err
	}
	if err := add(usage.CachedInputTokens, "cached_input_tokens"); err != nil {
		return nil, err
	}
	if err := add(usage.CacheWriteInputTokens, "cache_write_input_tokens"); err != nil {
		return nil, err
	}
	if err := add(usage.ReasoningTokens, "reasoning_tokens"); err != nil {
		return nil, err
	}
	if err := add(usage.ToolUsePromptTokens, "tool_use_prompt_tokens"); err != nil {
		return nil, err
	}
	if err := add(usage.ImageInputTokens, "image_input_tokens"); err != nil {
		return nil, err
	}
	if err := add(usage.ImageOutputTokens, "image_output_tokens"); err != nil {
		return nil, err
	}
	return units, nil
}

func EstimateProviderUnits(weights map[string]string, inputTokens, maxOutputTokens uint64) (*big.Rat, error) {
	inputWeight, err := maxWeight(weights, "input_tokens", "cached_input_tokens", "cache_write_input_tokens", "tool_use_prompt_tokens", "image_input_tokens")
	if err != nil {
		return nil, err
	}
	outputWeight, err := maxWeight(weights, "output_tokens", "reasoning_tokens", "image_output_tokens")
	if err != nil {
		return nil, err
	}
	units := big.NewRat(0, 1)
	units.Add(units, new(big.Rat).Mul(new(big.Rat).SetUint64(inputTokens), inputWeight))
	units.Add(units, new(big.Rat).Mul(new(big.Rat).SetUint64(maxOutputTokens), outputWeight))
	return units, nil
}

func ApplyMaxTaskPriceCap(billableGwei *big.Rat, maxTaskPriceCNX string) (*big.Rat, error) {
	if billableGwei == nil || billableGwei.Sign() < 0 {
		return nil, errors.New("billable gwei must be non-negative")
	}
	maxFeeGwei, err := config.MaxTaskFeeGweiFromPrice(maxTaskPriceCNX)
	if err != nil {
		return nil, fmt.Errorf("max_task_price_cnx: %w", err)
	}
	maxFee := new(big.Rat).SetInt(maxFeeGwei)
	if billableGwei.Cmp(maxFee) > 0 {
		return maxFee, nil
	}
	return new(big.Rat).Set(billableGwei), nil
}

// ProviderBillableGwei returns post-cap billable Gwei as an exact rational.
// Credits and Key Owner wei MUST floor only at their final conversions.
func ProviderBillableGwei(effectivePriorityGwei *big.Int, providerUnits *big.Rat, maxTaskPriceCNX string) (*big.Rat, error) {
	if effectivePriorityGwei == nil || effectivePriorityGwei.Sign() <= 0 {
		return nil, errors.New("effective priority is required")
	}
	if providerUnits == nil || providerUnits.Sign() < 0 {
		return nil, errors.New("provider units are invalid")
	}
	product := new(big.Rat).Mul(new(big.Rat).SetInt(effectivePriorityGwei), providerUnits)
	return ApplyMaxTaskPriceCap(product, maxTaskPriceCNX)
}

func ProviderConsumerCredits(billableGwei *big.Rat, creditsPerGwei string) (*big.Int, error) {
	if billableGwei == nil || billableGwei.Sign() < 0 {
		return nil, errors.New("billable gwei must be non-negative")
	}
	rate, ok := new(big.Rat).SetString(strings.TrimSpace(creditsPerGwei))
	if !ok || rate.Sign() < 0 {
		return nil, errors.New("credits_per_gwei is invalid")
	}
	product := new(big.Rat).Mul(billableGwei, rate)
	credits := new(big.Int).Quo(product.Num(), product.Denom())
	if credits.Sign() <= 0 {
		return big.NewInt(1), nil
	}
	return credits, nil
}

func ProviderEarningWei(billableGwei *big.Rat) *big.Int {
	if billableGwei == nil || billableGwei.Sign() <= 0 {
		return big.NewInt(0)
	}
	product := new(big.Rat).Mul(billableGwei, big.NewRat(1_000_000_000, 1))
	return new(big.Int).Quo(product.Num(), product.Denom())
}

func floorProviderBillableGwei(billableGwei *big.Rat) *big.Int {
	if billableGwei == nil || billableGwei.Sign() <= 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Quo(new(big.Int).Set(billableGwei.Num()), billableGwei.Denom())
}

func rejectIfStaticProviderCreditsExceedMax(project *models.Project, billableGwei *big.Rat) error {
	if err := requireAutoMaxCreditsPerTask(project); err != nil {
		return err
	}
	appCfg := config.GetConfig()
	if appCfg == nil {
		return errors.New("config is not initialized")
	}
	credits, err := ProviderConsumerCredits(billableGwei, appCfg.LLM.CreditsPerGwei)
	if err != nil {
		return err
	}
	if credits.Cmp(&project.AutoMaxCreditsPerTask.Int) > 0 {
		return ErrMaxCreditsExceeded
	}
	return nil
}

// ceilPriorityFromFeeAndUnits returns ceil(minFeeGwei / units).
func ceilPriorityFromFeeAndUnits(minFeeGwei *big.Int, units *big.Rat) (*big.Int, error) {
	if minFeeGwei == nil || minFeeGwei.Sign() < 0 {
		return nil, errors.New("minimum task fee must be non-negative")
	}
	if units == nil || units.Sign() <= 0 {
		return nil, errors.New("provider units must be positive")
	}
	if minFeeGwei.Sign() == 0 {
		return big.NewInt(0), nil
	}
	required := new(big.Rat).SetInt(minFeeGwei)
	required.Quo(required, units)
	priority := new(big.Int).Quo(required.Num(), required.Denom())
	if new(big.Rat).SetInt(priority).Cmp(required) < 0 {
		priority.Add(priority, big.NewInt(1))
	}
	if priority.Sign() <= 0 {
		priority = big.NewInt(1)
	}
	return priority, nil
}

// CheapestProviderUnits returns the smallest estimated Provider units across
// configured Provider models with the given api_kind for the same estimates.
func CheapestProviderUnits(inputTokens, maxOutputTokens uint64, apiKind string) (*big.Rat, error) {
	routes := ListConfiguredProviderModels()
	var cheapest *big.Rat
	for _, route := range routes {
		if apiKind != "" && route.APIKind != "" && route.APIKind != apiKind {
			continue
		}
		units, err := EstimateProviderUnits(route.UsageWeights, inputTokens, maxOutputTokens)
		if err != nil {
			return nil, fmt.Errorf("estimate units for %s: %w", route.ModelName, err)
		}
		if units.Sign() <= 0 {
			continue
		}
		if cheapest == nil || units.Cmp(cheapest) < 0 {
			cheapest = units
		}
	}
	if cheapest == nil {
		return nil, nil
	}
	return new(big.Rat).Set(cheapest), nil
}

// ResolveProviderSharedMinPriority converts llm.min_task_price_cnx.providers into
// the shared Cost Level lower bound for one request workload. When the configured
// providers minimum is 0 or no Provider models are configured, it returns nil.
func ResolveProviderSharedMinPriority(inputTokens, maxOutputTokens uint64, apiKind string) (*big.Int, error) {
	cfg := config.GetConfig()
	if cfg == nil {
		return nil, errors.New("config is not initialized")
	}
	minFeeGwei, err := cfg.MinTaskFeeGwei(config.MinTaskPriceProviders)
	if err != nil {
		return nil, fmt.Errorf("min_task_price_cnx.%s: %w", config.MinTaskPriceProviders, err)
	}
	if minFeeGwei.Sign() <= 0 {
		return nil, nil
	}
	cheapestUnits, err := CheapestProviderUnits(inputTokens, maxOutputTokens, apiKind)
	if err != nil {
		return nil, err
	}
	if cheapestUnits == nil {
		return nil, nil
	}
	priority, err := ceilPriorityFromFeeAndUnits(minFeeGwei, cheapestUnits)
	if err != nil {
		return nil, err
	}
	return ClampPriorityGwei(priority)
}

// enforceAdmissionSharedMinPriority applies the shared Provider Cost Level lower
// bound while remaining within Max Credits for the selected model.
func enforceAdmissionSharedMinPriority(
	project *models.Project,
	priority *big.Int,
	minPriority *big.Int,
	feeAtPriority func(priority *big.Int) (*big.Rat, error),
) (*big.Int, *big.Rat, error) {
	if project == nil {
		return nil, nil, errors.New("project is required")
	}
	if priority == nil || priority.Sign() <= 0 {
		return nil, nil, errors.New("priority_gwei must be positive")
	}
	if feeAtPriority == nil {
		return nil, nil, errors.New("fee calculator is required")
	}
	mode := project.CostLevelMode
	if mode == "" {
		mode = models.CostLevelModeStatic
	}
	workingPriority := new(big.Int).Set(priority)
	feeAtPriorityInt := func(p *big.Int) (*big.Int, error) {
		billable, err := feeAtPriority(p)
		if err != nil {
			return nil, err
		}
		return floorProviderBillableGwei(billable), nil
	}
	if mode != models.CostLevelModeAuto {
		billable, err := feeAtPriority(workingPriority)
		if err != nil {
			return nil, nil, err
		}
		if err := rejectIfStaticProviderCreditsExceedMax(project, billable); err != nil {
			return nil, nil, err
		}
		if minPriority != nil && minPriority.Sign() > 0 && workingPriority.Cmp(minPriority) < 0 {
			return nil, nil, ErrTaskFeeBelowMinimum
		}
		return workingPriority, billable, nil
	}

	upper, err := MaxPriorityWithinAutoMaxCredits(project, feeAtPriorityInt)
	if err != nil {
		return nil, nil, err
	}
	lower := big.NewInt(0)
	if minPriority != nil && minPriority.Sign() > 0 {
		lower = new(big.Int).Set(minPriority)
	} else {
		appCfg := config.GetConfig()
		if appCfg == nil {
			return nil, nil, errors.New("config is not initialized")
		}
		lower, err = appCfg.ParseMinPriorityGwei()
		if err != nil {
			return nil, nil, fmt.Errorf("llm.min_priority_gwei is invalid: %w", err)
		}
	}
	if lower.Cmp(upper) > 0 {
		return nil, nil, ErrAutoMaxCreditsInsufficient
	}
	if workingPriority.Cmp(lower) < 0 {
		workingPriority = new(big.Int).Set(lower)
	}
	if workingPriority.Cmp(upper) > 0 {
		workingPriority = new(big.Int).Set(upper)
	}
	billable, err := feeAtPriority(workingPriority)
	if err != nil {
		return nil, nil, err
	}
	return workingPriority, billable, nil
}

func BuildProviderBillingData(
	project *models.Project,
	route ProviderModelRoute,
	effectivePriority *big.Int,
	input ProviderInputTokenEstimate,
	maxOutputTokens uint64,
	lockedCredits *big.Int,
) (models.ProviderTaskBillingData, *big.Int, error) {
	cfg := config.GetConfig()
	maxTaskPriceCNX, err := cfg.MaxTaskPriceCNXFor(string(route.ServiceProvider))
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	inputTokens := input.Total()
	units, err := EstimateProviderUnits(route.UsageWeights, inputTokens, maxOutputTokens)
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	feeAtPriority := func(priority *big.Int) (*big.Rat, error) {
		return ProviderBillableGwei(priority, units, maxTaskPriceCNX)
	}
	apiKind := route.APIKind
	if apiKind == "" {
		apiKind = config.ProviderAPIKindChat
	}
	sharedMinPriority, err := ResolveProviderSharedMinPriority(inputTokens, maxOutputTokens, apiKind)
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	adjustedPriority, billableGwei, err := enforceAdmissionSharedMinPriority(
		project,
		effectivePriority,
		sharedMinPriority,
		feeAtPriority,
	)
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	credits, err := ProviderConsumerCredits(billableGwei, cfg.LLM.CreditsPerGwei)
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	if lockedCredits == nil {
		lockedCredits = credits
	}
	return models.ProviderTaskBillingData{
		ServiceProvider:           string(route.ServiceProvider),
		ModelName:                 route.ModelName,
		UsageWeights:              route.UsageWeights,
		EffectivePriorityGwei:     adjustedPriority.String(),
		CreditsPerGwei:            cfg.LLM.CreditsPerGwei,
		MaxTaskPriceCNX:           maxTaskPriceCNX,
		EstimatedInputTokens:      inputTokens,
		EstimatedTextInputTokens:  input.TextTokens,
		EstimatedImageInputTokens: input.ImageTokens,
		EstimatedOutputTokens:     maxOutputTokens,
		EstimatedProviderUnits:    units.FloatString(18),
		LockedCredits:             lockedCredits.String(),
	}, credits, nil
}
