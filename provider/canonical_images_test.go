package provider

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

const (
	testPNG1B64  = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAAEElEQVR4nGL6z8AACAAA//8DCQECWLbVUAAAAABJRU5ErkJggg=="
	testPNG32B64 = "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAALklEQVR4nOzNQQ0AAAjEMELwbxlMcL9OwDpb2Tr8BwAAAAAAAAAAAG9dAAAA///IJAFCa46k9gAAAABJRU5ErkJggg=="
	testJPEGB64  = "/9j/2wCEAAMCAgMCAgMDAwMEAwMEBQgFBQQEBQoHBwYIDAoMDAsKCwsNDhIQDQ4RDgsLEBYQERMUFRUVDA8XGBYUGBIUFRQBAwQEBQQFCQUFCRQNCw0UFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFP/AABEIAAgACAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AO7ooor+Hz/OI//Z"
	testWebPB64  = "UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAwA0JaQAA3AA/vuUAAA="
)

func chatImageBody(mime, data, detail, text string) []byte {
	imageURL := map[string]any{"url": llmadapter.FormatImageDataURL(mime, data)}
	if detail != "" {
		imageURL["detail"] = detail
	}
	content := []map[string]any{
		{"type": "image_url", "image_url": imageURL},
	}
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": content},
		},
	})
	return body
}

func TestParseCanonicalRequestPreservesOrderedImages(t *testing.T) {
	body := chatImageBody("image/png", testPNG1B64, "high", "describe")
	req, err := ParseCanonicalRequest(body, "gpt-4o", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 2 {
		t.Fatalf("parts=%+v", req.Messages[0].Parts)
	}
	if req.Messages[0].Parts[0].Type != CanonicalPartImage || req.Messages[0].Parts[0].Detail != "high" {
		t.Fatalf("image part=%+v", req.Messages[0].Parts[0])
	}
	if req.Messages[0].Parts[1].Type != CanonicalPartText || req.Messages[0].Parts[1].Text != "describe" {
		t.Fatalf("text part=%+v", req.Messages[0].Parts[1])
	}
}

func TestParseCanonicalResponsesInputImage(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"input": []map[string]any{{
			"type": "message",
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": "caption"},
				{
					"type":      "input_image",
					"image_url": llmadapter.FormatImageDataURL("image/webp", testWebPB64),
					"detail":    "low",
				},
			},
		}},
	})
	req, err := ParseCanonicalRequest(body, "gpt-4o", models.TaskAPITypeResponses, 2048)
	if err != nil {
		t.Fatal(err)
	}
	parts := req.Messages[0].Parts
	if len(parts) != 2 || parts[0].Type != CanonicalPartText || parts[1].MIMEType != "image/webp" || parts[1].Detail != "low" {
		t.Fatalf("parts=%+v", parts)
	}
}

func TestValidateCanonicalImagesAcceptsThreeMIMETypes(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "image_url", "image_url": map[string]any{"url": llmadapter.FormatImageDataURL("image/png", testPNG1B64)}},
				{"type": "image_url", "image_url": map[string]any{"url": llmadapter.FormatImageDataURL("image/jpeg", testJPEGB64)}},
				{"type": "image_url", "image_url": map[string]any{"url": llmadapter.FormatImageDataURL("image/webp", testWebPB64)}},
			},
		}},
	})
	req, err := ParseCanonicalRequest(body, "gpt-4o", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	images, err := ValidateCanonicalImages(req, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 3 {
		t.Fatalf("images=%d", len(images))
	}
}

func TestValidateCanonicalImagesRejectsHTTPAndGIFAndOverLimit(t *testing.T) {
	httpBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`)
	if _, err := ParseCanonicalRequest(httpBody, "gpt-4o", models.TaskAPITypeChatCompletions, 2048); err == nil {
		t.Fatal("expected HTTP URL rejection")
	}

	gifBody := chatImageBody("image/gif", testPNG1B64, "", "")
	req, err := ParseCanonicalRequest(gifBody, "gpt-4o", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCanonicalImages(req, 1<<20); err == nil {
		t.Fatal("expected GIF rejection")
	}

	pngReq, err := ParseCanonicalRequest(chatImageBody("image/png", testPNG32B64, "", ""), "gpt-4o", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCanonicalImages(pngReq, 10); err == nil || !strings.Contains(err.Error(), "total image size exceeds") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestValidateCanonicalImagesRejectsAssistantImages(t *testing.T) {
	req := CanonicalRequest{Messages: []CanonicalMessage{{
		Role: "assistant",
		Parts: []CanonicalContentPart{{
			Type:     CanonicalPartImage,
			MIMEType: "image/png",
			Data:     testPNG1B64,
		}},
	}}}
	if _, err := ValidateCanonicalImages(req, 1<<20); err == nil {
		t.Fatal("expected assistant image rejection")
	}
}
