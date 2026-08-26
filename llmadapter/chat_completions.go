package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
)

/* Request */

type ChatCompletionsRequest struct {
	Model               string                   `json:"model"`
	Messages            []CCReqMessage           `json:"messages"`
	Stream              bool                     `json:"stream"`
	MaxTokens           *int                     `json:"max_tokens"`
	MaxCompletionTokens *int                     `json:"max_completion_tokens"`
	Temperature         float64                  `json:"temperature"`
	Seed                int                      `json:"seed"`
	TopP                *float64                 `json:"top_p"`
	TopK                *int                     `json:"top_k"`
	FrequencyPenalty    *float64                 `json:"frequency_penalty"`
	PresencePenalty     *float64                 `json:"presence_penalty"`
	RepetitionPenalty   *float64                 `json:"repetition_penalty"`
	LogitBias           map[string]float64       `json:"logit_bias"`
	TopLogprobs         int                      `json:"top_logprobs"`
	MinP                *float64                 `json:"min_p"`
	TopA                *float64                 `json:"top_a"`
	Stop                []string                 `json:"stop"`
	Audio               *CCReqAudio              `json:"audio"`
	LogProbs            bool                     `json:"logprobs"`
	MetaData            map[string]string        `json:"metadata"`
	Modalities          []string                 `json:"modalities"`
	N                   int                      `json:"n"`
	Prediction          *CCReqPrediction         `json:"prediction"`
	ReasoningEffort     string                   `json:"reasoning_effort"`
	ResponseFormat      map[string]interface{}   `json:"response_format"`
	StructuredOutputs   bool                     `json:"structured_outputs"`
	ServiceTier         string                   `json:"service_tier"`
	Store               bool                     `json:"store"`
	StreamOptions       *CCReqStreamOptions      `json:"stream_options"`
	ToolChoice          any                      `json:"tool_choice"`
	Tools               []map[string]interface{} `json:"tools"`
	User                string                   `json:"user"`
	WebSearchOptions    json.RawMessage          `json:"web_search_options"`
	VramLimit           *uint64                  `json:"vram_limit"`
}

func (ccr *ChatCompletionsRequest) setDefaultValues() {
	if ccr.N == 0 {
		ccr.N = 1
	}
}

type CCReqMessage struct {
	Role       ChatCompletionsRole    `json:"role"`
	Content    *CCReqMessageContent   `json:"content"`
	Name       string                 `json:"name"`
	Audio      *CCReqMessageAudio     `json:"audio"`
	Refusal    string                 `json:"refusal"`
	ToolCalls  []CCReqMessageToolCall `json:"tool_calls"`
	ToolCallID string                 `json:"tool_call_id"`
}

type CCReqMessageContent struct {
	Text  *string
	Parts []CCReqMessageContentPart
}

func (c *CCReqMessageContent) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		c.Text = nil
		c.Parts = nil
		return nil
	}
	if len(data) == 0 {
		return fmt.Errorf("content cannot be empty")
	}

	var strContent string
	if err := json.Unmarshal(data, &strContent); err == nil {
		c.Text = &strContent
		c.Parts = nil
		return nil
	}

	var partContent []CCReqMessageContentPart
	if err := json.Unmarshal(data, &partContent); err == nil {
		c.Text = nil
		c.Parts = partContent
		return nil
	}

	return fmt.Errorf("content must be a string or an array of content parts")
}

type CCReqMessageContentPart struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	ImageURL *CCReqMessageImageURL `json:"image_url,omitempty"`
}

type CCReqMessageImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type ChatCompletionsRole string

const (
	ChatCompletionsRoleDeveloper ChatCompletionsRole = "developer"
	ChatCompletionsRoleSystem    ChatCompletionsRole = "system"
	ChatCompletionsRoleUser      ChatCompletionsRole = "user"
	ChatCompletionsRoleAssistant ChatCompletionsRole = "assistant"
	ChatCompletionsRoleTool      ChatCompletionsRole = "tool"
)

type CCReqMessageAudio struct {
	ID string `json:"id"`
}

type CCReqMessageToolCall struct {
	ID       string                       `json:"id"`
	Function CCReqMessageToolCallFunction `json:"function"`
	Type     string                       `json:"type"`
}

type CCReqMessageToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type CCReqPrediction struct {
	StaticContent StaticContent
}

type StaticContent struct {
	Content json.RawMessage `json:"content"`
	Type    string          `json:"type"`
}

type CCReqStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type CCReqAudio struct {
	Format string `json:"format"`
	Voice  string `json:"voice"`
}

/* Response */

type ChatCompletionsResponse struct {
	Id          string        `json:"id"`
	Object      string        `json:"object"`
	Created     int64         `json:"created"`
	Model       string        `json:"model"`
	Choices     []CCResChoice `json:"choices"`
	Usage       CCResUsage    `json:"usage"`
	ServiceTier string        `json:"service_tier,omitempty"`
}

type CCResChoice struct {
	Index        int          `json:"index"`
	Message      CCResMessage `json:"message"`
	LogProbs     interface{}  `json:"logprobs"`
	FinishReason string       `json:"finish_reason"`
}

type CCResMessage struct {
	Role        ChatCompletionsRole `json:"role"`
	Content     string              `json:"content"`
	Refusal     string              `json:"refusal,omitempty"`
	Annotations []interface{}       `json:"annotations,omitempty"`
	Audio       interface{}         `json:"audio,omitempty"`
	ToolCalls   []models.ToolCall   `json:"tool_calls,omitempty"`
}

type CCResUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

/* Meta */

type ChatCompletionsMeta struct {
	Model               string
	Stream              bool
	MaxTokens           *int
	MaxCompletionTokens *int
	VramLimit           *uint64
	StreamOptions       *StreamOptionsMeta
	N                   int
}

type StreamOptionsMeta struct {
	IncludeUsage bool
}

// BuildChatCompletionsTaskArgs parses a chat completions request body and returns canonical task args JSON.
func BuildChatCompletionsTaskArgs(body []byte, defaultMaxTokens int) (taskArgsJSON string, meta ChatCompletionsMeta, err error) {
	var req ChatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", ChatCompletionsMeta{}, newValidationError("", "invalid JSON body")
	}
	req.setDefaultValues()

	if req.Model == "" {
		return "", ChatCompletionsMeta{}, newValidationError("model", "model is required")
	}
	if len(req.Messages) == 0 {
		return "", ChatCompletionsMeta{}, newValidationError("messages", "messages is required")
	}

	messages := make([]models.Message, len(req.Messages))
	for i, m := range req.Messages {
		convertedMessage, convErr := ccReqMessageToMessage(m)
		if convErr != nil {
			return "", ChatCompletionsMeta{}, newValidationError("messages", fmt.Sprintf("messages[%d].content: %v", i, convErr))
		}
		messages[i] = convertedMessage
	}

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
		Tools:            req.Tools,
		GenerationConfig: generationConfig,
		Seed:             req.Seed,
		DType:            resolveDType(req.Model),
	}

	taskMessages, err := BuildTemplateToolCallMessages(req.Model, messages)
	if err != nil {
		return "", ChatCompletionsMeta{}, newValidationError("messages", err.Error())
	}
	taskArgsPayload := buildLLMTaskArgsPayload(taskArgs, taskMessages)
	taskArgsBytes, err := json.Marshal(taskArgsPayload)
	if err != nil {
		return "", ChatCompletionsMeta{}, fmt.Errorf("failed to marshal task args: %w", err)
	}

	meta = ChatCompletionsMeta{
		Model:               req.Model,
		Stream:              req.Stream,
		MaxTokens:           req.MaxTokens,
		MaxCompletionTokens: req.MaxCompletionTokens,
		VramLimit:           req.VramLimit,
		N:                   req.N,
	}
	if req.StreamOptions != nil {
		meta.StreamOptions = &StreamOptionsMeta{IncludeUsage: req.StreamOptions.IncludeUsage}
	}

	return string(taskArgsBytes), meta, nil
}

