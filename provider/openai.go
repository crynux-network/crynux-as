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

type OpenAIAdapter struct {
	baseURL          string
	client           *http.Client
	cfg              config.ProviderServiceConfig
	defaultMaxTokens int
}

func NewOpenAIAdapter(cfg config.ProviderServiceConfig, defaultMaxTokens int) *OpenAIAdapter {
	timeout := time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	return &OpenAIAdapter{
		baseURL:          strings.TrimRight(cfg.BaseURL, "/"),
		cfg:              cfg,
		defaultMaxTokens: defaultMaxTokens,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeoutSeconds) * time.Second,
			},
		},
	}
}

func (a *OpenAIAdapter) ValidateCredential(ctx context.Context, secret []byte) (CredentialValidationResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(a.cfg.CredentialValidationTimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, a.baseURL+"/v1/models", nil)
	if err != nil {
		return CredentialTemporarilyUnavailable, nil
	}
	req.Header.Set("Authorization", "Bearer "+string(secret))
	resp, err := a.client.Do(req)
	if err != nil {
		return CredentialTemporarilyUnavailable, nil
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

func (a *OpenAIAdapter) Execute(ctx context.Context, secret []byte, apiType models.TaskAPIType, model string, requestJSON []byte, tryIndex int, sink EventSink) (ExecuteResult, error) {
	switch apiType {
	case models.TaskAPITypeChatCompletions, models.TaskAPITypeResponses:
	default:
		return ExecuteResult{
			AcceptanceKnown: true,
			Acceptance:      AcceptanceDefinitelyUnaccepted,
			ErrorClass:      ErrorRequestInvalid,
			ErrorMessage:    "unsupported api type for openai adapter",
		}, nil
	}
	canonical, err := ParseCanonicalRequest(requestJSON, model, apiType, a.defaultMaxTokens)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	payload, err := convertCanonicalToOpenAIChat(canonical)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: "failed to create request", KeySpecific: false}, nil
	}
	req.Header.Set("Authorization", "Bearer "+string(secret))
	req.Header.Set("Content-Type", "application/json")
	if tryIndex >= 0 {
		req.Header.Set("Idempotency-Key", fmt.Sprintf("crynux-as-%d", tryIndex))
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return cancelledExecuteResult(ctx), nil
		}
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: "provider connection failed"}, nil
	}
	defer resp.Body.Close()
	upstreamID := resp.Header.Get("x-request-id")
	if resp.StatusCode >= 400 {
		return classifyOpenAIHTTPError(resp, upstreamID)
	}
	if canonical.Stream {
		return a.readChatStream(resp.Body, apiType, model, upstreamID, sink)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, UpstreamRequestID: upstreamID, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: "failed to read response"}, nil
	}
	msg, id, respModel, created, usage, err := parseOpenAIChatCompletionResponse(raw)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, UpstreamRequestID: upstreamID, FinalResultJSON: raw, ErrorClass: ErrorUsageMissing, ErrorMessage: err.Error()}, nil
	}
	publicJSON, err := BuildPublicResult(apiType, id, firstNonEmpty(respModel, model), created, msg, usage)
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

func classifyOpenAIHTTPError(resp *http.Response, upstreamID string) (ExecuteResult, error) {
	_, _ = io.Copy(io.Discard, resp.Body)
	result := ExecuteResult{
		AcceptanceKnown:   true,
		UpstreamRequestID: upstreamID,
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorAuthenticationInvalid
		result.KeySpecific = true
		result.ErrorMessage = "authentication invalid"
	case http.StatusForbidden:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorPermissionDenied
		result.KeySpecific = true
		result.ErrorMessage = "permission denied"
	case http.StatusTooManyRequests:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorRateLimited
		result.KeySpecific = true
		result.ErrorMessage = "rate limited"
	case http.StatusPaymentRequired:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorQuotaExhausted
		result.KeySpecific = true
		result.ErrorMessage = "quota exhausted"
	default:
		if resp.StatusCode >= 500 {
			result.Acceptance = AcceptanceDefinitelyUnaccepted
			result.ErrorClass = ErrorProviderUnavailable
			result.ErrorMessage = "provider unavailable"
		} else {
			result.Acceptance = AcceptanceDefinitelyUnaccepted
			result.ErrorClass = ErrorRequestInvalid
			result.ErrorMessage = fmt.Sprintf("provider rejected request with status %d", resp.StatusCode)
		}
	}
	return result, nil
}

