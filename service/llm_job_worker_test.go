package service

import (
	"math/big"
	"testing"

	"crynux_as/models"
)

func TestLLMJobTaskFeeWei(t *testing.T) {
	job := &models.LLMJob{
		TaskFeeGwei: &models.BigInt{Int: *big.NewInt(321)},
	}
	got, err := llmJobTaskFeeWei(job)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(321_000_000_000)) != 0 {
		t.Fatalf("task fee = %s, want 321000000000", got)
	}
}

func TestLLMJobTaskFeeWeiSupportsFeeAboveUint64(t *testing.T) {
	taskFeeGwei, ok := new(big.Int).SetString("20000000000", 10)
	if !ok {
		t.Fatal("parse task fee")
	}
	job := &models.LLMJob{
		TaskFeeGwei: &models.BigInt{Int: *taskFeeGwei},
	}
	got, err := llmJobTaskFeeWei(job)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "20000000000000000000" {
		t.Fatalf("task fee = %s, want 20000000000000000000", got)
	}
}

func TestLLMJobTaskFeeWeiRejectsMissingAndNegative(t *testing.T) {
	if _, err := llmJobTaskFeeWei(&models.LLMJob{}); err == nil {
		t.Fatal("missing task fee was accepted")
	}

	job := &models.LLMJob{
		TaskFeeGwei: &models.BigInt{Int: *big.NewInt(-1)},
	}
	if _, err := llmJobTaskFeeWei(job); err == nil {
		t.Fatal("negative task fee was accepted")
	}
}
