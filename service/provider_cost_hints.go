package service

import (
	"crynux_as/config"
	"fmt"
	"math/big"
)

// ProviderCostHints are derived UI values for project Cost Level controls.
type ProviderCostHints struct {
	// MinPriorityGwei is the conservative Cost Level at which every configured
	// Provider model meets its minimum task fee for an empty input and
	// llm.default_max_tokens maximum output.
	MinPriorityGwei *big.Int
	// MinCredits is the conservative Credits amount covering every configured
	// Provider minimum task fee at llm.default_max_tokens.
	MinCredits *big.Int
}

// ResolveProviderCostHints aggregates admission thresholds across configured Provider models.
func ResolveProviderCostHints() (*ProviderCostHints, error) {
	appCfg := config.GetConfig()
	if appCfg == nil {
		return nil, fmt.Errorf("config is not initialized")
	}
	rate, err := appCfg.ParseCreditsPerGwei()
	if err != nil {
		return nil, fmt.Errorf("credits_per_gwei: %w", err)
	}
	defaultMaxTokens := appCfg.LLM.DefaultMaxTokens
	if defaultMaxTokens == 0 {
		return nil, fmt.Errorf("llm.default_max_tokens is not set")
	}

	routes := ListConfiguredProviderModels()
	var maxMinPriority *big.Int
	var maxMinCredits *big.Int
	for _, route := range routes {
		baseMinFee, err := appCfg.MinTaskFeeGwei(string(route.ServiceProvider))
		if err != nil {
			return nil, fmt.Errorf("min_task_price_cnx.%s: %w", route.ServiceProvider, err)
		}
		if baseMinFee.Sign() <= 0 {
			continue
		}
		minCredits, err := CalcCreditsFromBillable(new(big.Float).SetInt(baseMinFee), rate)
		if err != nil {
			return nil, err
		}
		if maxMinCredits == nil || minCredits.Cmp(maxMinCredits) > 0 {
			maxMinCredits = minCredits
		}

		units, err := EstimateProviderUnits(route.UsageWeights, 0, defaultMaxTokens)
		if err != nil {
			return nil, fmt.Errorf("estimate units for %s: %w", route.ModelName, err)
		}
		if units.Sign() <= 0 {
			continue
		}
		// priority = ceil(min_fee / units)
		required := new(big.Rat).SetInt(baseMinFee)
		required.Quo(required, units)
		priority := new(big.Int).Quo(required.Num(), required.Denom())
		if new(big.Rat).SetInt(priority).Cmp(required) < 0 {
			priority.Add(priority, big.NewInt(1))
		}
		if priority.Sign() <= 0 {
			priority = big.NewInt(1)
		}
		clamped, err := ClampPriorityGwei(priority)
		if err != nil {
			return nil, err
		}
		if maxMinPriority == nil || clamped.Cmp(maxMinPriority) > 0 {
			maxMinPriority = clamped
		}
	}

	if maxMinPriority == nil {
		minPriority, err := appCfg.ParseMinPriorityGwei()
		if err != nil {
			return nil, err
		}
		maxMinPriority = minPriority
	}
	if maxMinCredits == nil {
		maxMinCredits = big.NewInt(1)
	}
	return &ProviderCostHints{
		MinPriorityGwei: maxMinPriority,
		MinCredits:      maxMinCredits,
	}, nil
}

// ProviderQueueBoundsForUI returns the current non-expired Provider queue bounds.
func ProviderQueueBoundsForUI() (lowest, highest *big.Int, hasJobs bool) {
	snapshot := GetProviderQueuePrioritySnapshot()
	if !snapshot.HasJobs || snapshot.Lowest == nil || snapshot.Highest == nil {
		return nil, nil, false
	}
	if snapshot.Highest.Cmp(snapshot.Lowest) < 0 {
		return nil, nil, false
	}
	return new(big.Int).Set(snapshot.Lowest), new(big.Int).Set(snapshot.Highest), true
}
