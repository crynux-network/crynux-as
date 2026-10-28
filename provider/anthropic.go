package provider

import (
	"bufio"
	"bytes"
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AnthropicAdapter struct {
	baseURL string
	client  *http.Client
	cfg     config.ProviderServiceConfig
}

func NewAnthropicAdapter(cfg config.ProviderServiceConfig) *AnthropicAdapter {
	return &AnthropicAdapter{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		cfg:     cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeoutSeconds) * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeoutSeconds) * time.Second,
			},
		},
	}
}

func (a *AnthropicAdapter) ValidateCredential(ctx context.Context, secret []byte) (CredentialValidationResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(a.cfg.CredentialValidationTimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, a.baseURL+"/v1/models", nil)
	if err != nil {
		return CredentialTemporarilyUnavailable, err
	}
	req.Header.Set("x-api-key", string(secret))
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := a.client.Do(req)
	if err != nil {
		return CredentialTemporarilyUnavailable, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusOK:
		return CredentialValid, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return CredentialInvalidAuthentication, nil
	default:
		return CredentialTemporarilyUnavailable, nil
	}
}

func (a *AnthropicAdapter) Execute(ctx context.Context, secret []byte, apiType models.TaskAPIType, model string, requestJSON []byte, tryIndex int, sink EventSink) (ExecuteResult, error) {
	canonical, err := ParseCanonicalRequest(requestJSON, model, apiType)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	native, err := convertCanonicalToAnthropic(canonical)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(native))
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: err.Error()}, nil
	}
	req.Header.Set("x-api-key", string(secret))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	if tryIndex >= 0 {
		req.Header.Set("anthropic-idempotency-key", fmt.Sprintf("crynux-as-%d", tryIndex))
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: "request cancelled or timed out after send"}, nil
		}
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close()
	upstreamID := resp.Header.Get("request-id")
	if resp.StatusCode >= 400 {
		return classifyAnthropicHTTPError(resp, upstreamID)
	}
	if canonical.Stream {
		return a.readStream(resp.Body, apiType, model, upstreamID, sink)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, UpstreamRequestID: upstreamID, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: err.Error()}, nil
	}
	msg, usage, err := parseAnthropicMessageResponse(raw)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, UpstreamRequestID: upstreamID, ErrorClass: ErrorResponseInvalid, ErrorMessage: err.Error()}, nil
	}
	publicJSON, err := BuildPublicResult(apiType, "", model, time.Now().Unix(), msg, usage)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, UpstreamRequestID: upstreamID, ErrorClass: ErrorResponseInvalid, ErrorMessage: err.Error()}, nil
	}
	return ExecuteResult{
		AcceptanceKnown:   true,
		Acceptance:        AcceptanceAcceptedBillable,
		UpstreamRequestID: upstreamID,
		FinalResultJSON:   publicJSON,
		FinalUsage:        usage,
	}, nil
}

func classifyAnthropicHTTPError(resp *http.Response, upstreamID string) (ExecuteResult, error) {
	_, _ = io.Copy(io.Discard, resp.Body)
	result := ExecuteResult{AcceptanceKnown: true, UpstreamRequestID: upstreamID}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorAuthenticationInvalid
		result.KeySpecific = true
	case http.StatusForbidden:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorPermissionDenied
		result.KeySpecific = true
	case http.StatusTooManyRequests:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorRateLimited
		result.KeySpecific = true
	default:
		if resp.StatusCode >= 500 {
			result.Acceptance = AcceptanceDefinitelyUnaccepted
			result.ErrorClass = ErrorProviderUnavailable
		} else {
			result.Acceptance = AcceptanceDefinitelyUnaccepted
			result.ErrorClass = ErrorRequestInvalid
		}
	}
	result.ErrorMessage = fmt.Sprintf("anthropic status %d", resp.StatusCode)
	return result, nil
}

