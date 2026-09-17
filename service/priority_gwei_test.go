package service

import (
	"crynux_as/config"
	"crynux_as/models"
	"math/big"
	"testing"
)

func withPriorityBounds(t *testing.T, min, max string) {
	t.Helper()
	cfg := &config.AppConfig{}
	cfg.LLM.MinPriorityGwei = min
	cfg.LLM.MaxPriorityGwei = max
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })
}

func TestParsePriorityGweiAllowed(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	got, err := ParsePriorityGwei("34")
	if err != nil {
		t.Fatalf("ParsePriorityGwei: %v", err)
	}
	if got.Cmp(big.NewInt(34)) != 0 {
		t.Fatalf("priority = %s, want 34", got.String())
	}
}

func TestParsePriorityGweiRejected(t *testing.T) {
	withPriorityBounds(t, "10", "100")
	for _, value := range []string{"", "0", "9", "101", "1.5", "-1"} {
		if _, err := ParsePriorityGwei(value); err == nil {
			t.Fatalf("ParsePriorityGwei(%q) should fail", value)
		}
	}
}

func TestClampPriorityGwei(t *testing.T) {
	withPriorityBounds(t, "10", "100")
	low, err := ClampPriorityGwei(big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if low.Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("clamped low = %s, want 10", low.String())
	}
	high, err := ClampPriorityGwei(big.NewInt(1000))
	if err != nil {
		t.Fatal(err)
	}
	if high.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("clamped high = %s, want 100", high.String())
	}
}

func TestResolveEffectivePriorityGweiStatic(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	project := &models.Project{
		CostLevelMode: models.CostLevelModeStatic,
		PriorityGwei:  models.BigInt{Int: *big.NewInt(42)},
	}
	got, err := ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("got %s, want 42", got.String())
	}
}

func TestResolveEffectivePriorityGweiAutoLinearFloorAndCap(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)
	setQueuedPriorityCacheStateForTest(QueuedPrioritySnapshot{
		AsOf:                1,
		QueuedTaskCount:     3,
		HighestPriorityGwei: big.NewInt(21),
		LowestPriorityGwei:  big.NewInt(10),
		MedianPriorityGwei:  big.NewInt(15),
	}, big.NewInt(15))

	position := 50
	cap := models.BigInt{Int: *big.NewInt(1000)}
	project := &models.Project{
		CostLevelMode:       models.CostLevelModeAuto,
		AutoQueuePosition:   &position,
		AutoMaxPriorityGwei: &cap,
	}
	got, err := ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	// 10 + floor((21-10)*50/100) = 10 + floor(5.5) = 15
	if got.Cmp(big.NewInt(15)) != 0 {
		t.Fatalf("got %s, want 15", got.String())
	}

	position = 100
	project.AutoQueuePosition = &position
	lowCap := models.BigInt{Int: *big.NewInt(12)}
	project.AutoMaxPriorityGwei = &lowCap
	got, err = ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(12)) != 0 {
		t.Fatalf("capped got %s, want 12", got.String())
	}
}

func TestResolveEffectivePriorityGweiAutoEmptyQueueUsesMin(t *testing.T) {
	withPriorityBounds(t, "7", "1000")
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)
	setQueuedPriorityCacheStateForTest(QueuedPrioritySnapshot{
		AsOf:            1,
		QueuedTaskCount: 0,
	}, nil)

	position := 80
	cap := models.BigInt{Int: *big.NewInt(500)}
	project := &models.Project{
		CostLevelMode:       models.CostLevelModeAuto,
		AutoQueuePosition:   &position,
		AutoMaxPriorityGwei: &cap,
	}
	got, err := ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("got %s, want 7", got.String())
	}
}

func TestResolveEffectivePriorityGweiAutoRequiresMax(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	project := &models.Project{
		CostLevelMode: models.CostLevelModeAuto,
	}
	if _, err := ResolveEffectivePriorityGwei(project); err == nil {
		t.Fatal("expected error when auto max is missing")
	}
}

func TestParseAutoQueuePosition(t *testing.T) {
	if _, err := ParseAutoQueuePosition(-1); err == nil {
		t.Fatal("expected error for -1")
	}
	if _, err := ParseAutoQueuePosition(101); err == nil {
		t.Fatal("expected error for 101")
	}
	got, err := ParseAutoQueuePosition(0)
	if err != nil || got != 0 {
		t.Fatalf("got %d err %v", got, err)
	}
}

