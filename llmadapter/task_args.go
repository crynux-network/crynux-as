package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
)

type llmTaskArgsPayload struct {
	Model            string                      `json:"model"`
	Messages         any                         `json:"messages"`
	Tools            []map[string]interface{}    `json:"tools,omitempty"`
	GenerationConfig *models.GPTGenerationConfig `json:"generation_config,omitempty"`
	TemplateArgs     map[string]interface{}      `json:"template_args,omitempty"`
	Seed             int                         `json:"seed"`
	DType            models.DType                `json:"dtype,omitempty"`
	QuantizeBits     models.QuantizeBits         `json:"quantize_bits,omitempty"`
}

func buildLLMTaskArgsPayload(args models.GPTTaskArgs, messages any) llmTaskArgsPayload {
	return llmTaskArgsPayload{
		Model:            args.Model,
		Messages:         messages,
		Tools:            args.Tools,
		GenerationConfig: args.GenerationConfig,
		TemplateArgs:     args.TemplateArgs,
		Seed:             args.Seed,
		DType:            args.DType,
		QuantizeBits:     args.QuantizeBits,
	}
}

func resolveMaxNewTokens(maxTokens *int, maxCompletionTokens *int, defaultMaxCompletionTokens int) *int {
	if maxTokens != nil {
		return maxTokens
	}
	if maxCompletionTokens != nil {
		return maxCompletionTokens
	}
	if defaultMaxCompletionTokens > 0 {
		return &defaultMaxCompletionTokens
	}
	return nil
}

func resolveDType(model string) models.DType {
	if strings.HasPrefix(model, "Qwen/Qwen2.5") {
		return models.DTypeBFloat16
	}
	return models.DTypeAuto
}

func messageContentToString(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func buildGenerationConfig(req chatGenerationParams) *models.GPTGenerationConfig {
	generationConfig := &models.GPTGenerationConfig{
		DoSample:           false,
		Temperature:        0,
		NumReturnSequences: req.N,
	}
	maxNewTokens := resolveMaxNewTokens(req.MaxTokens, req.MaxCompletionTokens, req.DefaultMaxTokens)
	if maxNewTokens != nil {
		generationConfig.MaxNewTokens = *maxNewTokens
	}
	if req.TopP != nil {
		generationConfig.TopP = *req.TopP
	}
	if req.TopK != nil {
		generationConfig.TopK = *req.TopK
	}
	if req.MinP != nil {
		generationConfig.MinP = *req.MinP
	}
	if req.RepetitionPenalty != nil {
		generationConfig.RepetitionPenalty = *req.RepetitionPenalty
	}
	if len(req.Stop) > 0 {
		generationConfig.StopStrings = req.Stop
	}
	return generationConfig
}

type chatGenerationParams struct {
	N                   int
	MaxTokens           *int
	MaxCompletionTokens *int
	DefaultMaxTokens    int
	TopP                *float64
	TopK                *int
	MinP                *float64
	RepetitionPenalty   *float64
	Stop                []string
}
