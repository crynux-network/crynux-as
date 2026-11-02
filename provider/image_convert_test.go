package provider

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestOpenAIConvertPassesDetailAndDataURL(t *testing.T) {
	body := chatImageBody("image/png", testPNG1B64, "high", "look")
	canonical, err := ParseCanonicalRequest(body, "gpt-4o", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	native, err := convertCanonicalToOpenAIChat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(native, &out); err != nil {
		t.Fatal(err)
	}
	msg := out["messages"].([]any)[0].(map[string]any)
	parts := msg["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content=%v", msg["content"])
	}
	imagePart := parts[0].(map[string]any)
	imageURL := imagePart["image_url"].(map[string]any)
	if imagePart["type"] != "image_url" {
		t.Fatalf("type=%v", imagePart["type"])
	}
	if imageURL["url"] != llmadapter.FormatImageDataURL("image/png", testPNG1B64) {
		t.Fatalf("url=%v", imageURL["url"])
	}
	if imageURL["detail"] != "high" {
		t.Fatalf("detail=%v", imageURL["detail"])
	}
	textPart := parts[1].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "look" {
		t.Fatalf("text part=%v", textPart)
	}
}

func TestAnthropicConvertDropsDetail(t *testing.T) {
	body := chatImageBody("image/jpeg", testJPEGB64, "high", "caption")
	canonical, err := ParseCanonicalRequest(body, "claude-haiku-5-5", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	native, err := convertCanonicalToAnthropic(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(native, &out); err != nil {
		t.Fatal(err)
	}
	msg := out["messages"].([]any)[0].(map[string]any)
	parts := msg["content"].([]any)
	imagePart := parts[0].(map[string]any)
	source := imagePart["source"].(map[string]any)
	if imagePart["type"] != "image" {
		t.Fatalf("type=%v", imagePart["type"])
	}
	if _, ok := imagePart["detail"]; ok {
		t.Fatalf("detail leaked: %v", imagePart)
	}
	if source["media_type"] != "image/jpeg" || source["data"] != testJPEGB64 || source["type"] != "base64" {
		t.Fatalf("source=%v", source)
	}
	textPart := parts[1].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "caption" {
		t.Fatalf("text=%v", textPart)
	}
}

func TestGoogleConvertDropsDetail(t *testing.T) {
	body := chatImageBody("image/webp", testWebPB64, "low", "what")
	canonical, err := ParseCanonicalRequest(body, "gemini-3.8-flash", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	native, err := convertCanonicalToGoogle(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(native, &out); err != nil {
		t.Fatal(err)
	}
	content := out["contents"].([]any)[0].(map[string]any)
	parts := content["parts"].([]any)
	inline := parts[0].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "image/webp" || inline["data"] != testWebPB64 {
		t.Fatalf("inlineData=%v", inline)
	}
	if _, ok := parts[0].(map[string]any)["detail"]; ok {
		t.Fatalf("detail leaked: %v", parts[0])
	}
	if parts[1].(map[string]any)["text"] != "what" {
		t.Fatalf("text=%v", parts[1])
	}
}

func TestImageOnlyUserMessageConverts(t *testing.T) {
	body := chatImageBody("image/png", testPNG1B64, "", "")
	canonical, err := ParseCanonicalRequest(body, "gpt-4o", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	openaiBody, err := convertCanonicalToOpenAIChat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(openaiBody, &out)
	parts := out["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["type"] != "image_url" {
		t.Fatalf("openai content=%v", parts)
	}
}
