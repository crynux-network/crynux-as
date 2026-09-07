package service

import (
	"math/big"
	"testing"
)

func TestCalcBillableAndCreditsBasic(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		ReferencePriorityGwei: big.NewInt(10),
		TokenRatioStored:      10, // display 1.0
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
	// billable = 10 * 1.0 * 32 * 1 = 320
	if got.EstimatedNodeSeconds != 32 {
		t.Fatalf("EstimatedNodeSeconds = %v, want 32", got.EstimatedNodeSeconds)
	}
	if got.VramWeight != 1 {
		t.Fatalf("VramWeight = %v, want 1", got.VramWeight)
	}
	if got.TaskFeeGwei.Cmp(big.NewInt(320)) != 0 {
		t.Fatalf("TaskFeeGwei = %s, want 320", got.TaskFeeGwei.String())
	}
	credits, err := CalcCreditsFromBillable(got.BillableGwei, 2)
	if err != nil {
		t.Fatalf("CalcCreditsFromBillable: %v", err)
	}
	if credits.Cmp(big.NewInt(640)) != 0 {
		t.Fatalf("Credits = %s, want 640", credits.String())
	}
}

func TestCalcBillableTokenRatioAndVramWeight(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		ReferencePriorityGwei: big.NewInt(20),
		TokenRatioStored:      5, // display 0.5
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
	// estimated = 10, vram_weight = 24/8 = 3, product = 20 * 0.5 * 10 * 3 = 300
	if got.VramWeight != 3 {
		t.Fatalf("VramWeight = %v, want 3", got.VramWeight)
	}
	if got.TaskFeeGwei.Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("TaskFeeGwei = %s, want 300", got.TaskFeeGwei.String())
	}
}

func TestCalcBillableTruncatesTowardZero(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		ReferencePriorityGwei: big.NewInt(1),
		TokenRatioStored:      1, // display 0.1
		EffectiveVram:         8,
		BaseVram:              8,
		ConstantSeconds:       2.9,
		SecondsPerInputToken:  0,
		SecondsPerOutputToken: 0,
		PromptTokens:          0,
		CompletionTokens:      0,
	})
	if err != nil {
		t.Fatalf("CalcBillableGwei: %v", err)
	}
	// product = 1 * 0.1 * 2.9 * 1 = 0.29 -> floor 0
	if got.TaskFeeGwei.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("TaskFeeGwei = %s, want 0", got.TaskFeeGwei.String())
	}
}

func TestCalcBillableVramWeightUsesBaseWhenEffectiveLower(t *testing.T) {
	got, err := CalcBillableGwei(CalcBillableInput{
		ReferencePriorityGwei: big.NewInt(10),
		TokenRatioStored:      10,
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

func TestCalcCreditsWithPersistedWeight(t *testing.T) {
	credits, err := CalcCredits(
		1000, 100,
		10,
		3,
		10, 0, 0,
		big.NewInt(20),
		1,
	)
	if err != nil {
		t.Fatalf("CalcCredits: %v", err)
	}
	// estimated = 10, billable = 20 * 1.0 * 10 * 3 = 600
	if credits.Cmp(big.NewInt(600)) != 0 {
		t.Fatalf("Credits = %s, want 600", credits.String())
	}
}
