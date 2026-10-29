package provider

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PublicStreamEncoder converts semantic StreamEvents into OpenAI-compatible SSE frames.
type PublicStreamEncoder struct {
	APIType      models.TaskAPIType
	ResponseID   string
	Model        string
	Created      int64
	IncludeUsage bool

	roleSent        bool
	responsesSeq    int
	outputIndex     int
	contentIndex    int
	functionCallSeq int
}

func NewPublicStreamEncoder(apiType models.TaskAPIType, responseID, model string, created int64, includeUsage bool) *PublicStreamEncoder {
	if created == 0 {
		created = time.Now().Unix()
	}
	if responseID == "" {
		if apiType == models.TaskAPITypeResponses {
			responseID = fmt.Sprintf("resp_%d", created)
		} else {
			responseID = fmt.Sprintf("chatcmpl-%d", created)
		}
	}
	return &PublicStreamEncoder{
		APIType:      apiType,
		ResponseID:   responseID,
		Model:        model,
		Created:      created,
		IncludeUsage: includeUsage,
	}
}

// Encode returns zero or more SSE frames for one semantic event.
// stop=true means the stream should end after writing these frames (error path).
func (e *PublicStreamEncoder) Encode(event StreamEvent) (frames [][]byte, stop bool, err error) {
	if e.APIType == models.TaskAPITypeResponses {
		return e.encodeResponses(event)
	}
	return e.encodeChatCompletions(event)
}

func (e *PublicStreamEncoder) encodeChatCompletions(event StreamEvent) ([][]byte, bool, error) {
	switch event.Kind {
	case StreamEventCreated:
		return nil, false, nil
	case StreamEventContentDelta:
		var p ContentDeltaPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		var out [][]byte
		if !e.roleSent {
			frame, err := e.chatChunk(map[string]any{"role": "assistant"}, nil, nil)
			if err != nil {
				return nil, false, err
			}
			out = append(out, frame)
			e.roleSent = true
		}
		frame, err := e.chatChunk(map[string]any{"content": p.Text}, nil, nil)
		if err != nil {
			return nil, false, err
		}
		return append(out, frame), false, nil
	case StreamEventToolCallStart:
		var p ToolCallStartPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		var out [][]byte
		if !e.roleSent {
			frame, err := e.chatChunk(map[string]any{"role": "assistant"}, nil, nil)
			if err != nil {
				return nil, false, err
			}
			out = append(out, frame)
			e.roleSent = true
		}
		toolCalls := []map[string]any{{
			"index": p.Index,
			"id":    p.ID,
			"type":  "function",
			"function": map[string]any{
				"name":      p.Name,
				"arguments": "",
			},
		}}
		frame, err := e.chatChunk(map[string]any{"tool_calls": toolCalls}, nil, nil)
		if err != nil {
			return nil, false, err
		}
		return append(out, frame), false, nil
	case StreamEventToolCallArgsDelta:
		var p ToolCallArgsDeltaPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		toolCalls := []map[string]any{{
			"index": p.Index,
			"function": map[string]any{
				"arguments": p.Arguments,
			},
		}}
		frame, err := e.chatChunk(map[string]any{"tool_calls": toolCalls}, nil, nil)
		if err != nil {
			return nil, false, err
		}
		return [][]byte{frame}, false, nil
	case StreamEventToolCallCompleted:
		return nil, false, nil
	case StreamEventFinish:
		var p FinishPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		reason := p.FinishReason
		frame, err := e.chatChunk(map[string]any{}, &reason, nil)
		if err != nil {
			return nil, false, err
		}
		return [][]byte{frame}, false, nil
	case StreamEventFinalUsage:
		if !e.IncludeUsage {
			return nil, false, nil
		}
		u, err := UsageFromPayload(event.Payload)
		if err != nil {
			return nil, false, err
		}
		chunk := map[string]any{
			"id":      e.ResponseID,
			"object":  "chat.completion.chunk",
			"created": e.Created,
			"model":   e.Model,
			"choices": []any{},
			"usage":   chatUsageMap(u),
		}
		frame, err := marshalSSEData(chunk)
		if err != nil {
			return nil, false, err
		}
		return [][]byte{frame}, false, nil
	case StreamEventError:
		var p ErrorPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		body := map[string]any{
			"error": map[string]any{
				"message": p.Message,
				"type":    firstNonEmpty(p.Type, "server_error"),
				"code":    firstNonEmpty(p.Code, "provider_error"),
			},
		}
		frame, err := marshalSSEData(body)
		if err != nil {
			return nil, false, err
		}
		return [][]byte{frame}, true, nil
	default:
		return nil, false, nil
	}
}

