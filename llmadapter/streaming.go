package llmadapter

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

const simulatedStreamChunkSize = 16

type chatCompletionChunk struct {
	Id      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []chatCompletionChunkChoice `json:"choices"`
	Usage   *CCResUsage                 `json:"usage,omitempty"`
}

type chatCompletionChunkChoice struct {
	Index        int                      `json:"index"`
	Delta        chatCompletionChunkDelta `json:"delta"`
	FinishReason *string                  `json:"finish_reason"`
}

type chatCompletionChunkDelta struct {
	Role      ChatCompletionsRole `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []streamToolCall    `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	Index    int                    `json:"index"`
	Id       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function streamToolCallFunction `json:"function"`
}

type streamToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type completionChunk struct {
	Id      string                  `json:"id"`
	Object  string                  `json:"object"`
	Created int64                   `json:"created"`
	Model   string                  `json:"model"`
	Choices []completionChunkChoice `json:"choices"`
	Usage   *CResUsage              `json:"usage,omitempty"`
}

type completionChunkChoice struct {
	Text         string  `json:"text"`
	Index        int     `json:"index"`
	LogProbs     any     `json:"logprobs"`
	FinishReason *string `json:"finish_reason"`
}

// StreamChatCompletions writes a simulated SSE stream from a formatted chat completions JSON body.
func StreamChatCompletions(c *gin.Context, responseBytes []byte, includeUsage bool) error {
	var res ChatCompletionsResponse
	if err := json.Unmarshal(responseBytes, &res); err != nil {
		return err
	}
	return streamChatCompletionsResponse(c, &res, includeUsage)
}

// StreamCompletions writes a simulated SSE stream from a formatted completions JSON body.
func StreamCompletions(c *gin.Context, responseBytes []byte, includeUsage bool) error {
	var res CompletionsResponse
	if err := json.Unmarshal(responseBytes, &res); err != nil {
		return err
	}
	return streamCompletionsResponse(c, &res, includeUsage)
}

func streamChatCompletionsResponse(c *gin.Context, res *ChatCompletionsResponse, includeUsage bool) error {
	prepareSSEHeaders(c)

	for _, choice := range res.Choices {
		role := choice.Message.Role
		if role == "" {
			role = ChatCompletionsRoleAssistant
		}
		if err := writeSSEEvent(c, chatCompletionChunk{
			Id:      res.Id,
			Object:  "chat.completion.chunk",
			Created: res.Created,
			Model:   res.Model,
			Choices: []chatCompletionChunkChoice{
				{
					Index: choice.Index,
					Delta: chatCompletionChunkDelta{
						Role: role,
					},
					FinishReason: nil,
				},
			},
		}); err != nil {
			return err
		}

		for _, textPart := range splitTextForStreaming(choice.Message.Content, simulatedStreamChunkSize) {
			if err := writeSSEEvent(c, chatCompletionChunk{
				Id:      res.Id,
				Object:  "chat.completion.chunk",
				Created: res.Created,
				Model:   res.Model,
				Choices: []chatCompletionChunkChoice{
					{
						Index: choice.Index,
						Delta: chatCompletionChunkDelta{
							Content: textPart,
						},
						FinishReason: nil,
					},
				},
			}); err != nil {
				return err
			}
		}

		if len(choice.Message.ToolCalls) > 0 {
			toolCalls := make([]streamToolCall, len(choice.Message.ToolCalls))
			for i, toolCall := range choice.Message.ToolCalls {
				toolCalls[i] = streamToolCall{
					Index: i,
					Id:    toolCall.Id,
					Type:  toolCall.Type,
					Function: streamToolCallFunction{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}

			if err := writeSSEEvent(c, chatCompletionChunk{
				Id:      res.Id,
				Object:  "chat.completion.chunk",
				Created: res.Created,
				Model:   res.Model,
				Choices: []chatCompletionChunkChoice{
					{
						Index: choice.Index,
						Delta: chatCompletionChunkDelta{
							ToolCalls: toolCalls,
						},
						FinishReason: nil,
					},
				},
			}); err != nil {
				return err
			}
		}

		finishReason := choice.FinishReason
		if err := writeSSEEvent(c, chatCompletionChunk{
			Id:      res.Id,
			Object:  "chat.completion.chunk",
			Created: res.Created,
			Model:   res.Model,
			Choices: []chatCompletionChunkChoice{
				{
					Index:        choice.Index,
					Delta:        chatCompletionChunkDelta{},
					FinishReason: &finishReason,
				},
			},
		}); err != nil {
			return err
		}
	}

	if includeUsage {
		if err := writeSSEEvent(c, chatCompletionChunk{
			Id:      res.Id,
			Object:  "chat.completion.chunk",
			Created: res.Created,
			Model:   res.Model,
			Choices: []chatCompletionChunkChoice{},
			Usage:   &res.Usage,
		}); err != nil {
			return err
		}
	}

	return writeSSEDone(c)
}

func streamCompletionsResponse(c *gin.Context, res *CompletionsResponse, includeUsage bool) error {
	prepareSSEHeaders(c)

	for _, choice := range res.Choices {
		for _, textPart := range splitTextForStreaming(choice.Text, simulatedStreamChunkSize) {
			if err := writeSSEEvent(c, completionChunk{
				Id:      res.Id,
				Object:  "text_completion",
				Created: res.Created,
				Model:   res.Model,
				Choices: []completionChunkChoice{
					{
						Text:         textPart,
						Index:        choice.Index,
						LogProbs:     nil,
						FinishReason: nil,
					},
				},
			}); err != nil {
				return err
			}
		}

		finishReason := choice.FinishReason
		if err := writeSSEEvent(c, completionChunk{
			Id:      res.Id,
			Object:  "text_completion",
			Created: res.Created,
			Model:   res.Model,
			Choices: []completionChunkChoice{
				{
					Text:         "",
					Index:        choice.Index,
					LogProbs:     nil,
					FinishReason: &finishReason,
				},
			},
		}); err != nil {
			return err
		}
	}

	if includeUsage {
		if err := writeSSEEvent(c, completionChunk{
			Id:      res.Id,
			Object:  "text_completion",
			Created: res.Created,
			Model:   res.Model,
			Choices: []completionChunkChoice{},
			Usage:   &res.Usage,
		}); err != nil {
			return err
		}
	}

	return writeSSEDone(c)
}

func prepareSSEHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

func writeSSEEvent(c *gin.Context, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", b))); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writeSSEDone(c *gin.Context) error {
	if _, err := c.Writer.Write([]byte("data: [DONE]\n\n")); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func splitTextForStreaming(text string, chunkSize int) []string {
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}

	return chunks
}
