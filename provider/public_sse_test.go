package provider

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicStreamEncoderChatCompletions(t *testing.T) {
	enc := NewPublicStreamEncoder(models.TaskAPITypeChatCompletions, "chatcmpl-1", "gpt-test", 100, true)
	var events []StreamEvent
	collect := func(e StreamEvent) { events = append(events, e) }

	EmitCreated(collect)
	EmitContentDelta(collect, "Hi")
	EmitToolCallStart(collect, 0, "call_1", "weather")
	EmitToolCallArgsDelta(collect, 0, `{"city":`)
	EmitToolCallArgsDelta(collect, 0, `"SF"}`)
	EmitToolCallCompleted(collect, 0)
	EmitFinish(collect, "tool_calls")
	EmitFinalUsage(collect, &NormalizedUsage{InputTokens: 3, OutputTokens: 5, TotalTokens: 8})

	var frames []string
	for _, ev := range events {
		out, stop, err := enc.Encode(ev)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if stop {
			t.Fatalf("unexpected stop on %s", ev.Kind)
		}
		for _, f := range out {
			frames = append(frames, string(f))
		}
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, `"role":"assistant"`) {
		t.Fatalf("missing role chunk: %s", joined)
	}
	if !strings.Contains(joined, `"content":"Hi"`) {
		t.Fatalf("missing content: %s", joined)
	}
	if !strings.Contains(joined, `"name":"weather"`) {
		t.Fatalf("missing tool name: %s", joined)
	}
	if !strings.Contains(joined, `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing finish: %s", joined)
	}
	if !strings.Contains(joined, `"prompt_tokens":3`) {
		t.Fatalf("missing usage: %s", joined)
	}
}

func TestPublicStreamEncoderSkipsUsageWhenNotRequested(t *testing.T) {
	enc := NewPublicStreamEncoder(models.TaskAPITypeChatCompletions, "chatcmpl-1", "m", 1, false)
	frames, stop, err := enc.Encode(StreamEvent{
		Kind:    StreamEventFinalUsage,
		Payload: mustJSON(FinalUsagePayload{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}),
	})
	if err != nil || stop {
		t.Fatalf("err=%v stop=%v", err, stop)
	}
	if len(frames) != 0 {
		t.Fatalf("expected no usage frames, got %d", len(frames))
	}
}

func TestRequestIncludeUsage(t *testing.T) {
	if RequestIncludeUsage([]byte(`{"stream":true}`), models.TaskAPITypeChatCompletions) {
		t.Fatal("expected false")
	}
	if !RequestIncludeUsage([]byte(`{"stream_options":{"include_usage":true}}`), models.TaskAPITypeChatCompletions) {
		t.Fatal("expected true")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
