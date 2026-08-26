package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ResponsesStatusQueued     = "queued"
	ResponsesStatusInProgress = "in_progress"
	ResponsesStatusCompleted  = "completed"
	ResponsesStatusFailed     = "failed"
)

var responsesUnsupportedFields = []string{
	"stream",
	"previous_response_id",
	"conversation",
	"cancel",
	"delete",
}

/* Request */

type ResponsesRequest struct {
	Model           string                   `json:"model"`
	Input           ResponsesInput           `json:"input"`
	Instructions    string                   `json:"instructions"`
	Tools           []map[string]interface{} `json:"tools"`
	Background      bool                     `json:"background"`
	MaxOutputTokens *int                     `json:"max_output_tokens"`
	Temperature     *float64                 `json:"temperature"`
	TopP            *float64                 `json:"top_p"`
	Stop            ResponsesStop            `json:"stop"`
	Seed            *int                     `json:"seed"`
	ToolChoice      any                      `json:"tool_choice"`
	VramLimit       *uint64                  `json:"vram_limit"`
}

type ResponsesInput struct {
	Text  *string
	Items []ResponsesInputItem
}

func (i *ResponsesInput) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("input is required")
	}

	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		i.Text = &text
		i.Items = nil
		return nil
	}

	var items []ResponsesInputItem
	if err := json.Unmarshal(data, &items); err == nil {
		i.Text = nil
		i.Items = items
		return nil
	}

	return fmt.Errorf("input must be a string or an array of input items")
}

type ResponsesInputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Output    string          `json:"output,omitempty"`
}

type ResponsesStop struct {
	Strings []string
}

func (s *ResponsesStop) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.Strings = []string{str}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		s.Strings = arr
		return nil
	}
	return fmt.Errorf("stop must be a string or array of strings")
}

/* Response */

type ResponsesAPIObject struct {
	ID         string                `json:"id"`
	Object     string                `json:"object"`
	CreatedAt  int64                 `json:"created_at"`
	Status     string                `json:"status"`
	Model      string                `json:"model"`
	Output     []ResponsesOutputItem `json:"output,omitempty"`
	Background bool                  `json:"background,omitempty"`
	Usage      *ResponsesUsage       `json:"usage,omitempty"`
	Error      *ResponsesAPIError    `json:"error,omitempty"`
}

type ResponsesOutputItem struct {
	Type      string                   `json:"type"`
	ID        string                   `json:"id,omitempty"`
	Role      string                   `json:"role,omitempty"`
	Status    string                   `json:"status,omitempty"`
	Content   []ResponsesContentPart   `json:"content,omitempty"`
	CallID    string                   `json:"call_id,omitempty"`
	Name      string                   `json:"name,omitempty"`
	Arguments string                   `json:"arguments,omitempty"`
}

type ResponsesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type ResponsesAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

/* Meta */

type ResponsesMeta struct {
	Model             string
	Background        bool
	MaxOutputTokens   *int
	VramLimit         *uint64
}

type ResponsesObjectParams struct {
	ID         string
	Model      string
	CreatedAt  int64
	Status     string
	Background bool
	Error      *ResponsesAPIError
}

// ParseResponsesRequest parses a Responses API request body and rejects unsupported fields.
func ParseResponsesRequest(body []byte) (ResponsesRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ResponsesRequest{}, newValidationError("", "invalid JSON body")
	}

	for _, field := range responsesUnsupportedFields {
		if value, ok := raw[field]; ok && !isJSONNull(value) {
			if field == "stream" {
				var stream bool
				if json.Unmarshal(value, &stream) == nil && stream {
					return ResponsesRequest{}, newValidationError(field, "streaming is not supported")
				}
				continue
			}
			return ResponsesRequest{}, newValidationError(field, "is not supported")
		}
	}

	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return ResponsesRequest{}, newValidationError("", "invalid JSON body")
	}

	if req.Model == "" {
		return ResponsesRequest{}, newValidationError("model", "model is required")
	}
	if req.Input.Text == nil && len(req.Input.Items) == 0 {
		return ResponsesRequest{}, newValidationError("input", "input is required")
	}

	if err := validateResponsesTools(req.Tools); err != nil {
		return ResponsesRequest{}, err
	}

	if err := validateResponsesInputItems(req.Input.Items); err != nil {
		return ResponsesRequest{}, err
	}

	return req, nil
}

func validateResponsesTools(tools []map[string]interface{}) error {
	for i, tool := range tools {
		toolType, _ := tool["type"].(string)
		if toolType != "" && toolType != "function" {
			return newValidationError("tools", fmt.Sprintf("built-in tool type %q at index %d is not supported", toolType, i))
		}
	}
	return nil
}

