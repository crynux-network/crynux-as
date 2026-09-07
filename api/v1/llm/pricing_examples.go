package llm

import (
	"crynux_as/api/v1/response"
	"crynux_as/service"
	"math"
	"sort"

	"github.com/gin-gonic/gin"
)

const (
	pricingPromptTokens     uint64 = 1_000_000
	pricingCompletionTokens uint64 = 1_000_000
	timePromptTokens        uint64 = 512
	timeCompletionTokens    uint64 = 2048
)

type PricingExampleRow struct {
	Model                 string  `json:"model" description:"Selected model id"`
	MinVRAM               uint64  `json:"min_vram" description:"Model minimum VRAM in GB"`
	ConstantSeconds       float64 `json:"constant_seconds" description:"Relay constant seconds coefficient"`
	SecondsPerInputToken  float64 `json:"seconds_per_input_token" description:"Relay seconds per input token"`
	SecondsPerOutputToken float64 `json:"seconds_per_output_token" description:"Relay seconds per output token"`
}

type PricingExamplesData struct {
	PricingPromptTokens     uint64              `json:"pricing_prompt_tokens" description:"Token count for the Credits-per-1M Input unit example"`
	PricingCompletionTokens uint64              `json:"pricing_completion_tokens" description:"Token count for the Credits-per-1M Output unit example"`
	TimePromptTokens        uint64              `json:"time_prompt_tokens" description:"Prompt tokens for execution-time examples"`
	TimeCompletionTokens    uint64              `json:"time_completion_tokens" description:"Completion tokens for execution-time examples"`
	Examples                []PricingExampleRow `json:"examples" description:"Selected per-model coefficient rows"`
}

type GetPricingExamplesResponse struct {
	response.Response
	Data *PricingExamplesData `json:"data"`
}

func GetPricingExamples(c *gin.Context) (*GetPricingExamplesResponse, error) {
	selected := selectPricingExampleModels(service.ListLoadedLLMModels())
	examples := make([]PricingExampleRow, 0, len(selected))
	for _, model := range selected {
		coefficients, err := service.GetLLMExecutionTime(c.Request.Context(), model.ModelID, model.MinVRAM)
		if err != nil {
			return nil, err
		}
		examples = append(examples, PricingExampleRow{
			Model:                 model.ModelID,
			MinVRAM:               model.MinVRAM,
			ConstantSeconds:       coefficients.ConstantSeconds,
			SecondsPerInputToken:  coefficients.SecondsPerInputToken,
			SecondsPerOutputToken: coefficients.SecondsPerOutputToken,
		})
	}

	return &GetPricingExamplesResponse{
		Data: &PricingExamplesData{
			PricingPromptTokens:     pricingPromptTokens,
			PricingCompletionTokens: pricingCompletionTokens,
			TimePromptTokens:        timePromptTokens,
			TimeCompletionTokens:    timeCompletionTokens,
			Examples:                examples,
		},
	}, nil
}

func selectPricingExampleModels(models []service.LoadedLLMModel) []service.LoadedLLMModel {
	byVRAM := map[uint64]service.LoadedLLMModel{}
	for _, model := range models {
		if model.MinVRAM == 0 {
			continue
		}
		existing, ok := byVRAM[model.MinVRAM]
		if !ok ||
			model.NodeCount > existing.NodeCount ||
			(model.NodeCount == existing.NodeCount && model.ModelID < existing.ModelID) {
			byVRAM[model.MinVRAM] = model
		}
	}

	representatives := make([]service.LoadedLLMModel, 0, len(byVRAM))
	for _, model := range byVRAM {
		representatives = append(representatives, model)
	}
	sort.Slice(representatives, func(i, j int) bool {
		return representatives[i].MinVRAM < representatives[j].MinVRAM
	})

	n := len(representatives)
	if n == 0 {
		return nil
	}
	if n <= 4 {
		return representatives
	}

	indexes := []int{
		0,
		int(math.Floor(float64(n-1) / 3)),
		int(math.Floor(2 * float64(n-1) / 3)),
		n - 1,
	}
	selected := make([]service.LoadedLLMModel, 0, 4)
	seen := map[int]struct{}{}
	for _, idx := range indexes {
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		selected = append(selected, representatives[idx])
	}
	return selected
}
