package models

import (
	"math/big"
	"testing"
)

func TestTaskBillingDataRoundTrip(t *testing.T) {
	encoded, err := EncodeTaskBillingData(TaskBillingData{
		Version:      TaskBillingDataVersion,
		PriorityGwei: "12",
		BilledVram:   24,
		TaskFeeWei:   "34000000000",
		LLM: &LLMTaskBillingData{
			VramWeight:            3,
			ConstantSeconds:       1,
			SecondsPerInputToken:  0.1,
			SecondsPerOutputToken: 0.2,
			CreditsPerGwei:        "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := TaskJob{TaskType: TaskTypeLLM, BillingData: encoded}
	decoded, err := job.DecodeBillingData()
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != TaskBillingDataVersion || decoded.LLM == nil || decoded.Image != nil {
		t.Fatalf("decoded billing data = %+v", decoded)
	}
	if decoded.PriorityGwei != "12" || decoded.TaskFeeWei != "34000000000" {
		t.Fatalf("decoded billing data = %+v", decoded)
	}
}

func TestTaskBillingDataRejectsVersionAndTypeMismatch(t *testing.T) {
	job := TaskJob{
		TaskType:    TaskTypeImage,
		BillingData: `{"version":2,"priority_gwei":"1","billed_vram":8,"task_fee_wei":"0","image":{"settlement_credits":"1"}}`,
	}
	if _, err := job.DecodeBillingData(); err == nil {
		t.Fatal("unsupported version was accepted")
	}

	job.BillingData = `{"version":1,"priority_gwei":"1","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":0,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`
	if _, err := job.DecodeBillingData(); err == nil {
		t.Fatal("image job without image billing data was accepted")
	}
}

func TestTaskBillingDataSupportsLargeWeiFee(t *testing.T) {
	fee := new(big.Int).Exp(big.NewInt(10), big.NewInt(40), nil)
	encoded, err := EncodeTaskBillingData(TaskBillingData{
		Version:      TaskBillingDataVersion,
		PriorityGwei: "1",
		BilledVram:   8,
		TaskFeeWei:   fee.String(),
		Image:        &ImageTaskBillingData{SettlementCredits: "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (TaskJob{TaskType: TaskTypeImage, BillingData: encoded}).DecodeBillingData(); err != nil {
		t.Fatal(err)
	}
}
