package models

type LLMRole string

const (
	LLMRoleSystem    LLMRole = "system"
	LLMRoleUser      LLMRole = "user"
	LLMRoleAssistant LLMRole = "assistant"
	LLMRoleTool      LLMRole = "tool"
)

type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonToolCalls FinishReason = "tool_calls"
)

type DType string

const (
	DTypeFloat16  DType = "float16"
	DTypeBFloat16 DType = "bfloat16"
	DTypeFloat32  DType = "float32"
	DTypeAuto     DType = "auto"
)

type QuantizeBits int

const (
	QuantizeBits4 QuantizeBits = 4
	QuantizeBits8 QuantizeBits = 8
)

type ToolCall struct {
	Id       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role       LLMRole     `json:"role"`
	Content    any         `json:"content,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

type MessageContentBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Base64 string `json:"base64,omitempty"`
}

type GPTGenerationConfig struct {
	MaxNewTokens       int      `json:"max_new_tokens,omitempty"`
	StopStrings        []string `json:"stop_strings,omitempty"`
	DoSample           bool     `json:"do_sample,omitempty"`
	NumBeams           int      `json:"num_beams,omitempty"`
	Temperature        float64  `json:"temperature,omitempty"`
	TopK               int      `json:"top_k,omitempty"`
	TopP               float64  `json:"top_p,omitempty"`
	MinP               float64  `json:"min_p,omitempty"`
	RepetitionPenalty  float64  `json:"repetition_penalty,omitempty"`
	NumReturnSequences int      `json:"num_return_sequences,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type GPTTaskArgs struct {
	Model            string                   `json:"model"`
	Messages         []Message                `json:"messages"`
	Tools            []map[string]interface{} `json:"tools,omitempty"`
	GenerationConfig *GPTGenerationConfig     `json:"generation_config,omitempty"`
	TemplateArgs     map[string]interface{}   `json:"template_args,omitempty"`
	Seed             int                      `json:"seed"`
	DType            DType                    `json:"dtype,omitempty"`
	QuantizeBits     QuantizeBits             `json:"quantize_bits,omitempty"`
}

type ResponseChoice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
}

type GPTTaskResponse struct {
	Model   string           `json:"model"`
	Choices []ResponseChoice `json:"choices"`
	Usage   Usage            `json:"usage"`
}
