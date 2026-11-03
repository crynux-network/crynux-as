package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"math/big"
	"strings"
	"testing"
)

func setProviderBillingTestCatalog(t *testing.T, routes ...ProviderModelRoute) {
	t.Helper()
	catalog := make(map[string]ProviderModelRoute, len(routes))
	for _, route := range routes {
		catalog[route.ModelName] = route
	}
	providerCatalogMu.Lock()
	prev := providerCatalog
	providerCatalog = catalog
	providerCatalogMu.Unlock()
	t.Cleanup(func() {
		providerCatalogMu.Lock()
		providerCatalog = prev
		providerCatalogMu.Unlock()
	})
}

func TestEnforceAdmissionMinTaskFeeStaticRejects(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0.001",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(1_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
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

func TestEnforceAdmissionMinTaskFeeStaticRejectsMaxCreditsExceeded(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(50)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	priority := big.NewInt(100)
	gotPriority, _, err := EnforceAdmissionMinTaskFee(
		config.MaxTaskPriceProviderCrynux,
		project,
		priority,
		func(p *big.Int) (*big.Int, error) {
			return new(big.Int).Mul(p, big.NewInt(1)), nil
		},
	)
	if !errors.Is(err, ErrMaxCreditsExceeded) {
		t.Fatalf("err=%v, want ErrMaxCreditsExceeded", err)
	}
	if gotPriority != nil {
		t.Fatalf("priority=%v, want nil on Max Credits reject", gotPriority)
	}
}

func TestEnforceAdmissionMinTaskFeeStaticPassesWithinMaxCredits(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	gotPriority, fee, err := EnforceAdmissionMinTaskFee(
		config.MaxTaskPriceProviderCrynux,
		project,
		big.NewInt(40),
		func(p *big.Int) (*big.Int, error) {
			return new(big.Int).Set(p), nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotPriority.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("priority=%s, want 40", gotPriority.String())
	}
	if fee.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("fee=%s, want 40", fee.String())
	}
}

func TestEnforceAdmissionMinTaskFeeAutoRaisesToMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0.001",
		config.MinTaskPriceProviders:      "0",
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
		config.MaxTaskPriceProviderCrynux,
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
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })
	setProviderBillingTestCatalog(t)

	hints, err := ResolveProviderCostHints()
	if err != nil {
		t.Fatal(err)
	}
	if hints.MinPriorityGwei.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("MinPriorityGwei=%s, want 3", hints.MinPriorityGwei.String())
	}
	if hints.MinCreditsCheapest.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("MinCreditsCheapest=%s, want 1", hints.MinCreditsCheapest.String())
	}
	if hints.MinCreditsExpensive.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("MinCreditsExpensive=%s, want 1", hints.MinCreditsExpensive.String())
	}
	if hints.ReferenceInputTokens != ProviderMinReferenceInputTokens {
		t.Fatalf("ReferenceInputTokens=%d, want %d", hints.ReferenceInputTokens, ProviderMinReferenceInputTokens)
	}
	if hints.ReferenceOutputTokens != ProviderMinReferenceOutputTokens {
		t.Fatalf("ReferenceOutputTokens=%d, want %d", hints.ReferenceOutputTokens, ProviderMinReferenceOutputTokens)
	}
}