type openAIStreamAccumulator struct {
	id      string
	model   string
	created int64
	msg     AssembledMessage
	usage   *NormalizedUsage
	// toolCallsByIndex accumulates parallel tool calls by dense tool index.
	toolCallsByIndex map[int]*AssembledToolCall
	// toolStartEmitted tracks whether ToolCallStart was already emitted per dense index.
	toolStartEmitted map[int]bool
}

func (a *openAIStreamAccumulator) ensureTool(index int) *AssembledToolCall {
	if a.toolCallsByIndex == nil {
		a.toolCallsByIndex = map[int]*AssembledToolCall{}
	}
	if tc, ok := a.toolCallsByIndex[index]; ok {
		return tc
	}
	tc := &AssembledToolCall{}
	a.toolCallsByIndex[index] = tc
	return tc
}

func (a *openAIStreamAccumulator) finalizeTools() {
	if len(a.toolCallsByIndex) == 0 {
		return
	}
	maxIdx := -1
	for i := range a.toolCallsByIndex {
		if i > maxIdx {
			maxIdx = i
		}
	}
	out := make([]AssembledToolCall, 0, maxIdx+1)
	for i := 0; i <= maxIdx; i++ {
		if tc, ok := a.toolCallsByIndex[i]; ok {
			out = append(out, *tc)
		}
	}
	a.msg.ToolCalls = out
}

func (a *OpenAIAdapter) readChatStream(body io.Reader, apiType models.TaskAPIType, model, upstreamID string, sink EventSink) (ExecuteResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	acc := &openAIStreamAccumulator{model: model, created: time.Now().Unix()}
	publicBegan := false
	EmitCreated(sink)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role      string `json:"role"`
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			acc.id = chunk.ID
		}
		if chunk.Created != 0 {
			acc.created = chunk.Created
		}
		if chunk.Model != "" {
			acc.model = chunk.Model
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			encoded, _ := json.Marshal(map[string]any{"usage": json.RawMessage(chunk.Usage)})
			if u, err := parseOpenAIUsage(encoded, models.TaskAPITypeChatCompletions); err == nil {
				acc.usage = u
			}
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				acc.msg.Content += choice.Delta.Content
				EmitContentDelta(sink, choice.Delta.Content)
				publicBegan = true
			}
			for _, tc := range choice.Delta.ToolCalls {
				slot := acc.ensureTool(tc.Index)
				if tc.ID != "" {
					slot.ID = tc.ID
				}
				if tc.Function.Name != "" {
					slot.Name = tc.Function.Name
				}
				if (tc.ID != "" || tc.Function.Name != "") && !acc.toolStartEmitted[tc.Index] {
					if acc.toolStartEmitted == nil {
						acc.toolStartEmitted = map[int]bool{}
					}
					acc.toolStartEmitted[tc.Index] = true
					EmitToolCallStart(sink, tc.Index, slot.ID, slot.Name)
					publicBegan = true
				}
				if tc.Function.Arguments != "" {
					slot.Arguments += tc.Function.Arguments
					EmitToolCallArgsDelta(sink, tc.Index, tc.Function.Arguments)
					publicBegan = true
				}
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				acc.msg.FinishReason = *choice.FinishReason
				for idx := range acc.toolCallsByIndex {
					EmitToolCallCompleted(sink, idx)
				}
				EmitFinish(sink, *choice.FinishReason)
				publicBegan = true
			}
		}
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
	acc.finalizeTools()
	if acc.usage == nil {
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
	EmitFinalUsage(sink, acc.usage)
	resultJSON, err := BuildPublicResult(apiType, acc.id, firstNonEmpty(acc.model, model), acc.created, acc.msg, acc.usage)
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
		FinalResultJSON:   resultJSON,
		FinalUsage:        acc.usage,
	}, nil
}

func parseOpenAIUsage(raw []byte, _ models.TaskAPIType) (*NormalizedUsage, error) {
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Usage) == 0 {
		return nil, fmt.Errorf("usage missing")
	}
	var u struct {
		PromptTokens            uint64 `json:"prompt_tokens"`
		CompletionTokens        uint64 `json:"completion_tokens"`
		TotalTokens             uint64 `json:"total_tokens"`
		PromptTokensDetails     *struct {
			CachedTokens uint64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails *struct {
			ReasoningTokens uint64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	}
	if err := json.Unmarshal(envelope.Usage, &u); err != nil {
		return nil, err
	}
	usage := &NormalizedUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
	}
	if u.PromptTokensDetails != nil {
		usage.CachedInputTokens = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		usage.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	return usage, nil
}