func validateResponsesInputItems(items []ResponsesInputItem) error {
	for i, item := range items {
		switch item.Type {
		case "message", "function_call", "function_call_output":
			continue
		default:
			return newValidationError("input", fmt.Sprintf("unsupported input item type %q at index %d", item.Type, i))
		}
	}
	return nil
}

// BuildResponsesTaskArgs converts a parsed Responses request into canonical task args JSON.
func BuildResponsesTaskArgs(req ResponsesRequest, defaultMaxTokens int) (taskArgsJSON string, err error) {
	messages, err := responsesInputToMessages(req.Instructions, req.Input)
	if err != nil {
		return "", err
	}

	var maxTokens *int
	if req.MaxOutputTokens != nil {
		maxTokens = req.MaxOutputTokens
	}

	generationConfig := buildGenerationConfig(chatGenerationParams{
		N:                   1,
		MaxTokens:           maxTokens,
		MaxCompletionTokens: maxTokens,
		DefaultMaxTokens:    defaultMaxTokens,
		TopP:                req.TopP,
		Stop:                req.Stop.Strings,
	})

	seed := 0
	if req.Seed != nil {
		seed = *req.Seed
	}

	taskArgs := models.GPTTaskArgs{
		Model:            req.Model,
		Messages:         messages,
		Tools:            req.Tools,
		GenerationConfig: generationConfig,
		Seed:             seed,
		DType:            resolveDType(req.Model),
	}

	taskMessages, err := BuildTemplateToolCallMessages(req.Model, messages)
	if err != nil {
		return "", newValidationError("input", err.Error())
	}
	taskArgsPayload := buildLLMTaskArgsPayload(taskArgs, taskMessages)
	taskArgsBytes, err := json.Marshal(taskArgsPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal task args: %w", err)
	}

	return string(taskArgsBytes), nil
}

// FormatResponsesPendingObject returns a queued or in_progress Responses object.
func FormatResponsesPendingObject(params ResponsesObjectParams) ([]byte, error) {
	obj := ResponsesAPIObject{
		ID:         params.ID,
		Object:     "response",
		CreatedAt:  params.CreatedAt,
		Status:     params.Status,
		Model:      params.Model,
		Background: params.Background,
		Output:     []ResponsesOutputItem{},
	}
	return json.Marshal(obj)
}

// FormatResponsesObject returns a completed or failed Responses object from raw GPT task output.
func FormatResponsesObject(params ResponsesObjectParams, raw *models.GPTTaskResponse) ([]byte, error) {
	obj := ResponsesAPIObject{
		ID:         params.ID,
		Object:     "response",
		CreatedAt:  params.CreatedAt,
		Status:     params.Status,
		Model:      params.Model,
		Background: params.Background,
		Error:      params.Error,
	}

	if params.Status == ResponsesStatusFailed {
		if obj.Error == nil {
			obj.Error = &ResponsesAPIError{
				Message: "task failed",
				Type:    "server_error",
			}
		}
		return json.Marshal(obj)
	}

	if raw == nil {
		return nil, fmt.Errorf("gpt task response is required for completed response")
	}

	output, err := rawResponseToOutputItems(params.ID, raw)
	if err != nil {
		return nil, err
	}
	obj.Output = output
	obj.Usage = &ResponsesUsage{
		InputTokens:  raw.Usage.PromptTokens,
		OutputTokens: raw.Usage.CompletionTokens,
		TotalTokens:  raw.Usage.TotalTokens,
	}
	return json.Marshal(obj)
}

func rawResponseToOutputItems(responseID string, raw *models.GPTTaskResponse) ([]ResponsesOutputItem, error) {
	if len(raw.Choices) == 0 {
		return []ResponsesOutputItem{}, nil
	}

	choice := raw.Choices[0]
	content := messageContentToString(choice.Message.Content)
	cleanContent, parsedToolCalls := NormalizeAssistantContent(content)

	output := make([]ResponsesOutputItem, 0, 1+len(parsedToolCalls))
	if cleanContent != "" || len(parsedToolCalls) == 0 {
		output = append(output, ResponsesOutputItem{
			Type:   "message",
			ID:     fmt.Sprintf("msg_%s", responseID),
			Role:   "assistant",
			Status: "completed",
			Content: []ResponsesContentPart{{
				Type: "output_text",
				Text: cleanContent,
			}},
		})
	}

	for i, toolCall := range parsedToolCalls {
		output = append(output, ResponsesOutputItem{
			Type:      "function_call",
			ID:        fmt.Sprintf("fc_%s_%d", responseID, i),
			CallID:    fmt.Sprintf("call_%s_%d", responseID, i),
			Name:      toolCall.Name,
			Arguments: toolCall.Arguments,
			Status:    "completed",
		})
	}

	if len(choice.Message.ToolCalls) > 0 && len(parsedToolCalls) == 0 {
		for i, toolCall := range choice.Message.ToolCalls {
			output = append(output, ResponsesOutputItem{
				Type:      "function_call",
				ID:        fmt.Sprintf("fc_%s_%d", responseID, i),
				CallID:    toolCall.Id,
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
				Status:    "completed",
			})
		}
	}

	return output, nil
}

