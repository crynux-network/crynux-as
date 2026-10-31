package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"math/big"
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
	if hints.MinCredits.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("MinCredits=%s, want 1", hints.MinCredits.String())
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
	wantCredits := big.NewInt(10_000_200)
	if hints.MinCredits.Cmp(wantCredits) != 0 {
		t.Fatalf("MinCredits=%s, want %s", hints.MinCredits.String(), wantCredits.String())
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

	got, err := ResolveProviderSharedMinPriority(10, 50)
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
	_, _, err := BuildProviderBillingData(project, route, big.NewInt(1), 10, 50, nil)
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

	cheapSnapshot, _, err := BuildProviderBillingData(project, cheap, big.NewInt(1), 10, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	expensiveSnapshot, _, err := BuildProviderBillingData(project, expensive, big.NewInt(1), 10, 50, nil)
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
	_, cheapCredits, err := BuildProviderBillingData(project, cheap, big.NewInt(1), 10, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, expensiveCredits, err := BuildProviderBillingData(project, expensive, big.NewInt(1), 10, 50, nil)
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
	_, _, err := BuildProviderBillingData(project, route, big.NewInt(1), 10, 50, nil)
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
	snapshot, credits, err := BuildProviderBillingData(project, route, big.NewInt(1), 100, 10, nil)
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