func (a *AnthropicAdapter) readStream(body io.Reader, apiType models.TaskAPIType, model, upstreamID string, sink EventSink) (ExecuteResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	acc := AssembledMessage{}
	usage := &NormalizedUsage{}
	publicBegan := false
	eventName := ""
	toolIndexByBlock := map[int]int{}
	nextToolIndex := 0
	currentBlockIndex := -1
	EmitCreated(sink)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			eventName = ""
			continue
		}
		typ := eventName
		if raw, ok := payload["type"]; ok {
			_ = json.Unmarshal(raw, &typ)
		}
		switch typ {
		case "content_block_start":
			var block struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
				Text  string          `json:"text"`
			}
			index := 0
			if raw, ok := payload["index"]; ok {
				_ = json.Unmarshal(raw, &index)
			}
			currentBlockIndex = index
			if raw, ok := payload["content_block"]; ok {
				_ = json.Unmarshal(raw, &block)
			}
			if block.Type == "tool_use" {
				toolIdx := nextToolIndex
				nextToolIndex++
				toolIndexByBlock[index] = toolIdx
				args := ""
				if len(block.Input) > 0 && string(block.Input) != "{}" && string(block.Input) != "null" {
					args = string(block.Input)
				}
				acc.ToolCalls = append(acc.ToolCalls, AssembledToolCall{
					ID:        block.ID,
					Name:      block.Name,
					Arguments: args,
				})
				EmitToolCallStart(sink, toolIdx, block.ID, block.Name)
				if args != "" {
					EmitToolCallArgsDelta(sink, toolIdx, args)
				}
				publicBegan = true
			}
		case "content_block_delta":
			var delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			}
			index := currentBlockIndex
			if raw, ok := payload["index"]; ok {
				_ = json.Unmarshal(raw, &index)
			}
			if raw, ok := payload["delta"]; ok {
				_ = json.Unmarshal(raw, &delta)
			}
			switch delta.Type {
			case "text_delta":
				acc.Content += delta.Text
				EmitContentDelta(sink, delta.Text)
				publicBegan = true
			case "input_json_delta":
				toolIdx, ok := toolIndexByBlock[index]
				if !ok {
					toolIdx = len(acc.ToolCalls)
				}
				if toolIdx < len(acc.ToolCalls) {
					acc.ToolCalls[toolIdx].Arguments += delta.PartialJSON
				}
				EmitToolCallArgsDelta(sink, toolIdx, delta.PartialJSON)
				publicBegan = true
			}
		case "content_block_stop":
			index := currentBlockIndex
			if raw, ok := payload["index"]; ok {
				_ = json.Unmarshal(raw, &index)
			}
			if toolIdx, ok := toolIndexByBlock[index]; ok {
				EmitToolCallCompleted(sink, toolIdx)
			}
		case "message_delta":
			var delta struct {
				StopReason string `json:"stop_reason"`
			}
			if raw, ok := payload["delta"]; ok {
				_ = json.Unmarshal(raw, &delta)
			}
			if delta.StopReason != "" {
				acc.FinishReason = MapAnthropicStopReason(delta.StopReason)
				EmitFinish(sink, acc.FinishReason)
				publicBegan = true
			}
			var u struct {
				OutputTokens uint64 `json:"output_tokens"`
			}
			if raw, ok := payload["usage"]; ok {
				_ = json.Unmarshal(raw, &u)
				if u.OutputTokens > 0 {
					usage.OutputTokens = u.OutputTokens
				}
			}
		case "message_start":
			var message struct {
				Usage struct {
					InputTokens              uint64 `json:"input_tokens"`
					CacheReadInputTokens     uint64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens uint64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			}
			if raw, ok := payload["message"]; ok {
				_ = json.Unmarshal(raw, &message)
				usage.InputTokens = message.Usage.InputTokens
				usage.CachedInputTokens = message.Usage.CacheReadInputTokens
				usage.CacheWriteInputTokens = message.Usage.CacheCreationInputTokens
			}
		case "message_stop":
			// finalization below
		case "error":
			EmitError(sink, "anthropic stream error", "server_error", "provider_error")
			return ExecuteResult{
				AcceptanceKnown:   true,
				Acceptance:        AcceptanceUncertain,
				UpstreamRequestID: upstreamID,
				PublicOutputBegan: publicBegan,
				ErrorClass:        ErrorStreamInterrupted,
				ErrorMessage:      "anthropic stream error",
			}, nil
		}
		eventName = ""
	}
	if err := scanner.Err(); err != nil {
		EmitError(sink, err.Error(), "server_error", "stream_interrupted")
		return ExecuteResult{
			AcceptanceKnown:   true,
			Acceptance:        AcceptanceUncertain,
			UpstreamRequestID: upstreamID,
			PublicOutputBegan: publicBegan,
			ErrorClass:        ErrorStreamInterrupted,
			ErrorMessage:      err.Error(),
		}, nil
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		EmitError(sink, "missing final usage", "server_error", "usage_missing")
		return ExecuteResult{
			AcceptanceKnown:   true,
			Acceptance:        AcceptanceUncertain,
			UpstreamRequestID: upstreamID,
			PublicOutputBegan: publicBegan,
			ErrorClass:        ErrorUsageMissing,
			ErrorMessage:      "missing final usage",
		}, nil
	}
	if acc.FinishReason == "" {
		if len(acc.ToolCalls) > 0 {
			acc.FinishReason = "tool_calls"
		} else {
			acc.FinishReason = "stop"
		}
		EmitFinish(sink, acc.FinishReason)
	}
	EmitFinalUsage(sink, usage)
	publicJSON, err := BuildPublicResult(apiType, "", model, time.Now().Unix(), acc, usage)
	if err != nil {
		return ExecuteResult{
			AcceptanceKnown:   true,
			Acceptance:        AcceptanceUncertain,
			UpstreamRequestID: upstreamID,
			PublicOutputBegan: publicBegan,
			ErrorClass:        ErrorResponseInvalid,
			ErrorMessage:      err.Error(),
		}, nil
	}
	return ExecuteResult{
		AcceptanceKnown:   true,
		Acceptance:        AcceptanceAcceptedBillable,
		UpstreamRequestID: upstreamID,
		PublicOutputBegan: publicBegan,
		FinalResultJSON:   publicJSON,
		FinalUsage:        usage,
	}, nil
}
