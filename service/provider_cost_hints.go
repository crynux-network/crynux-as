package service

import (
	"crynux_as/config"
	"fmt"
	"math/big"
)

// Fixed UI reference workload for Provider minimum Cost Level and Credits hints.
const (
	ProviderMinReferenceInputTokens  uint64 = 10
	ProviderMinReferenceOutputTokens uint64 = 50
)

// ProviderCostHints are derived UI values for project Cost Level controls.
type ProviderCostHints struct {
	// MinPriorityGwei is the shared Cost Level lower bound for the fixed
	// reference workload of ProviderMinReferenceInputTokens input tokens and
	// ProviderMinReferenceOutputTokens output tokens.
	MinPriorityGwei *big.Int
	// MinCreditsCheapest is the Credits required for the cheapest configured
	// Provider model at MinPriorityGwei for the same reference workload.
	MinCreditsCheapest *big.Int
	// MinCreditsExpensive is the Credits required for the most expensive
	// configured Provider model at MinPriorityGwei for the same reference
	// workload.
	MinCreditsExpensive *big.Int
	// ReferenceInputTokens is the input size used to derive the hints.
	ReferenceInputTokens uint64
	// ReferenceOutputTokens is the maximum output size used to derive the hints.
	ReferenceOutputTokens uint64
}

// ResolveProviderCostHints derives Stable WebUI Provider minimum hints from the
// shared providers floor and the fixed 10-input / 50-output reference request.
func ResolveProviderCostHints() (*ProviderCostHints, error) {
	appCfg := config.GetConfig()
	if appCfg == nil {
		return nil, fmt.Errorf("config is not initialized")
	}
	rate, err := appCfg.ParseCreditsPerGwei()
	if err != nil {
		return nil, fmt.Errorf("credits_per_gwei: %w", err)
	}

	sharedMinPriority, err := ResolveProviderSharedMinPriority(
		ProviderMinReferenceInputTokens,
		ProviderMinReferenceOutputTokens,
	)
	if err != nil {
		return nil, err
	}
	if sharedMinPriority == nil {
		minPriority, err := appCfg.ParseMinPriorityGwei()
		if err != nil {
			return nil, err
		}
		one := big.NewInt(1)
		return &ProviderCostHints{
			MinPriorityGwei:       minPriority,
			MinCreditsCheapest:    new(big.Int).Set(one),
			MinCreditsExpensive:   new(big.Int).Set(one),
			ReferenceInputTokens:  ProviderMinReferenceInputTokens,
			ReferenceOutputTokens: ProviderMinReferenceOutputTokens,
		}, nil
	}

	routes := ListConfiguredProviderModels()
	var minCredits *big.Int
	var maxCredits *big.Int
	for _, route := range routes {
		units, err := EstimateProviderUnits(
			route.UsageWeights,
			ProviderMinReferenceInputTokens,
			ProviderMinReferenceOutputTokens,
		)
		if err != nil {
			return nil, fmt.Errorf("estimate units for %s: %w", route.ModelName, err)
		}
		if units.Sign() <= 0 {
			continue
		}
		maxTaskPriceCNX, err := appCfg.MaxTaskPriceCNXFor(string(route.ServiceProvider))
		if err != nil {
			return nil, err
		}
		billable, err := ProviderBillableGwei(sharedMinPriority, units, maxTaskPriceCNX)
		if err != nil {
			return nil, err
		}
		credits, err := CalcCreditsFromBillable(new(big.Float).SetInt(billable), rate)
		if err != nil {
			return nil, err
		}
		if credits.Sign() <= 0 {
			credits = big.NewInt(1)
		}
		if minCredits == nil || credits.Cmp(minCredits) < 0 {
			minCredits = new(big.Int).Set(credits)
		}
		if maxCredits == nil || credits.Cmp(maxCredits) > 0 {
			maxCredits = new(big.Int).Set(credits)
		}
	}
	if minCredits == nil {
		minCredits = big.NewInt(1)
	}
	if maxCredits == nil {
		maxCredits = big.NewInt(1)
	}

	return &ProviderCostHints{
		MinPriorityGwei:       sharedMinPriority,
		MinCreditsCheapest:    minCredits,
		MinCreditsExpensive:   maxCredits,
		ReferenceInputTokens:  ProviderMinReferenceInputTokens,
		ReferenceOutputTokens: ProviderMinReferenceOutputTokens,
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
