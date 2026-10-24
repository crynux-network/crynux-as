package llm

import (
	"crynux_as/service"
	"testing"
)

func TestSelectImagePricingExampleModelsCapsAtFourAcrossVRAM(t *testing.T) {
	models := []service.LoadedLLMModel{
		{ModelID: "a/low", MinVRAM: 8, NodeCount: 5, QuantizationKey: "null", ComponentsKey: "{}"},
		{ModelID: "c/mid", MinVRAM: 24, NodeCount: 2, QuantizationKey: "null", ComponentsKey: "{}"},
		{ModelID: "d/high", MinVRAM: 48, NodeCount: 3, QuantizationKey: "null", ComponentsKey: "{}"},
		{ModelID: "e/max", MinVRAM: 96, NodeCount: 4, QuantizationKey: "null", ComponentsKey: "{}"},
		{ModelID: "f/extra", MinVRAM: 128, NodeCount: 1, QuantizationKey: "null", ComponentsKey: "{}"},
		{ModelID: "g/top", MinVRAM: 160, NodeCount: 1, QuantizationKey: "null", ComponentsKey: "{}"},
		{ModelID: "z/skip", MinVRAM: 0, NodeCount: 99, QuantizationKey: "null", ComponentsKey: "{}"},
	}

	got := selectImagePricingExampleModels(models)
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

func TestSelectImagePricingExampleModelsKeepsSameVRAMDifferentComponents(t *testing.T) {
	models := []service.LoadedLLMModel{
		{
			ModelID:         "org/sd",
			MinVRAM:         24,
			NodeCount:       2,
			OnDiskNodeCount: 10,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ComponentsKey:   `{"unet":"a"}`,
			ModelCacheHash:  "hash-a",
		},
		{
			ModelID:         "org/sd",
			MinVRAM:         24,
			NodeCount:       2,
			OnDiskNodeCount: 10,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ComponentsKey:   `{"unet":"b"}`,
			ModelCacheHash:  "hash-b",
		},
		{
			ModelID:         "org/sd",
			MinVRAM:         24,
			NodeCount:       2,
			OnDiskNodeCount: 10,
			ExecutionDType:  "bfloat16",
			QuantizationKey: `{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_4bit":true}}`,
			ComponentsKey:   `{"unet":"a"}`,
			ModelCacheHash:  "hash-4bit",
		},
	}

	got := selectImagePricingExampleModels(models)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	type rowKey struct {
		model      string
		quant      string
		components string
		vram       uint64
	}
	seen := map[rowKey]bool{}
	for _, row := range got {
		seen[rowKey{
			model:      row.ModelID,
			quant:      row.QuantizationKey,
			components: row.ComponentsKey,
			vram:       row.MinVRAM,
		}] = true
	}
	want := []rowKey{
		{model: "org/sd", quant: "null", components: `{"unet":"a"}`, vram: 24},
		{model: "org/sd", quant: "null", components: `{"unet":"b"}`, vram: 24},
		{
			model:      "org/sd",
			quant:      `{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_4bit":true}}`,
			components: `{"unet":"a"}`,
			vram:       24,
		},
	}
	for _, key := range want {
		if !seen[key] {
			t.Fatalf("missing row %+v in %#v", key, got)
		}
	}
}

func TestSelectImagePricingExampleModelsCollapsesDtypeForSameIdentity(t *testing.T) {
	models := []service.LoadedLLMModel{
		{
			ModelID:         "org/sd",
			MinVRAM:         24,
			NodeCount:       1,
			OnDiskNodeCount: 3,
			ExecutionDType:  "float16",
			QuantizationKey: "null",
			ComponentsKey:   `{"unet":"a"}`,
			ModelCacheHash:  "hash-f16",
		},
		{
			ModelID:         "org/sd",
			MinVRAM:         24,
			NodeCount:       5,
			OnDiskNodeCount: 3,
			ExecutionDType:  "bfloat16",
			QuantizationKey: "null",
			ComponentsKey:   `{"unet":"a"}`,
			ModelCacheHash:  "hash-bf16",
		},
	}

	got := selectImagePricingExampleModels(models)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ExecutionDType != "bfloat16" || got[0].NodeCount != 5 {
		t.Fatalf("got = %+v, want bfloat16 with node count 5", got[0])
	}
}
