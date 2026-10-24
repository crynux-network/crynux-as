package llm

import (
	"crynux_as/service"
	"testing"
)

func TestSelectPricingExampleModelsCapsAtFourAcrossVRAM(t *testing.T) {
	models := []service.LoadedLLMModel{
		{ModelID: "a/low", MinVRAM: 8, NodeCount: 5, QuantizationKey: "null"},
		{ModelID: "c/mid", MinVRAM: 24, NodeCount: 2, QuantizationKey: "null"},
		{ModelID: "d/high", MinVRAM: 48, NodeCount: 3, QuantizationKey: "null"},
		{ModelID: "e/max", MinVRAM: 96, NodeCount: 4, QuantizationKey: "null"},
		{ModelID: "f/extra", MinVRAM: 128, NodeCount: 1, QuantizationKey: "null"},
		{ModelID: "g/top", MinVRAM: 160, NodeCount: 1, QuantizationKey: "null"},
		{ModelID: "z/skip", MinVRAM: 0, NodeCount: 99, QuantizationKey: "null"},
	}

	got := selectPricingExampleModels(models)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[0].ModelID != "a/low" || got[0].MinVRAM != 8 {
		t.Fatalf("first = %+v, want a/low @ 8", got[0])
	}
	if got[1].MinVRAM != 24 {
		t.Fatalf("second min_vram = %d, want 24", got[1].MinVRAM)
	}
	if got[2].MinVRAM != 96 {
		t.Fatalf("third min_vram = %d, want 96", got[2].MinVRAM)
	}
	if got[3].ModelID != "g/top" || got[3].MinVRAM != 160 {
		t.Fatalf("last = %+v, want g/top @ 160", got[3])
	}
}

func TestSelectPricingExampleModelsKeepsSameVRAMDifferentQuant(t *testing.T) {
	models := []service.LoadedLLMModel{
		{
			ModelID:         "qwen/qwen3.8-27b",
			MinVRAM:         96,
			NodeCount:       2,
			OnDiskNodeCount: 19,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ModelCacheHash:  "hash-full",
		},
		{
			ModelID:         "qwen/qwen3.8-27b",
			MinVRAM:         96,
			NodeCount:       2,
			OnDiskNodeCount: 19,
			ExecutionDType:  "bfloat16",
			QuantizationKey: `{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_8bit":true}}`,
			ModelCacheHash:  "hash-8bit",
		},
		{
			ModelID:         "qwen/qwen3.8-27b",
			MinVRAM:         24,
			NodeCount:       2,
			OnDiskNodeCount: 19,
			ExecutionDType:  "bfloat16",
			QuantizationKey: `{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_4bit":true}}`,
			ModelCacheHash:  "hash-4bit",
		},
		{
			ModelID:         "qwen/qwen3.5-9b",
			MinVRAM:         24,
			NodeCount:       3,
			OnDiskNodeCount: 20,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ModelCacheHash:  "hash-9b",
		},
	}

	got := selectPricingExampleModels(models)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}

	type rowKey struct {
		model string
		quant string
		vram  uint64
	}
	seen := map[rowKey]bool{}
	for _, row := range got {
		seen[rowKey{model: row.ModelID, quant: row.QuantizationKey, vram: row.MinVRAM}] = true
	}
	want := []rowKey{
		{model: "qwen/qwen3.5-9b", quant: "null", vram: 24},
		{model: "qwen/qwen3.8-27b", quant: `{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_4bit":true}}`, vram: 24},
		{model: "qwen/qwen3.8-27b", quant: "null", vram: 96},
		{model: "qwen/qwen3.8-27b", quant: `{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_8bit":true}}`, vram: 96},
	}
	for _, key := range want {
		if !seen[key] {
			t.Fatalf("missing row %+v in %#v", key, got)
		}
	}
}

func TestSelectPricingExampleModelsCollapsesDtypeForSameQuantAndVRAM(t *testing.T) {
	models := []service.LoadedLLMModel{
		{
			ModelID:         "org/model",
			MinVRAM:         24,
			NodeCount:       1,
			OnDiskNodeCount: 3,
			ExecutionDType:  "float16",
			QuantizationKey: "null",
			ModelCacheHash:  "hash-f16",
		},
		{
			ModelID:         "org/model",
			MinVRAM:         24,
			NodeCount:       5,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ModelCacheHash:  "hash-bf16",
		},
	}

	got := selectPricingExampleModels(models)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ExecutionDType != "bfloat16" || got[0].NodeCount != 5 {
		t.Fatalf("got = %+v, want bfloat16 with node count 5", got[0])
	}
}

func TestSelectPricingExampleModelsKeepsSameQuantDifferentVRAM(t *testing.T) {
	models := []service.LoadedLLMModel{
		{
			ModelID:         "org/model",
			MinVRAM:         24,
			NodeCount:       2,
			OnDiskNodeCount: 5,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ModelCacheHash:  "hash-24",
		},
		{
			ModelID:         "org/model",
			MinVRAM:         48,
			NodeCount:       2,
			OnDiskNodeCount: 5,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ModelCacheHash:  "hash-48",
		},
	}

	got := selectPricingExampleModels(models)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].MinVRAM != 24 || got[1].MinVRAM != 48 {
		t.Fatalf("got = %+v, want vram 24 then 48", got)
	}
}
