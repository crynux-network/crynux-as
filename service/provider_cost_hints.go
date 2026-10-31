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
	// MinCredits is the Credits required to cover every configured Provider
	// model at MinPriorityGwei for the same reference workload.
	MinCredits *big.Int
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
		return &ProviderCostHints{
			MinPriorityGwei:       minPriority,
			MinCredits:            big.NewInt(1),
			ReferenceInputTokens:  ProviderMinReferenceInputTokens,
			ReferenceOutputTokens: ProviderMinReferenceOutputTokens,
		}, nil
	}

	routes := ListConfiguredProviderModels()
	maxMinCredits := big.NewInt(1)
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
		if credits.Cmp(maxMinCredits) > 0 {
			maxMinCredits = credits
		}
	}

	return &ProviderCostHints{
		MinPriorityGwei:       sharedMinPriority,
		MinCredits:            maxMinCredits,
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
