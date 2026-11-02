package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
)

type llmTaskArgsPayload struct {
	Version            string                      `json:"version"`
	Model              string                      `json:"model"`
	Messages           any                         `json:"messages"`
	Tools              []map[string]interface{}    `json:"tools,omitempty"`
	ToolChoice         any                         `json:"tool_choice,omitempty"`
	ResponseFormat     map[string]interface{}      `json:"response_format,omitempty"`
	GenerationConfig   *models.GPTGenerationConfig `json:"generation_config,omitempty"`
	TemplateArgs       map[string]interface{}      `json:"template_args,omitempty"`
	Seed               *int                        `json:"seed,omitempty"`
	DType              models.DType                `json:"dtype,omitempty"`
	QuantizationConfig any                         `json:"quantization_config"`
}

// crynuxTaskMessages returns a copy of messages whose image blocks contain only
// the fields accepted by Crynux LLM workers.
func crynuxTaskMessages(messages []models.Message) []models.Message {
	out := make([]models.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg
		blocks, ok := msg.Content.([]models.MessageContentBlock)
		if !ok {
			continue
		}
		stripped := make([]models.MessageContentBlock, len(blocks))
		for j, block := range blocks {
			stripped[j] = models.MessageContentBlock{Type: block.Type, Text: block.Text, Base64: block.Base64}
		}
		out[i].Content = stripped
	}
	return out
}

// messageContentBlocks returns content as ordered content blocks when it is a
// block array, either typed or decoded from stored JSON.
func messageContentBlocks(content any) ([]models.MessageContentBlock, bool, error) {
	switch v := content.(type) {
	case []models.MessageContentBlock:
		return v, true, nil
	case []any:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, false, err
		}
		var blocks []models.MessageContentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil, false, err
		}
		return blocks, true, nil
	default:
		return nil, false, nil
	}
}

func buildLLMTaskArgsPayload(args models.GPTTaskArgs, messages any, seed *int) llmTaskArgsPayload {
	version := args.Version
	if version == "" {
		version = models.GPTTaskArgsVersion
	}
	return llmTaskArgsPayload{
		Version:            version,
		Model:              args.Model,
		Messages:           messages,
		Tools:              args.Tools,
		ToolChoice:         args.ToolChoice,
		ResponseFormat:     args.ResponseFormat,
		GenerationConfig:   args.GenerationConfig,
		TemplateArgs:       args.TemplateArgs,
		Seed:               seed,
		DType:              args.DType,
		QuantizationConfig: args.QuantizationConfig,
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
	if strings.HasPrefix(model, "qwen/qwen2.5") {
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

// buildTemplateArgs maps public chat_template_kwargs and reasoning_effort into
// canonical GPTTaskArgs.template_args. chat_template_kwargs is copied as-is.
// When reasoning_effort is set and enable_thinking is absent from the map,
// "none" injects enable_thinking=false and any other non-empty value injects true.
func buildTemplateArgs(chatTemplateKwargs map[string]interface{}, reasoningEffort string) map[string]interface{} {
	var result map[string]interface{}
	if len(chatTemplateKwargs) > 0 {
		result = make(map[string]interface{}, len(chatTemplateKwargs)+1)
		for k, v := range chatTemplateKwargs {
			result[k] = v
		}
	}

	if reasoningEffort != "" {
		if result == nil {
			result = make(map[string]interface{}, 1)
		}
		if _, exists := result["enable_thinking"]; !exists {
			result["enable_thinking"] = reasoningEffort != "none"
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
