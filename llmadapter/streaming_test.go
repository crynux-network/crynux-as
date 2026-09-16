package llmadapter

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStreamChatCompletionsIncludesStructuredToolCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	response := []byte(`{
		"id":"chatcmpl_1",
		"object":"chat.completion",
		"created":1,
		"model":"model",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":"",
				"tool_calls":[{
					"id":"call_1",
					"type":"function",
					"function":{"name":"lookup","arguments":"{\"q\":\"a\"}"}
				}]
			},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`)

	if err := StreamChatCompletions(context, response, false); err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"a\"}"}}]`) {
		t.Fatalf("stream does not contain tool call: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("stream does not contain tool finish reason: %s", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("stream does not end with done event: %s", body)
	}
}
