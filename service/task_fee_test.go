package service

import (
	"math/big"
	"testing"
)

func TestCalcBillableAndCreditsBasic(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:          big.NewInt(10),
		EffectiveVram:         8,
		BaseVram:              8,
		ConstantSeconds:       30,
		SecondsPerInputToken:  0.001,
		SecondsPerOutputToken: 0.01,
		PromptTokens:          1000,
		CompletionTokens:      100,
	})
	if err != nil {
		t.Fatalf("CalcBillableGwei: %v", err)
	}
	// estimated_node_seconds = 30 + 0.001*1000 + 0.01*100 = 32
	// vram_weight = 1
	// billable = 10 * 32 * 1 = 320
	if got.EstimatedNodeSeconds != 32 {
		t.Fatalf("EstimatedNodeSeconds = %v, want 32", got.EstimatedNodeSeconds)
	}
	if got.VramWeight != 1 {
		t.Fatalf("VramWeight = %v, want 1", got.VramWeight)
	}
	if got.TaskFeeGwei.Cmp(big.NewInt(320)) != 0 {
		t.Fatalf("TaskFeeGwei = %s, want 320", got.TaskFeeGwei.String())
	}
	rate, err := ParseCreditsPerGwei("2")
	if err != nil {
		t.Fatalf("ParseCreditsPerGwei: %v", err)
	}
	credits, err := CalcCreditsFromBillable(got.BillableGwei, rate)
	if err != nil {
		t.Fatalf("CalcCreditsFromBillable: %v", err)
	}
	if credits.Cmp(big.NewInt(640)) != 0 {
		t.Fatalf("Credits = %s, want 640", credits.String())
	}
}

func TestCalcBillablePriorityAndVramWeight(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:          big.NewInt(10),
		EffectiveVram:         24,
		BaseVram:              8,
		ConstantSeconds:       10,
		SecondsPerInputToken:  0,
		SecondsPerOutputToken: 0,
		PromptTokens:          0,
		CompletionTokens:      0,
	})
	if err != nil {
		t.Fatalf("CalcBillableGwei: %v", err)
	}
	// estimated = 10, vram_weight = 3, billable = 10 * 10 * 3 = 300
	if got.VramWeight != 3 {
		t.Fatalf("VramWeight = %v, want 3", got.VramWeight)
	}
	if got.TaskFeeGwei.Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("TaskFeeGwei = %s, want 300", got.TaskFeeGwei.String())
	}
}

func TestCalcBillableTruncatesTowardZero(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:          big.NewInt(1),
		EffectiveVram:         8,
		BaseVram:              8,
		ConstantSeconds:       0.29,
		SecondsPerInputToken:  0,
		SecondsPerOutputToken: 0,
		PromptTokens:          0,
		CompletionTokens:      0,
	})
	if err != nil {
		t.Fatalf("CalcBillableGwei: %v", err)
	}
	if got.TaskFeeGwei.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("TaskFeeGwei = %s, want 0", got.TaskFeeGwei.String())
	}
}

func TestCalcBillableVramWeightUsesBaseWhenEffectiveLower(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		PriorityGwei:          big.NewInt(10),
		EffectiveVram:         4,
		BaseVram:              8,
		ConstantSeconds:       5,
		SecondsPerInputToken:  0,
		SecondsPerOutputToken: 0,
		PromptTokens:          0,
		CompletionTokens:      0,
	})
	if err != nil {
		t.Fatalf("CalcBillableGwei: %v", err)
	}
	if got.VramWeight != 1 {
		t.Fatalf("VramWeight = %v, want 1", got.VramWeight)
	}
}

func TestCalcCreditsFromBillableDecimalRate(t *testing.T) {
	rate, err := ParseCreditsPerGwei("0.000001")
	if err != nil {
		t.Fatalf("ParseCreditsPerGwei: %v", err)
	}
	credits, err := CalcCreditsFromBillable(big.NewFloat(1_000_000_000), rate)
	if err != nil {
		t.Fatalf("CalcCreditsFromBillable: %v", err)
	}
	if credits.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("Credits = %s, want 1000", credits.String())
	}
}

func TestCalcCreditsFromBillableMinimumOne(t *testing.T) {
	rate, err := ParseCreditsPerGwei("0.000001")
	if err != nil {
		t.Fatalf("ParseCreditsPerGwei: %v", err)
	}
	credits, err := CalcCreditsFromBillable(big.NewFloat(373083.24), rate)
	if err != nil {
		t.Fatalf("CalcCreditsFromBillable: %v", err)
	}
	if credits.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("Credits = %s, want 1", credits.String())
	}
}

func TestParseCreditsPerGweiRejected(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "abc", "1/0"} {
		if _, err := ParseCreditsPerGwei(value); err == nil {
			t.Fatalf("ParseCreditsPerGwei(%q) should fail", value)
		}
	}
}