// FormatChatCompletionsResponse converts a raw GPT task response into OpenAI chat completions JSON.
func FormatChatCompletionsResponse(raw *models.GPTTaskResponse, taskID string, created int64) ([]byte, error) {
	if raw == nil {
		return nil, fmt.Errorf("gpt task response is required")
	}

	choices := make([]CCResChoice, len(raw.Choices))
	for i, choice := range raw.Choices {
		choiceMessageContent := messageContentToString(choice.Message.Content)
		cleanContent, parsedToolCalls := NormalizeAssistantContent(choiceMessageContent)

		finishReason := string(choice.FinishReason)
		toolCalls := choice.Message.ToolCalls
		if len(parsedToolCalls) > 0 {
			finishReason = string(models.FinishReasonToolCalls)
			toolCalls = make([]models.ToolCall, len(parsedToolCalls))
			for toolIdx, parsedToolCall := range parsedToolCalls {
				toolCalls[toolIdx] = models.ToolCall{
					Id:   fmt.Sprintf("call_%s_choice%d_tool%d", taskID, i, toolIdx),
					Type: "function",
					Function: models.FunctionCall{
						Name:      parsedToolCall.Name,
						Arguments: parsedToolCall.Arguments,
					},
				}
			}
		} else if finishReason == "" {
			finishReason = string(models.FinishReasonStop)
		}

		choices[i] = CCResChoice{
			Index: choice.Index,
			Message: CCResMessage{
				Role:      roleToChatCompletionsRole(choice.Message.Role),
				Content:   cleanContent,
				ToolCalls: toolCalls,
			},
			FinishReason: finishReason,
		}
	}

	response := ChatCompletionsResponse{
		Id:      taskID,
		Object:  "chat.completion",
		Created: created,
		Model:   raw.Model,
		Choices: choices,
		Usage: CCResUsage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		},
	}
	return json.Marshal(response)
}

func chatCompletionsRoleToRole(role ChatCompletionsRole) models.LLMRole {
	switch role {
	case ChatCompletionsRoleDeveloper, ChatCompletionsRoleSystem:
		return models.LLMRoleSystem
	case ChatCompletionsRoleUser:
		return models.LLMRoleUser
	case ChatCompletionsRoleAssistant:
		return models.LLMRoleAssistant
	case ChatCompletionsRoleTool:
		return models.LLMRoleTool
	default:
		return models.LLMRoleUser
	}
}

func roleToChatCompletionsRole(role models.LLMRole) ChatCompletionsRole {
	switch role {
	case models.LLMRoleSystem:
		return ChatCompletionsRoleSystem
	case models.LLMRoleUser:
		return ChatCompletionsRoleUser
	case models.LLMRoleAssistant:
		return ChatCompletionsRoleAssistant
	case models.LLMRoleTool:
		return ChatCompletionsRoleTool
	default:
		return ChatCompletionsRoleAssistant
	}
}

func ccReqMessageToMessage(ccrMessage CCReqMessage) (models.Message, error) {
	var message models.Message
	message.Role = chatCompletionsRoleToRole(ccrMessage.Role)
	content, err := convertReqContentToTaskContent(ccrMessage.Content)
	if err != nil {
		return models.Message{}, err
	}
	message.Content = content
	message.ToolCallID = ccrMessage.ToolCallID

	if len(ccrMessage.ToolCalls) > 0 {
		message.ToolCalls = make([]models.ToolCall, len(ccrMessage.ToolCalls))
		for i, reqToolCall := range ccrMessage.ToolCalls {
			message.ToolCalls[i] = models.ToolCall{
				Id:   reqToolCall.ID,
				Type: reqToolCall.Type,
				Function: models.FunctionCall{
					Name:      reqToolCall.Function.Name,
					Arguments: reqToolCall.Function.Arguments,
				},
			}
		}
	}

	return message, nil
}

func convertReqContentToTaskContent(content *CCReqMessageContent) (any, error) {
	if content == nil {
		return nil, fmt.Errorf("content is required")
	}

	if content.Text != nil {
		return *content.Text, nil
	}

	if len(content.Parts) == 0 {
		return nil, fmt.Errorf("content must be a string or a non-empty content parts array")
	}

	blocks := make([]models.MessageContentBlock, 0, len(content.Parts))
	for i, part := range content.Parts {
		switch part.Type {
		case "text":
			if part.Text == "" {
				return nil, fmt.Errorf("content part %d: text is required when type is text", i)
			}
			blocks = append(blocks, models.MessageContentBlock{
				Type: "text",
				Text: part.Text,
			})
		case "image_url":
			if part.ImageURL == nil {
				return nil, fmt.Errorf("content part %d: image_url is required when type is image_url", i)
			}
			base64Payload, err := ExtractBase64PayloadFromDataURL(part.ImageURL.URL)
			if err != nil {
				return nil, fmt.Errorf("content part %d: %w", i, err)
			}
			blocks = append(blocks, models.MessageContentBlock{
				Type:   "image",
				Base64: base64Payload,
			})
		default:
			return nil, fmt.Errorf("content part %d: unsupported type %q", i, part.Type)
		}
	}

	return blocks, nil
}