func TestResolveProviderCostHintsUsesReferenceWorkloadAndCheapestModel(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{
		"anthropic": "100",
		"openai":    "100",
	}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0.001", // 1e6 gwei
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	cheap := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "cheap",
		UsageWeights: map[string]string{
			"input_tokens":  "1",
			"output_tokens": "1",
		},
	}
	expensive := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "expensive",
		UsageWeights: map[string]string{
			"input_tokens":  "10",
			"output_tokens": "10",
		},
	}
	setProviderBillingTestCatalog(t, expensive, cheap)

	hints, err := ResolveProviderCostHints()
	if err != nil {
		t.Fatal(err)
	}
	// cheapest units = 10*1 + 50*1 = 60; priority = ceil(1e6 / 60) = 16667
	wantPriority := big.NewInt(16_667)
	if hints.MinPriorityGwei.Cmp(wantPriority) != 0 {
		t.Fatalf("MinPriorityGwei=%s, want %s", hints.MinPriorityGwei.String(), wantPriority.String())
	}
	// expensive units = 10*10 + 50*10 = 600; fee = 16667 * 600 = 10_000_200
	// cheap units = 60; fee = 16667 * 60 = 1_000_020
	wantExpensive := big.NewInt(10_000_200)
	wantCheapest := big.NewInt(1_000_020)
	if hints.MinCreditsExpensive.Cmp(wantExpensive) != 0 {
		t.Fatalf("MinCreditsExpensive=%s, want %s", hints.MinCreditsExpensive.String(), wantExpensive.String())
	}
	if hints.MinCreditsCheapest.Cmp(wantCheapest) != 0 {
		t.Fatalf("MinCreditsCheapest=%s, want %s", hints.MinCreditsCheapest.String(), wantCheapest.String())
	}
}

func TestEnforceAdmissionMinTaskFeeAutoRejectsWhenMaxCreditsTooLow(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0.001",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	autoMaxCredits := models.BigInt{Int: *big.NewInt(50)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	_, _, err := EnforceAdmissionMinTaskFee(
		config.MaxTaskPriceProviderCrynux,
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

func TestResolveProviderSharedMinPriorityUsesCheapestModel(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })
	setProviderBillingTestCatalog(t,
		ProviderModelRoute{
			ServiceProvider: models.ServiceProviderAnthropic,
			ModelName:       "cheap",
			UsageWeights:    map[string]string{"input_tokens": "1", "output_tokens": "1"},
		},
		ProviderModelRoute{
			ServiceProvider: models.ServiceProviderOpenAI,
			ModelName:       "expensive",
			UsageWeights:    map[string]string{"input_tokens": "10", "output_tokens": "10"},
		},
	)

	got, err := ResolveProviderSharedMinPriority(10, 50, config.ProviderAPIKindChat)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(16_667)) != 0 {
		t.Fatalf("shared min priority=%s, want 16667", got.String())
	}
}

func TestBuildProviderBillingDataRejectsStaticBelowSharedMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "cheap",
		UsageWeights: map[string]string{
			"input_tokens":  "1",
			"output_tokens": "1",
		},
	}
	setProviderBillingTestCatalog(t, route)

	autoMaxCredits := models.BigInt{Int: *big.NewInt(1_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	_, _, err := BuildProviderBillingData(project, route, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 10}, 50, nil)
	if !errors.Is(err, ErrTaskFeeBelowMinimum) {
		t.Fatalf("err=%v, want ErrTaskFeeBelowMinimum", err)
	}
}

func TestBuildProviderBillingDataAutoRaisesToSharedMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100", "openai": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	cheap := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "cheap",
		UsageWeights: map[string]string{
			"input_tokens":  "1",
			"output_tokens": "1",
		},
	}
	expensive := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "expensive",
		UsageWeights: map[string]string{
			"input_tokens":  "10",
			"output_tokens": "10",
		},
	}
	setProviderBillingTestCatalog(t, cheap, expensive)

	autoMaxCredits := models.BigInt{Int: *big.NewInt(100_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}

	cheapSnapshot, _, err := BuildProviderBillingData(project, cheap, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 10}, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	expensiveSnapshot, _, err := BuildProviderBillingData(project, expensive, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 10}, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cheapSnapshot.EffectivePriorityGwei != expensiveSnapshot.EffectivePriorityGwei {
		t.Fatalf(
			"cheap priority=%s expensive priority=%s, want shared lower bound",
			cheapSnapshot.EffectivePriorityGwei,
			expensiveSnapshot.EffectivePriorityGwei,
		)
	}
	if cheapSnapshot.EffectivePriorityGwei != "16667" {
		t.Fatalf("shared priority=%s, want 16667", cheapSnapshot.EffectivePriorityGwei)
	}
}