func responsesInputToMessages(instructions string, input ResponsesInput) ([]models.Message, error) {
	messages := make([]models.Message, 0)
	if strings.TrimSpace(instructions) != "" {
		messages = append(messages, models.Message{
			Role:    models.LLMRoleSystem,
			Content: instructions,
		})
	}

	if input.Text != nil {
		messages = append(messages, models.Message{
			Role:    models.LLMRoleUser,
			Content: *input.Text,
		})
		return messages, nil
	}

	for i, item := range input.Items {
		switch item.Type {
		case "message":
			msg, err := responsesMessageItemToMessage(i, item)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		case "function_call":
			msg, err := responsesFunctionCallItemToMessage(i, item)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		case "function_call_output":
			msg, err := responsesFunctionCallOutputItemToMessage(i, item)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		default:
			return nil, newValidationError("input", fmt.Sprintf("unsupported input item type %q at index %d", item.Type, i))
		}
	}

	if len(messages) == 0 {
		return nil, newValidationError("input", "input must contain at least one message")
	}
	return messages, nil
}

func responsesMessageItemToMessage(index int, item ResponsesInputItem) (models.Message, error) {
	role := responsesRoleToLLMRole(item.Role)
	if role == "" {
		return models.Message{}, newValidationError("input", fmt.Sprintf("input[%d].role is required for message items", index))
	}

	content, err := parseResponsesMessageContent(index, item.Content)
	if err != nil {
		return models.Message{}, err
	}

	return models.Message{
		Role:    role,
		Content: content,
	}, nil
}

func responsesFunctionCallItemToMessage(index int, item ResponsesInputItem) (models.Message, error) {
	if item.CallID == "" {
		return models.Message{}, newValidationError("input", fmt.Sprintf("input[%d].call_id is required for function_call items", index))
	}
	if item.Name == "" {
		return models.Message{}, newValidationError("input", fmt.Sprintf("input[%d].name is required for function_call items", index))
	}

	return models.Message{
		Role: models.LLMRoleAssistant,
		ToolCalls: []models.ToolCall{{
			Id:   item.CallID,
			Type: "function",
			Function: models.FunctionCall{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		}},
	}, nil
}

func responsesFunctionCallOutputItemToMessage(index int, item ResponsesInputItem) (models.Message, error) {
	if item.CallID == "" {
		return models.Message{}, newValidationError("input", fmt.Sprintf("input[%d].call_id is required for function_call_output items", index))
	}

	return models.Message{
		Role:       models.LLMRoleTool,
		ToolCallID: item.CallID,
		Content:    item.Output,
	}, nil
}

func parseResponsesMessageContent(index int, raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, newValidationError("input", fmt.Sprintf("input[%d].content is required for message items", index))
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var parts []responsesContentInputPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		blocks := make([]models.MessageContentBlock, 0, len(parts))
		for partIndex, part := range parts {
			switch part.Type {
			case "input_text":
				if part.Text == "" {
					return nil, newValidationError("input", fmt.Sprintf("input[%d].content[%d].text is required", index, partIndex))
				}
				blocks = append(blocks, models.MessageContentBlock{Type: "text", Text: part.Text})
			case "input_image":
				if part.ImageURL == "" {
					return nil, newValidationError("input", fmt.Sprintf("input[%d].content[%d].image_url is required", index, partIndex))
				}
				base64Payload, err := ExtractBase64PayloadFromDataURL(part.ImageURL)
				if err != nil {
					return nil, newValidationError("input", fmt.Sprintf("input[%d].content[%d]: %v", index, partIndex, err))
				}
				blocks = append(blocks, models.MessageContentBlock{Type: "image", Base64: base64Payload})
			default:
				return nil, newValidationError("input", fmt.Sprintf("input[%d].content[%d]: unsupported type %q", index, partIndex, part.Type))
			}
		}
		if len(blocks) == 0 {
			return nil, newValidationError("input", fmt.Sprintf("input[%d].content must not be empty", index))
		}
		return blocks, nil
	}

	return nil, newValidationError("input", fmt.Sprintf("input[%d].content must be a string or content array", index))
}

type responsesContentInputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

func responsesRoleToLLMRole(role string) models.LLMRole {
	switch role {
	case "system", "developer":
		return models.LLMRoleSystem
	case "user":
		return models.LLMRoleUser
	case "assistant":
		return models.LLMRoleAssistant
	case "tool":
		return models.LLMRoleTool
	default:
		return ""
	}
}

func isJSONNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}
