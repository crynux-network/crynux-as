package llmadapter

import (
	"testing"
)

func TestNormalizeAssistantContentHermes(t *testing.T) {
	content := "reasoninganswer<tool_call>{\"name\":\"foo\",\"arguments\":{\"x\":1}}</tool_call>"
	clean, calls := NormalizeAssistantContent(content)
	if clean != "reasoninganswer" {
		t.Fatalf("clean=%q", clean)
	}
	if len(calls) != 1 || calls[0].Name != "foo" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestNormalizeAssistantContentPreservesThinkingWhenNoToolCall(t *testing.T) {
	content := "hiddenvisible"
	clean, calls := NormalizeAssistantContent(content)
	if clean != content {
		t.Fatalf("clean=%q", clean)
	}
	if len(calls) != 0 {
		t.Fatalf("calls=%v", calls)
	}
}