func TestBuildProviderBillingDataKeepsModelWeightProportions(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100", "openai": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	cheap := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "cheap",
		UsageWeights: map[string]string{
			"input_tokens":  "1",
			"output_tokens": "1",
		},
	}
	expensive := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "expensive",
		UsageWeights: map[string]string{
			"input_tokens":  "10",
			"output_tokens": "10",
		},
	}
	setProviderBillingTestCatalog(t, cheap, expensive)

	autoMaxCredits := models.BigInt{Int: *big.NewInt(100_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	_, cheapCredits, err := BuildProviderBillingData(project, cheap, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 10}, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, expensiveCredits, err := BuildProviderBillingData(project, expensive, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 10}, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Same shared priority, expensive units are 10x => credits should be 10x.
	want := new(big.Int).Mul(cheapCredits, big.NewInt(10))
	if expensiveCredits.Cmp(want) != 0 {
		t.Fatalf("expensive credits=%s cheap=%s, want 10x", expensiveCredits.String(), cheapCredits.String())
	}
}

func TestBuildProviderBillingDataAutoRejectsWhenMaxCreditsBelowSharedMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0.001",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderAnthropic,
		ModelName:       "cheap",
		UsageWeights: map[string]string{
			"input_tokens":  "1",
			"output_tokens": "1",
		},
	}
	setProviderBillingTestCatalog(t, route)

	autoMaxCredits := models.BigInt{Int: *big.NewInt(10)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	_, _, err := BuildProviderBillingData(project, route, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 10}, 50, nil)
	if !errors.Is(err, ErrAutoMaxCreditsInsufficient) {
		t.Fatalf("err=%v, want ErrAutoMaxCreditsInsufficient", err)
	}
}

