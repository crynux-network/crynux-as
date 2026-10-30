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
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		"anthropic": "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(2_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
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

func TestCapPriorityByAutoMaxCreditsLowersPriority(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(500)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	// fee = priority * 100 => Credits = priority * 100 with G=1.
	capped, err := CapPriorityByAutoMaxCredits(
		project,
		big.NewInt(20),
		func(priority *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(priority, big.NewInt(100)), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if capped.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("capped=%s, want 5", capped.String())
	}
}

func TestMaxPriorityWithinAutoMaxCreditsRejectsWhenMinExceedsBudget(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(50)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	_, err := MaxPriorityWithinAutoMaxCredits(
		project,
		func(priority *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(priority, big.NewInt(100)), nil
		},
	)
	if !errors.Is(err, ErrAutoMaxCreditsInsufficient) {
		t.Fatalf("err=%v, want ErrAutoMaxCreditsInsufficient", err)
	}
}

func TestProviderQueueBoundsForUIEmpty(t *testing.T) {
	providerQueuePriorityMu.Lock()
	providerQueuePrioritySnapshot = ProviderQueuePrioritySnapshot{}
	providerQueuePriorityMu.Unlock()

	lowest, highest, hasJobs := ProviderQueueBoundsForUI()
	if hasJobs || lowest != nil || highest != nil {
		t.Fatalf("want empty queue, got hasJobs=%v lowest=%v highest=%v", hasJobs, lowest, highest)
	}
}

func TestProviderQueueBoundsForUINonEmpty(t *testing.T) {
	providerQueuePriorityMu.Lock()
	providerQueuePrioritySnapshot = ProviderQueuePrioritySnapshot{
		HasJobs: true,
		Lowest:  big.NewInt(10),
		Highest: big.NewInt(40),
	}
	providerQueuePriorityMu.Unlock()
	t.Cleanup(func() {
		providerQueuePriorityMu.Lock()
		providerQueuePrioritySnapshot = ProviderQueuePrioritySnapshot{}
		providerQueuePriorityMu.Unlock()
	})

	lowest, highest, hasJobs := ProviderQueueBoundsForUI()
	if !hasJobs {
		t.Fatal("want hasJobs")
	}
	if lowest.Cmp(big.NewInt(10)) != 0 || highest.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("got %s-%s, want 10-40", lowest.String(), highest.String())
	}
}

func TestResolveProviderCostHintsEmptyProvidersUsesMinPriority(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "3"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.DefaultMaxTokens = 2048
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	hints, err := ResolveProviderCostHints()
	if err != nil {
		t.Fatal(err)
	}
	if hints.MinPriorityGwei.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("MinPriorityGwei=%s, want 3", hints.MinPriorityGwei.String())
	}
	if hints.MinCredits.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("MinCredits=%s, want 1", hints.MinCredits.String())
	}
}

func TestEnforceAdmissionMinTaskFeeAutoRejectsWhenMaxCreditsTooLow(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		"anthropic": "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(50)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	_, _, err := EnforceAdmissionMinTaskFee(
		"anthropic",
		project,
		big.NewInt(1),
		func(priority *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(priority, big.NewInt(100)), nil
		},
	)
	if !errors.Is(err, ErrAutoMaxCreditsInsufficient) {
		t.Fatalf("err=%v, want ErrAutoMaxCreditsInsufficient", err)
	}
}

func TestBuildProviderBillingDataRejectsStaticBelowMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.DefaultMaxTokens = 10
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
	cfg.LLM.DefaultMaxTokens = 10
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
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
	autoMaxCredits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
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

func TestBuildProviderBillingDataScalesMinimumWithMaxOutputTokens(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.DefaultMaxTokens = 10
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
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
	autoMaxCredits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}

	defaultSnapshot, _, err := BuildProviderBillingData(project, route, big.NewInt(1), 0, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	scaledSnapshot, _, err := BuildProviderBillingData(project, route, big.NewInt(1), 0, 40, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scaledSnapshot.EffectivePriorityGwei != defaultSnapshot.EffectivePriorityGwei {
		t.Fatalf(
			"scaled effective priority=%s, default effective priority=%s",
			scaledSnapshot.EffectivePriorityGwei,
			defaultSnapshot.EffectivePriorityGwei,
		)
	}
}

func TestScaleAdmissionMinTaskFeeRoundsDownToGwei(t *testing.T) {
	got, err := scaleAdmissionMinTaskFee(big.NewInt(10), 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("scaled minimum=%s, want 3", got.String())
	}
}

func TestBuildProviderBillingDataAllowsZeroMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.DefaultMaxTokens = 10
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
