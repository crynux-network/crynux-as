package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ProviderUsageComponents holds non-negative integer usage components for settlement.
type ProviderUsageComponents struct {
	InputTokens           uint64
	OutputTokens          uint64
	CachedInputTokens     uint64
	CacheWriteInputTokens uint64
	ReasoningTokens       uint64
	ToolUsePromptTokens   uint64
	TotalTokens           uint64
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

// EstimateProviderInputBytes counts UTF-8 bytes of text fields in a Provider request body.
func EstimateProviderInputBytes(requestJSON []byte) uint64 {
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
			for _, item := range obj {
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
	// Detail fields replace portions of parent totals for pricing rather than stacking.
	billableInput := usage.InputTokens
	if usage.CachedInputTokens > 0 || usage.CacheWriteInputTokens > 0 || usage.ToolUsePromptTokens > 0 {
		detail := usage.CachedInputTokens + usage.CacheWriteInputTokens + usage.ToolUsePromptTokens
		if billableInput >= detail {
			billableInput -= detail
		}
	}
	billableOutput := usage.OutputTokens
	if usage.ReasoningTokens > 0 && billableOutput >= usage.ReasoningTokens {
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
	return units, nil
}

func EstimateProviderUnits(weights map[string]string, inputBytes, maxOutputTokens uint64) (*big.Rat, error) {
	inputWeight, err := maxWeight(weights, "input_tokens", "cached_input_tokens", "cache_write_input_tokens", "tool_use_prompt_tokens")
	if err != nil {
		return nil, err
	}
	outputWeight, err := maxWeight(weights, "output_tokens", "reasoning_tokens")
	if err != nil {
		return nil, err
	}
	units := big.NewRat(0, 1)
	units.Add(units, new(big.Rat).Mul(new(big.Rat).SetUint64(inputBytes), inputWeight))
	units.Add(units, new(big.Rat).Mul(new(big.Rat).SetUint64(maxOutputTokens), outputWeight))
	return units, nil
}

func ApplyMaxTaskPriceCap(billableGwei *big.Int, maxTaskPriceCNX string) (*big.Int, error) {
	maxFeeGwei, err := config.MaxTaskFeeGweiFromPrice(maxTaskPriceCNX)
	if err != nil {
		return nil, fmt.Errorf("max_task_price_cnx: %w", err)
	}
	if billableGwei.Cmp(maxFeeGwei) > 0 {
		return new(big.Int).Set(maxFeeGwei), nil
	}
	return new(big.Int).Set(billableGwei), nil
}

func ProviderBillableGwei(effectivePriorityGwei *big.Int, providerUnits *big.Rat, maxTaskPriceCNX string) (*big.Int, error) {
	if effectivePriorityGwei == nil || effectivePriorityGwei.Sign() <= 0 {
		return nil, errors.New("effective priority is required")
	}
	if providerUnits == nil || providerUnits.Sign() < 0 {
		return nil, errors.New("provider units are invalid")
	}
	product := new(big.Rat).Mul(new(big.Rat).SetInt(effectivePriorityGwei), providerUnits)
	uncapped := new(big.Int).Quo(product.Num(), product.Denom())
	return ApplyMaxTaskPriceCap(uncapped, maxTaskPriceCNX)
}

func ProviderConsumerCredits(billableGwei *big.Int, creditsPerGwei string) (*big.Int, error) {
	rate, ok := new(big.Rat).SetString(strings.TrimSpace(creditsPerGwei))
	if !ok || rate.Sign() < 0 {
		return nil, errors.New("credits_per_gwei is invalid")
	}
	product := new(big.Rat).Mul(new(big.Rat).SetInt(billableGwei), rate)
	credits := new(big.Int).Quo(product.Num(), product.Denom())
	if credits.Sign() <= 0 {
		return big.NewInt(1), nil
	}
	return credits, nil
}

func ProviderEarningWei(billableGwei *big.Int) *big.Int {
	return new(big.Int).Mul(billableGwei, big.NewInt(1_000_000_000))
}

func BuildProviderBillingData(
	route ProviderModelRoute,
	effectivePriority *big.Int,
	inputBytes, maxOutputTokens uint64,
	lockedCredits *big.Int,
) (models.ProviderTaskBillingData, *big.Int, error) {
	cfg := config.GetConfig()
	maxTaskPriceCNX, err := cfg.MaxTaskPriceCNXFor(string(route.ServiceProvider))
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	units, err := EstimateProviderUnits(route.UsageWeights, inputBytes, maxOutputTokens)
	if err != nil {
		return models.ProviderTaskBillingData{}, nil, err
	}
	billableGwei, err := ProviderBillableGwei(effectivePriority, units, maxTaskPriceCNX)
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
		ServiceProvider:        string(route.ServiceProvider),
		ModelName:              route.ModelName,
		UsageWeights:           route.UsageWeights,
		EffectivePriorityGwei:  effectivePriority.String(),
		CreditsPerGwei:         cfg.LLM.CreditsPerGwei,
		MaxTaskPriceCNX:        maxTaskPriceCNX,
		EstimatedInputBytes:    inputBytes,
		EstimatedOutputTokens:  maxOutputTokens,
		EstimatedProviderUnits: units.FloatString(18),
		LockedCredits:          lockedCredits.String(),
	}, credits, nil
}