func TestBuildProviderBillingDataAllowsZeroMinimum(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "0.00000125"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"anthropic": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
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
	setProviderBillingTestCatalog(t, route)

	autoMaxCredits := models.BigInt{Int: *big.NewInt(1_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	snapshot, credits, err := BuildProviderBillingData(project, route, big.NewInt(1), ProviderInputTokenEstimate{TextTokens: 100}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.EffectivePriorityGwei != "1" {
		t.Fatalf("EffectivePriorityGwei=%q, want 1", snapshot.EffectivePriorityGwei)
	}
	if credits.Sign() <= 0 {
		t.Fatalf("credits=%s", credits.String())
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

func TestProviderBillableKeepsFractionalGweiUntilFinalConversion(t *testing.T) {
	// priority=3, units=5/4 => billable=15/4 Gwei. Floor only at Credits/wei conversion.
	billable, err := ProviderBillableGwei(big.NewInt(3), big.NewRat(5, 4), "100")
	if err != nil {
		t.Fatal(err)
	}
	wantBillable := big.NewRat(15, 4)
	if billable.Cmp(wantBillable) != 0 {
		t.Fatalf("billable=%s, want %s", billable.RatString(), wantBillable.RatString())
	}

	credits, err := ProviderConsumerCredits(billable, "2")
	if err != nil {
		t.Fatal(err)
	}
	// floor((15/4)*2) = floor(7.5) = 7
	if credits.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("credits=%s, want 7", credits.String())
	}

	earning := ProviderEarningWei(billable)
	// floor((15/4)*1e9) = 3_750_000_000
	if earning.Cmp(big.NewInt(3_750_000_000)) != 0 {
		t.Fatalf("earning=%s, want 3750000000", earning.String())
	}
}

func TestProviderBillableUnderOneGweiStillYieldsFractionalWei(t *testing.T) {
	billable, err := ProviderBillableGwei(big.NewInt(1), big.NewRat(1, 4), "100")
	if err != nil {
		t.Fatal(err)
	}
	earning := ProviderEarningWei(billable)
	if earning.Cmp(big.NewInt(250_000_000)) != 0 {
		t.Fatalf("earning=%s, want 250000000", earning.String())
	}
	credits, err := ProviderConsumerCredits(billable, "1")
	if err != nil {
		t.Fatal(err)
	}
	if credits.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("credits=%s, want 1", credits.String())
	}
}

func TestApplyMaxTaskPriceCapComparesRationalBillable(t *testing.T) {
	// 1.5 Gwei capped to 1 Gwei when max_task_price_cnx = 1e-9 CNX.
	capped, err := ApplyMaxTaskPriceCap(big.NewRat(3, 2), "0.000000001")
	if err != nil {
		t.Fatal(err)
	}
	if capped.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatalf("capped=%s, want 1", capped.RatString())
	}
}

func TestEstimateProviderTextInputTokensSkipsImageURLPayload(t *testing.T) {
	large := strings.Repeat("A", 10000)
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"hello"},
			{"type":"image_url","image_url":{"url":"data:image/png;base64,` + large + `","detail":"high"}}
		]}]
	}`)
	got := EstimateProviderTextInputTokens(body)
	if got >= uint64(len(large)) {
		t.Fatalf("text estimate included image payload: got=%d", got)
	}
	if got < uint64(len("hello")) {
		t.Fatalf("text estimate missing hello: got=%d", got)
	}
}

func TestBuildProviderBillingDataRecordsImageTokenBreakdown(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"openai": "100"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "gpt-4o",
		UsageWeights: map[string]string{
			"input_tokens":  "1",
			"output_tokens": "1",
		},
	}
	setProviderBillingTestCatalog(t, route)
	autoMaxCredits := models.BigInt{Int: *big.NewInt(1_000_000)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
	snapshot, _, err := BuildProviderBillingData(
		project,
		route,
		big.NewInt(1),
		ProviderInputTokenEstimate{TextTokens: 10, ImageTokens: 765},
		20,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.EstimatedInputTokens != 775 || snapshot.EstimatedTextInputTokens != 10 || snapshot.EstimatedImageInputTokens != 765 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestEstimateProviderInputTokensRejectsUnsupportedOriginal(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAAEElEQVR4nGL6z8AACAAA//8DCQECWLbVUAAAAABJRU5ErkJggg=="
	body := []byte(`{
		"model":"gpt-5.2",
		"messages":[{"role":"user","content":[
			{"type":"image_url","image_url":{"url":"data:image/png;base64,` + png + `","detail":"original"}}
		]}]
	}`)
	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "gpt-5.2",
	}
	_, err := EstimateProviderInputTokens(route, models.TaskAPITypeChatCompletions, body, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "detail") {
		t.Fatalf("expected detail rejection, got %v", err)
	}
}

func TestEstimateProviderInputTokensSupportsGPT6Astra(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAAEElEQVR4nGL6z8AACAAA//8DCQECWLbVUAAAAABJRU5ErkJggg=="
	body := []byte(`{
		"model":"gpt-6-astra",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"hi"},
			{"type":"image_url","image_url":{"url":"data:image/png;base64,` + png + `","detail":"high"}}
		]}]
	}`)
	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "gpt-6-astra",
	}
	got, err := EstimateProviderInputTokens(route, models.TaskAPITypeChatCompletions, body, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	// 1x1 image -> 1 patch * 1.2 -> 2 image tokens
	if got.ImageTokens != 2 || got.TextTokens < uint64(len("hi")) {
		t.Fatalf("estimate=%+v", got)
	}
}

func TestEstimateProviderInputTokensRejectsInvalidReasoningEffort(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6-astra",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"ultra"
	}`)
	route := ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "gpt-6-astra",
	}
	_, err := EstimateProviderInputTokens(route, models.TaskAPITypeChatCompletions, body, 1<<20)
	if err == nil {
		t.Fatal("expected invalid reasoning_effort rejection before credits reservation")
	}
}

