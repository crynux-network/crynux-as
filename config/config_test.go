package config

import (
	"math/big"
	"testing"
)

func TestParseEmptyQueueMedianPriorityGweiPreservesLargeInteger(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLM.EmptyQueueMedianPriorityGwei = "20000000000"

	got, err := cfg.ParseEmptyQueueMedianPriorityGwei()
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "20000000000" {
		t.Fatalf("median priority = %s, want 20000000000", got)
	}
}

func TestParseEmptyQueueMedianPriorityGweiRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "+1", "1.5", " 1"} {
		cfg := &AppConfig{}
		cfg.LLM.EmptyQueueMedianPriorityGwei = value
		if _, err := cfg.ParseEmptyQueueMedianPriorityGwei(); err == nil {
			t.Errorf("median priority %q was accepted", value)
		}
	}
}

func TestMaxTaskFeeGwei(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLM.MaxTaskPriceCNX = map[string]string{
		MaxTaskPriceProviderCrynux: "0.001",
		"openai":                   "0.002",
	}
	got, err := cfg.MaxTaskFeeGwei(MaxTaskPriceProviderCrynux)
	if err != nil {
		t.Fatal(err)
	}
	want := big.NewInt(1_000_000)
	if got.Cmp(want) != 0 {
		t.Fatalf("MaxTaskFeeGwei(crynux) = %s, want %s", got.String(), want.String())
	}
	got, err = cfg.MaxTaskFeeGwei("openai")
	if err != nil {
		t.Fatal(err)
	}
	want = big.NewInt(2_000_000)
	if got.Cmp(want) != 0 {
		t.Fatalf("MaxTaskFeeGwei(openai) = %s, want %s", got.String(), want.String())
	}
}

func TestMaxTaskFeeGweiRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "abc", "1e-12"} {
		cfg := &AppConfig{}
		cfg.LLM.MaxTaskPriceCNX = map[string]string{
			MaxTaskPriceProviderCrynux: value,
		}
		if _, err := cfg.MaxTaskFeeGwei(MaxTaskPriceProviderCrynux); err == nil {
			t.Errorf("max_task_price_cnx %q was accepted", value)
		}
	}
	cfg := &AppConfig{}
	cfg.LLM.MaxTaskPriceCNX = map[string]string{
		MaxTaskPriceProviderCrynux: "0.001",
	}
	if _, err := cfg.MaxTaskFeeGwei("openai"); err == nil {
		t.Fatal("missing provider max_task_price_cnx was accepted")
	}
}

func TestMinTaskFeeGweiAllowsZero(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		MaxTaskPriceProviderCrynux: "0",
		"openai":                   "0.001",
	}
	got, err := cfg.MinTaskFeeGwei(MaxTaskPriceProviderCrynux)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sign() != 0 {
		t.Fatalf("MinTaskFeeGwei(crynux) = %s, want 0", got.String())
	}
	got, err = cfg.MinTaskFeeGwei("openai")
	if err != nil {
		t.Fatal(err)
	}
	want := big.NewInt(1_000_000)
	if got.Cmp(want) != 0 {
		t.Fatalf("MinTaskFeeGwei(openai) = %s, want %s", got.String(), want.String())
	}
}

func TestMinTaskFeeGweiRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "-1", "abc"} {
		cfg := &AppConfig{}
		cfg.LLM.MinTaskPriceCNX = map[string]string{
			MaxTaskPriceProviderCrynux: value,
		}
		if _, err := cfg.MinTaskFeeGwei(MaxTaskPriceProviderCrynux); err == nil {
			t.Errorf("min_task_price_cnx %q was accepted", value)
		}
	}
	cfg := &AppConfig{}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		MaxTaskPriceProviderCrynux: "0",
	}
	if _, err := cfg.MinTaskFeeGwei("openai"); err == nil {
		t.Fatal("missing provider min_task_price_cnx was accepted")
	}
}

func TestCheckMinTaskPriceCNXConfigRejectsAboveMax(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { SetConfigForTest(prev) })
	cfg := &AppConfig{}
	cfg.LLM.MaxTaskPriceCNX = map[string]string{
		MaxTaskPriceProviderCrynux: "0.001",
	}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		MaxTaskPriceProviderCrynux: "0.002",
	}
	SetConfigForTest(cfg)
	if err := checkMinTaskPriceCNXConfig(); err == nil {
		t.Fatal("min above max was accepted")
	}
}
