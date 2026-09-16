package llmadapter

import (
	"crynux_as/models"
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

func TestNormalizeAssistantContentForSupportedStructuralTags(t *testing.T) {
	args := &models.GPTTaskArgs{
		Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name": "weather",
			},
		}},
		ToolChoice: "required",
	}
	tests := map[string]string{
		"llama":         `{"name":"weather","parameters":{"city":"Paris"}}`,
		"kimi":          `<|tool_calls_section_begin|><|tool_call_begin|>functions.weather:0<|tool_call_argument_begin|>{"city":"Paris"}<|tool_call_end|><|tool_calls_section_end|>`,
		"deepseek_r1":   "<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>function<｜tool▁sep｜>weather\n```json\n{\"city\":\"Paris\"}\n```<｜tool▁call▁end｜><｜tool▁calls▁end｜>",
		"deepseek_v3_1": `<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>weather<｜tool▁sep｜>{"city":"Paris"}<｜tool▁call▁end｜><｜tool▁calls▁end｜>`,
		"deepseek_v3_2": `<｜DSML｜function_calls><｜DSML｜invoke name="weather"><｜DSML｜parameter name="city" string="true">Paris</｜DSML｜parameter></｜DSML｜invoke></｜DSML｜function_calls>`,
		"deepseek_v4":   `<｜DSML｜tool_calls><｜DSML｜invoke name="weather"><｜DSML｜parameter name="city" string="true">Paris</｜DSML｜parameter></｜DSML｜invoke></｜DSML｜tool_calls>`,
		"qwen_3":        `<tool_call>{"name":"weather","arguments":{"city":"Paris"}}</tool_call>`,
		"qwen_3_coder":  `<tool_call><function=weather><parameter=city>Paris</parameter></function></tool_call>`,
		"qwen_3_5":      `<tool_call><function=weather><parameter=city>Paris</parameter></function></tool_call>`,
		"glm_4_7":       `<tool_call>weather<arg_key>city</arg_key><arg_value>Paris</arg_value></tool_call>`,
		"hermes":        `<tool_call>{"name":"weather","arguments":{"city":"Paris"}}</tool_call>`,
		"hy_v4":         `<tool_calls:abc><tool_call:abc>weather<arg_key:abc>city</arg_key:abc><arg_value:abc>Paris</arg_value:abc></tool_call:abc></tool_calls:abc>`,
		"kimi_k3":       `<|open|>response<|sep|><|close|>response<|sep|><|open|>tools<|sep|><|open|>call tool="weather" index="1"<|sep|><|open|>argument key="city" type="string"<|sep|>Paris<|close|>argument<|sep|><|close|>call<|sep|><|close|>tools<|sep|><|close|>message<|sep|>`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			_, calls := NormalizeAssistantContentForTask(content, args)
			if len(calls) != 1 || calls[0].Name != "weather" {
				t.Fatalf("calls=%v", calls)
			}
		})
	}
}

func TestNormalizeAssistantContentHonorsNoneAndNamedJSON(t *testing.T) {
	tools := []map[string]interface{}{{
		"type":     "function",
		"function": map[string]interface{}{"name": "weather"},
	}}
	content := `<tool_call>{"name":"weather","arguments":{"city":"Paris"}}</tool_call>`
	clean, calls := NormalizeAssistantContentForTask(content, &models.GPTTaskArgs{
		Tools:      tools,
		ToolChoice: "none",
	})
	if clean != content || len(calls) != 0 {
		t.Fatalf("none returned clean=%q calls=%v", clean, calls)
	}

	clean, calls = NormalizeAssistantContentForTask(`{"city":"Paris"}`, &models.GPTTaskArgs{
		Tools: tools,
		ToolChoice: map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": "weather"},
		},
	})
	if clean != "" || len(calls) != 1 || calls[0].Name != "weather" {
		t.Fatalf("named returned clean=%q calls=%v", clean, calls)
	}

	structured := `{"name":"weather","parameters":{"city":"Paris"}}`
	clean, calls = NormalizeAssistantContentForTask(structured, &models.GPTTaskArgs{
		Tools:          tools,
		ToolChoice:     "auto",
		ResponseFormat: map[string]interface{}{"type": "json_object"},
	})
	if clean != structured || len(calls) != 0 {
		t.Fatalf("structured output returned clean=%q calls=%v", clean, calls)
	}
}