func TestCheapestProviderUnitsFiltersByAPIKind(t *testing.T) {
	setProviderBillingTestCatalog(t,
		ProviderModelRoute{
			ServiceProvider: models.ServiceProviderOpenAI,
			ModelName:       "chat-cheap",
			APIKind:         config.ProviderAPIKindChat,
			UsageWeights:    map[string]string{"input_tokens": "1", "output_tokens": "1"},
		},
		ProviderModelRoute{
			ServiceProvider: models.ServiceProviderOpenAI,
			ModelName:       "image-expensive",
			APIKind:         config.ProviderAPIKindImage,
			UsageWeights: map[string]string{
				"input_tokens":        "1",
				"image_input_tokens":  "1",
				"image_output_tokens": "100",
			},
		},
	)
	chatUnits, err := CheapestProviderUnits(10, 50, config.ProviderAPIKindChat)
	if err != nil {
		t.Fatal(err)
	}
	if chatUnits.Cmp(big.NewRat(60, 1)) != 0 {
		t.Fatalf("chat cheapest=%s, want 60", chatUnits.FloatString(18))
	}
	imageUnits, err := CheapestProviderUnits(10, 50, config.ProviderAPIKindImage)
	if err != nil {
		t.Fatal(err)
	}
	// max(input, image_input)*10 + max(output, image_output)*50 = 1*10 + 100*50
	if imageUnits.Cmp(big.NewRat(5010, 1)) != 0 {
		t.Fatalf("image cheapest=%s, want 5010", imageUnits.FloatString(18))
	}
}

func TestCalculateProviderUnitsImageComponents(t *testing.T) {
	units, err := CalculateProviderUnits(ProviderUsageComponents{
		InputTokens:       100,
		ImageInputTokens:  200,
		ImageOutputTokens: 1120,
	}, map[string]string{
		"input_tokens":        "0.625",
		"image_input_tokens":  "1",
		"image_output_tokens": "3.75",
		"output_tokens":       "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 100*0.625 + 200*1 + 1120*3.75 = 62.5 + 200 + 4200 = 4462.5
	want := big.NewRat(8925, 2)
	if units.Cmp(want) != 0 {
		t.Fatalf("units=%s, want %s", units.FloatString(18), want.FloatString(18))
	}
}

func TestCalculateProviderUnitsOpenAIReasoningIsSubset(t *testing.T) {
	units, err := CalculateProviderUnits(ProviderUsageComponents{
		OutputTokens:              10,
		ReasoningTokens:           4,
		ReasoningIncludedInOutput: true,
	}, map[string]string{
		"output_tokens":     "1",
		"reasoning_tokens":  "1",
		"input_tokens":      "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// (10-4)*1 + 4*1 = 10
	if units.Cmp(big.NewRat(10, 1)) != 0 {
		t.Fatalf("units=%s", units.FloatString(18))
	}
}

func TestCalculateProviderUnitsGoogleReasoningIsAdditional(t *testing.T) {
	units, err := CalculateProviderUnits(ProviderUsageComponents{
		OutputTokens:              3,
		ReasoningTokens:           1,
		ReasoningIncludedInOutput: false,
	}, map[string]string{
		"output_tokens":    "5",
		"reasoning_tokens": "5",
		"input_tokens":     "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 3*5 + 1*5 = 20
	if units.Cmp(big.NewRat(20, 1)) != 0 {
		t.Fatalf("units=%s", units.FloatString(18))
	}
}

func TestCalculateProviderUnitsEffortDoesNotChangeWeights(t *testing.T) {
	weights := map[string]string{
		"input_tokens":     "1",
		"output_tokens":    "5",
		"reasoning_tokens": "5",
	}
	low, err := CalculateProviderUnits(ProviderUsageComponents{
		InputTokens:               10,
		OutputTokens:              20,
		ReasoningTokens:           8,
		ReasoningIncludedInOutput: true,
	}, weights)
	if err != nil {
		t.Fatal(err)
	}
	high, err := CalculateProviderUnits(ProviderUsageComponents{
		InputTokens:               10,
		OutputTokens:              40,
		ReasoningTokens:           25,
		ReasoningIncludedInOutput: true,
	}, weights)
	if err != nil {
		t.Fatal(err)
	}
	if low.Cmp(high) >= 0 {
		t.Fatalf("higher actual token usage must increase units: low=%s high=%s", low, high)
	}
}