func (e *PublicStreamEncoder) chatChunk(delta map[string]any, finishReason *string, usage map[string]any) ([]byte, error) {
	choice := map[string]any{
		"index":         0,
		"delta":         delta,
		"finish_reason": finishReason,
	}
	chunk := map[string]any{
		"id":      e.ResponseID,
		"object":  "chat.completion.chunk",
		"created": e.Created,
		"model":   e.Model,
		"choices": []map[string]any{choice},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	return marshalSSEData(chunk)
}

func (e *PublicStreamEncoder) encodeResponses(event StreamEvent) ([][]byte, bool, error) {
	switch event.Kind {
	case StreamEventCreated:
		payload := map[string]any{
			"type":     "response.created",
			"response": map[string]any{"id": e.ResponseID, "object": "response", "status": "in_progress", "model": e.Model},
		}
		return [][]byte{mustResponsesEvent("response.created", payload)}, false, nil
	case StreamEventContentDelta:
		var p ContentDeltaPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		payload := map[string]any{
			"type":         "response.output_text.delta",
			"item_id":      e.ResponseID + "_msg",
			"output_index": e.outputIndex,
			"content_index": e.contentIndex,
			"delta":        p.Text,
		}
		return [][]byte{mustResponsesEvent("response.output_text.delta", payload)}, false, nil
	case StreamEventToolCallStart:
		var p ToolCallStartPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		e.functionCallSeq++
		outIdx := responsesToolOutputIndex(p.Index)
		payload := map[string]any{
			"type":         "response.output_item.added",
			"output_index": outIdx,
			"item": map[string]any{
				"type":      "function_call",
				"id":        p.ID,
				"call_id":   p.ID,
				"name":      p.Name,
				"arguments": "",
				"status":    "in_progress",
				"index":     outIdx,
			},
		}
		return [][]byte{mustResponsesEvent("response.output_item.added", payload)}, false, nil
	case StreamEventToolCallArgsDelta:
		var p ToolCallArgsDeltaPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		payload := map[string]any{
			"type":         "response.function_call_arguments.delta",
			"output_index": responsesToolOutputIndex(p.Index),
			"delta":        p.Arguments,
		}
		return [][]byte{mustResponsesEvent("response.function_call_arguments.delta", payload)}, false, nil
	case StreamEventToolCallCompleted:
		var p ToolCallCompletedPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		payload := map[string]any{
			"type":         "response.function_call_arguments.done",
			"output_index": responsesToolOutputIndex(p.Index),
		}
		return [][]byte{mustResponsesEvent("response.function_call_arguments.done", payload)}, false, nil
	case StreamEventFinish:
		return nil, false, nil
	case StreamEventFinalUsage:
		var frames [][]byte
		completed := map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     e.ResponseID,
				"object": "response",
				"status": "completed",
				"model":  e.Model,
			},
		}
		if e.IncludeUsage {
			u, err := UsageFromPayload(event.Payload)
			if err != nil {
				return nil, false, err
			}
			completed["response"].(map[string]any)["usage"] = responsesUsageMap(u)
		}
		frames = append(frames, mustResponsesEvent("response.completed", completed))
		return frames, false, nil
	case StreamEventError:
		var p ErrorPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, false, err
		}
		payload := map[string]any{
			"type": "error",
			"error": map[string]any{
				"message": p.Message,
				"type":    firstNonEmpty(p.Type, "server_error"),
				"code":    firstNonEmpty(p.Code, "provider_error"),
			},
		}
		return [][]byte{mustResponsesEvent("error", payload)}, true, nil
	default:
		return nil, false, nil
	}
}

func marshalSSEData(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("data: ")
	b.Write(raw)
	b.WriteString("\n\n")
	return []byte(b.String()), nil
}

func mustResponsesEvent(eventName string, payload any) []byte {
	raw, _ := json.Marshal(payload)
	var b strings.Builder
	b.WriteString("event: ")
	b.WriteString(eventName)
	b.WriteString("\n")
	b.WriteString("data: ")
	b.Write(raw)
	b.WriteString("\n\n")
	return []byte(b.String())
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// RequestIncludeUsage reports whether the persisted OpenAI request asked for a public usage chunk.
func RequestIncludeUsage(requestJSON []byte, apiType models.TaskAPIType) bool {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(requestJSON, &body); err != nil {
		return false
	}
	if apiType == models.TaskAPITypeResponses {
		// Responses streams include usage on completed when the client requested stream; public include is via stream.
		// AS docs: emit usage only when stream_options.include_usage for Chat Completions.
		// For Responses there is no stream_options; include usage on completed always when IncludeUsage true from stream request is common.
		// Keep false unless explicitly present under a known field.
		return false
	}
	raw, ok := body["stream_options"]
	if !ok {
		return false
	}
	var opts struct {
		IncludeUsage bool `json:"include_usage"`
	}
	if err := json.Unmarshal(raw, &opts); err != nil {
		return false
	}
	return opts.IncludeUsage
}

// PublicStreamResponseID chooses the public id used in SSE chunks.
func PublicStreamResponseID(apiType models.TaskAPIType, jobID uint, publicID string) string {
	if apiType == models.TaskAPITypeResponses {
		if publicID != "" {
			return publicID
		}
		return "resp_" + strconv.FormatUint(uint64(jobID), 10)
	}
	return "chatcmpl-" + strconv.FormatUint(uint64(jobID), 10)
}
