package llm

import (
	"crynux_as/service"
	"testing"
)

func TestSelectPricingExampleModelsPicksPerVRAMAndCapsAtFour(t *testing.T) {
	models := []service.LoadedLLMModel{
		{ModelID: "a/low", MinVRAM: 8, NodeCount: 1},
		{ModelID: "b/low", MinVRAM: 8, NodeCount: 5},
		{ModelID: "c/mid", MinVRAM: 24, NodeCount: 2},
		{ModelID: "d/high", MinVRAM: 48, NodeCount: 3},
		{ModelID: "e/max", MinVRAM: 96, NodeCount: 4},
		{ModelID: "f/extra", MinVRAM: 128, NodeCount: 1},
		{ModelID: "z/skip", MinVRAM: 0, NodeCount: 99},
	}

	got := selectPricingExampleModels(models)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[0].ModelID != "b/low" || got[0].MinVRAM != 8 {
		t.Fatalf("first = %+v, want b/low @ 8", got[0])
	}
	if got[1].MinVRAM != 24 {
		t.Fatalf("second min_vram = %d, want 24", got[1].MinVRAM)
	}
	if got[2].MinVRAM != 48 {
		t.Fatalf("third min_vram = %d, want 48", got[2].MinVRAM)
	}
	if got[3].ModelID != "f/extra" || got[3].MinVRAM != 128 {
		t.Fatalf("last = %+v, want f/extra @ 128", got[3])
	}
}
