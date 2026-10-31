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
	withPriorityBounds(t, "1", "1000000000")
	got, err := ParsePriorityGwei("1000")
	if err != nil {
		t.Fatalf("ParsePriorityGwei: %v", err)
	}
	if got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("priority = %s, want 1000", got.String())
	}
	got, err = ParsePriorityGwei("2000")
	if err != nil {
		t.Fatalf("ParsePriorityGwei: %v", err)
	}
	if got.Cmp(big.NewInt(2000)) != 0 {
		t.Fatalf("priority = %s, want 2000", got.String())
	}
}

func TestParsePriorityGweiRejected(t *testing.T) {
	withPriorityBounds(t, "1", "1000000000")
	for _, value := range []string{"", "0", "1", "999", "1001", "1500", "1.5", "-1"} {
		if _, err := ParsePriorityGwei(value); err == nil {
			t.Fatalf("ParsePriorityGwei(%q) should fail", value)
		}
	}
}

func TestStaticProjectPriorityBounds(t *testing.T) {
	withPriorityBounds(t, "1", "1000000000")
	minPriority, maxPriority, err := StaticProjectPriorityBounds()
	if err != nil {
		t.Fatal(err)
	}
	if minPriority.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("min = %s, want 1000", minPriority.String())
	}
	if maxPriority.Cmp(big.NewInt(1000000000)) != 0 {
		t.Fatalf("max = %s, want 1000000000", maxPriority.String())
	}

	withPriorityBounds(t, "1501", "9999")
	minPriority, maxPriority, err = StaticProjectPriorityBounds()
	if err != nil {
		t.Fatal(err)
	}
	if minPriority.Cmp(big.NewInt(2000)) != 0 {
		t.Fatalf("min = %s, want 2000", minPriority.String())
	}
	if maxPriority.Cmp(big.NewInt(9000)) != 0 {
		t.Fatalf("max = %s, want 9000", maxPriority.String())
	}
}

func TestNormalizeStaticProjectPriorityGwei(t *testing.T) {
	withPriorityBounds(t, "1", "1000000000")
	cases := []struct {
		in   int64
		want int64
	}{
		{1, 1000},
		{499, 1000},
		{500, 1000},
		{1499, 1000},
		{1500, 2000},
		{1000, 1000},
		{2000, 2000},
	}
	for _, tc := range cases {
		got, err := NormalizeStaticProjectPriorityGwei(big.NewInt(tc.in))
		if err != nil {
			t.Fatalf("NormalizeStaticProjectPriorityGwei(%d): %v", tc.in, err)
		}
		if got.Cmp(big.NewInt(tc.want)) != 0 {
			t.Fatalf("NormalizeStaticProjectPriorityGwei(%d) = %s, want %d", tc.in, got.String(), tc.want)
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

func TestResolveEffectivePriorityGweiAutoLinearFloor(t *testing.T) {
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
	credits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoQueuePosition:     &position,
		AutoMaxCreditsPerTask: &credits,
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
	got, err = ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve no longer caps by Max Credits; 10 + (21-10) = 21
	if got.Cmp(big.NewInt(21)) != 0 {
		t.Fatalf("got %s, want 21", got.String())
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
	credits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoQueuePosition:     &position,
		AutoMaxCreditsPerTask: &credits,
	}
	got, err := ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("got %s, want 7", got.String())
	}
}

func TestResolveEffectivePriorityGweiAutoZeroLowestUsesLinearMap(t *testing.T) {
	withPriorityBounds(t, "1", "1000000000")
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)
	setQueuedPriorityCacheStateForTest(QueuedPrioritySnapshot{
		AsOf:                1,
		QueuedTaskCount:     169,
		HighestPriorityGwei: big.NewInt(5596),
		LowestPriorityGwei:  big.NewInt(0),
		MedianPriorityGwei:  big.NewInt(35),
	}, big.NewInt(35))

	position := 97
	credits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoQueuePosition:     &position,
		AutoMaxCreditsPerTask: &credits,
	}
	got, err := ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	// 0 + floor((5596-0)*97/100) = 5428
	if got.Cmp(big.NewInt(5428)) != 0 {
		t.Fatalf("got %s, want 5428", got.String())
	}
}

func TestResolveEffectivePriorityGweiAutoZeroHighestFallsBackToMin(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	resetQueuedPriorityCacheForTest()
	t.Cleanup(resetQueuedPriorityCacheForTest)
	setQueuedPriorityCacheStateForTest(QueuedPrioritySnapshot{
		AsOf:                1,
		QueuedTaskCount:     2,
		HighestPriorityGwei: big.NewInt(0),
		LowestPriorityGwei:  big.NewInt(0),
		MedianPriorityGwei:  big.NewInt(0),
	}, big.NewInt(0))

	position := 97
	credits := models.BigInt{Int: *big.NewInt(100)}
	project := &models.Project{
		CostLevelMode:         models.CostLevelModeAuto,
		AutoQueuePosition:     &position,
		AutoMaxCreditsPerTask: &credits,
	}
	got, err := ResolveEffectivePriorityGwei(project)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("got %s, want 1", got.String())
	}
}

func TestResolveEffectivePriorityGweiAutoRequiresMaxCredits(t *testing.T) {
	withPriorityBounds(t, "1", "1000")
	project := &models.Project{
		CostLevelMode: models.CostLevelModeAuto,
	}
	if _, err := ResolveEffectivePriorityGwei(project); err == nil {
		t.Fatal("expected error when auto max credits is missing")
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

func TestParseAutoMaxCreditsPerTask(t *testing.T) {
	got, err := ParseAutoMaxCreditsPerTask("100")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("got %s, want 100", got.String())
	}
	if _, err := ParseAutoMaxCreditsPerTask("0"); err == nil {
		t.Fatal("expected error for 0")
	}
	if _, err := ParseAutoMaxCreditsPerTask("1.5"); err == nil {
		t.Fatal("expected error for fractional value")
	}
	if _, err := ParseAutoMaxCreditsPerTask(""); err == nil {
		t.Fatal("expected error for empty value")
	}
}

