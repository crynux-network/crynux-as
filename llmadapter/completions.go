package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
)

/* Request */

type CompletionsRequest struct {
	Model               string             `json:"model"`
	Prompt              string             `json:"prompt"`
	Stream              bool               `json:"stream"`
	MaxTokens           *int               `json:"max_tokens"`
	MaxCompletionTokens *int               `json:"max_completion_tokens"`
	Temperature         float64            `json:"temperature"`
	Seed                int                `json:"seed"`
	TopP                *float64           `json:"top_p"`
	TopK                *int               `json:"top_k"`
	FrequencyPenalty    *float64           `json:"frequency_penalty"`
	PresencePenalty     *float64           `json:"presence_penalty"`
	RepetitionPenalty   *float64           `json:"repetition_penalty"`
	LogitBias           map[string]float64 `json:"logit_bias"`
	TopLogprobs         int                `json:"top_logprobs"`
	MinP                *float64           `json:"min_p"`
	TopA                *float64           `json:"top_a"`
	Stop                []string           `json:"stop"`
	BestOf              int                `json:"best_of"`
	Echo                bool               `json:"echo"`
	LogProbs            int                `json:"logprobs"`
	N                   int                `json:"n"`
	StreamOptions       CReqStreamOptions  `json:"stream_options"`
	Suffix              string             `json:"suffix"`
	User                string             `json:"user"`
	VramLimit           *uint64            `json:"vram_limit"`
}

func (cr *CompletionsRequest) setDefaultValues() {
	if cr.N == 0 {
		cr.N = 1
	}
	if cr.BestOf == 0 {
		cr.BestOf = 1
	}
}

type CReqStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

/* Response */

type CompletionsResponse struct {
	Id                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
	Choices           []CResChoice `json:"choices"`
	Usage             CResUsage    `json:"usage"`
}

type CResChoice struct {
	Text         string `json:"text"`
	Index        int    `json:"index"`
	LogProbs     string `json:"logprobs"`
	FinishReason string `json:"finish_reason"`
}

type CResUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

/* Meta */

type CompletionsMeta struct {
	Model               string
	Stream              bool
	MaxTokens           *int
	MaxCompletionTokens *int
	VramLimit           *uint64
	StreamOptions       *StreamOptionsMeta
	N                   int
}

// BuildCompletionsTaskArgs parses a completions request body and returns canonical task args JSON.
func BuildCompletionsTaskArgs(body []byte, defaultMaxTokens int) (taskArgsJSON string, meta CompletionsMeta, err error) {
	var req CompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", CompletionsMeta{}, newValidationError("", "invalid JSON body")
	}
	req.setDefaultValues()

	if req.Model == "" {
		return "", CompletionsMeta{}, newValidationError("model", "model is required")
	}
	if req.Prompt == "" {
		return "", CompletionsMeta{}, newValidationError("prompt", "prompt is required")
	}

	messages := []models.Message{{
		Role:    models.LLMRoleUser,
		Content: req.Prompt,
	}}

	generationConfig := buildGenerationConfig(chatGenerationParams{
		N:                   req.N,
		MaxTokens:           req.MaxTokens,
		MaxCompletionTokens: req.MaxCompletionTokens,
		DefaultMaxTokens:    defaultMaxTokens,
		TopP:                req.TopP,
		TopK:                req.TopK,
		MinP:                req.MinP,
		RepetitionPenalty:   req.RepetitionPenalty,
		Stop:                req.Stop,
	})

	taskArgs := models.GPTTaskArgs{
		Model:            req.Model,
		Messages:         messages,
		GenerationConfig: generationConfig,
		Seed:             req.Seed,
		DType:            resolveDType(req.Model),
	}

	taskArgsBytes, err := json.Marshal(taskArgs)
	if err != nil {
		return "", CompletionsMeta{}, fmt.Errorf("failed to marshal task args: %w", err)
	}

	meta = CompletionsMeta{
		Model:               req.Model,
		Stream:              req.Stream,
		MaxTokens:           req.MaxTokens,
		MaxCompletionTokens: req.MaxCompletionTokens,
		VramLimit:           req.VramLimit,
		N:                   req.N,
	}
	if req.Stream {
		meta.StreamOptions = &StreamOptionsMeta{IncludeUsage: req.StreamOptions.IncludeUsage}
	}

	return string(taskArgsBytes), meta, nil
}

// FormatCompletionsResponse converts a raw GPT task response into OpenAI completions JSON.
func FormatCompletionsResponse(raw *models.GPTTaskResponse, taskID string, created int64) ([]byte, error) {
	if raw == nil {
		return nil, fmt.Errorf("gpt task response is required")
	}

	choices := make([]CResChoice, len(raw.Choices))
	for i, choice := range raw.Choices {
		text := stripThinkingContent(messageContentToString(choice.Message.Content))
		finishReason := string(choice.FinishReason)
		if finishReason == "" {
			finishReason = string(models.FinishReasonStop)
		}
		choices[i] = CResChoice{
			Text:         text,
			Index:        choice.Index,
			FinishReason: finishReason,
		}
	}

	response := CompletionsResponse{
		Id:      taskID,
		Object:  "text_completion",
		Created: created,
		Model:   raw.Model,
		Choices: choices,
		Usage: CResUsage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		},
	}
	return json.Marshal(response)
}
