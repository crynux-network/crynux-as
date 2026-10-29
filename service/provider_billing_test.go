package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"math/big"
	"testing"
)

func TestEnforceAdmissionMinTaskFeeStaticRejects(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	project := &models.Project{CostLevelMode: models.CostLevelModeStatic}
	_, _, err := EnforceAdmissionMinTaskFee(
		config.MaxTaskPriceProviderCrynux,
		project,
		big.NewInt(1),
		func(priority *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(priority, big.NewInt(100)), nil
		},
	)
	if !errors.Is(err, ErrTaskFeeBelowMinimum) {
		t.Fatalf("err=%v, want ErrTaskFeeBelowMinimum", err)
	}
}

func TestEnforceAdmissionMinTaskFeeAutoRaisesToMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		"anthropic": "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMax := models.BigInt{Int: *big.NewInt(20_000)}
	project := &models.Project{
		CostLevelMode:       models.CostLevelModeAuto,
		AutoMaxPriorityGwei: &autoMax,
	}
	// fee = priority * 100; need priority >= 10000 for min 1e6 gwei
	adjusted, fee, err := EnforceAdmissionMinTaskFee(
		"anthropic",
		project,
		big.NewInt(1),
		func(priority *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(priority, big.NewInt(100)), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if adjusted.Cmp(big.NewInt(10_000)) != 0 {
		t.Fatalf("adjusted priority=%s, want 10000", adjusted.String())
	}
	if fee.Cmp(big.NewInt(1_000_000)) != 0 {
		t.Fatalf("fee=%s, want 1000000", fee.String())
	}
}

func TestEnforceAdmissionMinTaskFeeAutoRejectsWhenMaxTooLow(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		"anthropic": "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMax := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:       models.CostLevelModeAuto,
		AutoMaxPriorityGwei: &autoMax,
	}
	_, _, err := EnforceAdmissionMinTaskFee(
		"anthropic",
		project,
		big.NewInt(1),
		func(priority *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(priority, big.NewInt(100)), nil
		},
	)
	if !errors.Is(err, ErrTaskFeeBelowMinimum) {
		t.Fatalf("err=%v, want ErrTaskFeeBelowMinimum", err)
	}
}

func TestBuildProviderBillingDataRejectsStaticBelowMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{"anthropic": "0.001"}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "claude-haiku-5-5",
		UsageWeights: map[string]string{
			"input_tokens":             "1",
			"output_tokens":            "5",
			"cached_input_tokens":      "0.1",
			"cache_write_input_tokens": "1.25",
		},
	}
	project := &models.Project{CostLevelMode: models.CostLevelModeStatic}
	_, _, err := BuildProviderBillingData(project, route, big.NewInt(1), 100, 10, nil)
	if !errors.Is(err, ErrTaskFeeBelowMinimum) {
		t.Fatalf("err=%v, want ErrTaskFeeBelowMinimum", err)
	}
}

func TestBuildProviderBillingDataAutoRaisesPriority(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{"anthropic": "0.001"}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "claude-haiku-5-5",
		UsageWeights: map[string]string{
			"input_tokens":             "1",
			"output_tokens":            "5",
			"cached_input_tokens":      "0.1",
			"cache_write_input_tokens": "1.25",
		},
	}
	autoMax := models.BigInt{Int: *big.NewInt(1_000_000_000)}
	project := &models.Project{
		CostLevelMode:       models.CostLevelModeAuto,
		AutoMaxPriorityGwei: &autoMax,
	}
	snapshot, credits, err := BuildProviderBillingData(project, route, big.NewInt(1), 100, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	priority, ok := new(big.Int).SetString(snapshot.EffectivePriorityGwei, 10)
	if !ok || priority.Cmp(big.NewInt(1)) <= 0 {
		t.Fatalf("effective priority=%q was not raised", snapshot.EffectivePriorityGwei)
	}
	if credits.Sign() <= 0 {
		t.Fatalf("credits=%s", credits.String())
	}
}

func TestBuildProviderBillingDataAllowsZeroMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{"anthropic": "0"}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "claude-haiku-5-5",
		UsageWeights: map[string]string{
			"input_tokens":             "1",
			"output_tokens":            "5",
			"cached_input_tokens":      "0.1",
			"cache_write_input_tokens": "1.25",
		},
	}
	project := &models.Project{CostLevelMode: models.CostLevelModeStatic}
	snapshot, credits, err := BuildProviderBillingData(project, route, big.NewInt(1), 100, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.MinTaskPriceCNX != "0" {
		t.Fatalf("MinTaskPriceCNX=%q, want 0", snapshot.MinTaskPriceCNX)
	}
	if snapshot.EffectivePriorityGwei != "1" {
		t.Fatalf("EffectivePriorityGwei=%q, want 1", snapshot.EffectivePriorityGwei)
	}
	if credits.Sign() <= 0 {
		t.Fatalf("credits=%s", credits.String())
	}
}
