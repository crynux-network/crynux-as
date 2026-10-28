package provider

import "encoding/json"

// Semantic event payloads. Adapters marshal these into StreamEvent.Payload.

type CreatedPayload struct{}

type ContentDeltaPayload struct {
	Text string `json:"text"`
}

type ToolCallStartPayload struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}

type ToolCallArgsDeltaPayload struct {
	Index     int    `json:"index"`
	Arguments string `json:"arguments"`
}

type ToolCallCompletedPayload struct {
	Index int `json:"index"`
}

type FinishPayload struct {
	FinishReason string `json:"finish_reason"`
}

type FinalUsagePayload struct {
	InputTokens           uint64 `json:"input_tokens"`
	OutputTokens          uint64 `json:"output_tokens"`
	TotalTokens           uint64 `json:"total_tokens"`
	CachedInputTokens     uint64 `json:"cached_input_tokens,omitempty"`
	CacheWriteInputTokens uint64 `json:"cache_write_input_tokens,omitempty"`
	ReasoningTokens       uint64 `json:"reasoning_tokens,omitempty"`
	ToolUsePromptTokens   uint64 `json:"tool_use_prompt_tokens,omitempty"`
}

type ErrorPayload struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

func emit(sink EventSink, kind StreamEventKind, payload any) {
	if sink == nil {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	sink(StreamEvent{Kind: kind, Payload: raw})
}

func EmitCreated(sink EventSink) {
	emit(sink, StreamEventCreated, CreatedPayload{})
}

func EmitContentDelta(sink EventSink, text string) {
	if text == "" {
		return
	}
	emit(sink, StreamEventContentDelta, ContentDeltaPayload{Text: text})
}

func EmitToolCallStart(sink EventSink, index int, id, name string) {
	emit(sink, StreamEventToolCallStart, ToolCallStartPayload{Index: index, ID: id, Name: name})
}

func EmitToolCallArgsDelta(sink EventSink, index int, args string) {
	if args == "" {
		return
	}
	emit(sink, StreamEventToolCallArgsDelta, ToolCallArgsDeltaPayload{Index: index, Arguments: args})
}

func EmitToolCallCompleted(sink EventSink, index int) {
	emit(sink, StreamEventToolCallCompleted, ToolCallCompletedPayload{Index: index})
}

func EmitFinish(sink EventSink, reason string) {
	emit(sink, StreamEventFinish, FinishPayload{FinishReason: reason})
}

func EmitFinalUsage(sink EventSink, u *NormalizedUsage) {
	if u == nil {
		return
	}
	emit(sink, StreamEventFinalUsage, FinalUsagePayload{
		InputTokens:           u.InputTokens,
		OutputTokens:          u.OutputTokens,
		TotalTokens:           u.TotalTokens,
		CachedInputTokens:     u.CachedInputTokens,
		CacheWriteInputTokens: u.CacheWriteInputTokens,
		ReasoningTokens:       u.ReasoningTokens,
		ToolUsePromptTokens:   u.ToolUsePromptTokens,
	})
}

func EmitError(sink EventSink, message, typ, code string) {
	emit(sink, StreamEventError, ErrorPayload{Message: message, Type: typ, Code: code})
}

func UsageFromPayload(raw []byte) (*NormalizedUsage, error) {
	var p FinalUsagePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &NormalizedUsage{
		InputTokens:           p.InputTokens,
		OutputTokens:          p.OutputTokens,
		TotalTokens:           p.TotalTokens,
		CachedInputTokens:     p.CachedInputTokens,
		CacheWriteInputTokens: p.CacheWriteInputTokens,
		ReasoningTokens:       p.ReasoningTokens,
		ToolUsePromptTokens:   p.ToolUsePromptTokens,
	}, nil
}
