package provider

import (
	"bufio"
	"bytes"
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GoogleAdapter struct {
	baseURL string
	client  *http.Client
	cfg     config.ProviderServiceConfig
}

func NewGoogleAdapter(cfg config.ProviderServiceConfig) *GoogleAdapter {
	return &GoogleAdapter{
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

func (a *GoogleAdapter) ValidateCredential(ctx context.Context, secret []byte) (CredentialValidationResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(a.cfg.CredentialValidationTimeoutSeconds)*time.Second)
	defer cancel()
	url := a.baseURL + "/v1beta/models?key=" + string(secret)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return CredentialTemporarilyUnavailable, err
	}
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

func (a *GoogleAdapter) Execute(ctx context.Context, secret []byte, apiType models.TaskAPIType, model string, requestJSON []byte, tryIndex int, sink EventSink) (ExecuteResult, error) {
	canonical, err := ParseCanonicalRequest(requestJSON, model, apiType)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	native, err := convertCanonicalToGoogle(canonical)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	method := "generateContent"
	if canonical.Stream {
		method = "streamGenerateContent"
	}
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:%s?key=%s", a.baseURL, url.PathEscape(model), method, url.QueryEscape(string(secret)))
	if canonical.Stream {
		endpoint += "&alt=sse"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(native))
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	_ = tryIndex
	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: "request cancelled or timed out after send"}, nil
		}
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return classifyGoogleHTTPError(resp)
	}
	if canonical.Stream {
		return a.readStream(resp.Body, apiType, model, sink)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: err.Error()}, nil
	}
	msg, usage, err := parseGoogleGenerateContentResponse(raw)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: err.Error()}, nil
	}
	if usage == nil || (usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0) {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorUsageMissing, ErrorMessage: "missing usage"}, nil
	}
	publicJSON, err := BuildPublicResult(apiType, "", model, time.Now().Unix(), msg, usage)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: err.Error()}, nil
	}
	return ExecuteResult{
		AcceptanceKnown: true,
		Acceptance:      AcceptanceAcceptedBillable,
		FinalResultJSON: publicJSON,
		FinalUsage:      usage,
	}, nil
}

func classifyGoogleHTTPError(resp *http.Response) (ExecuteResult, error) {
	_, _ = io.Copy(io.Discard, resp.Body)
	result := ExecuteResult{AcceptanceKnown: true}
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
	result.ErrorMessage = fmt.Sprintf("google status %d", resp.StatusCode)
	return result, nil
}

func (a *GoogleAdapter) readStream(body io.Reader, apiType models.TaskAPIType, model string, sink EventSink) (ExecuteResult, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	acc := AssembledMessage{}
	var usage *NormalizedUsage
	publicBegan := false
	EmittedToolStarts := map[int]bool{}
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
		msgPart, usagePart, err := parseGoogleGenerateContentResponse([]byte(data))
		if err != nil {
			continue
		}
		if usagePart != nil && (usagePart.InputTokens > 0 || usagePart.OutputTokens > 0 || usagePart.TotalTokens > 0) {
			usage = usagePart
		}
		// Stream chunks often repeat full cumulative text; emit only the new suffix.
		if msgPart.Content != "" {
			if strings.HasPrefix(msgPart.Content, acc.Content) {
				delta := msgPart.Content[len(acc.Content):]
				acc.Content = msgPart.Content
				EmitContentDelta(sink, delta)
				if delta != "" {
					publicBegan = true
				}
			} else if !strings.HasPrefix(acc.Content, msgPart.Content) {
				// Non-cumulative chunk: append.
				acc.Content += msgPart.Content
				EmitContentDelta(sink, msgPart.Content)
				publicBegan = true
			}
		}
		for _, tc := range msgPart.ToolCalls {
			idx := -1
			for i, existing := range acc.ToolCalls {
				if existing.Name == tc.Name && existing.ID == tc.ID {
					idx = i
					break
				}
				if existing.Name == tc.Name && (existing.Arguments == "" || strings.HasPrefix(tc.Arguments, existing.Arguments)) {
					idx = i
					break
				}
			}
			if idx < 0 {
				idx = len(acc.ToolCalls)
				acc.ToolCalls = append(acc.ToolCalls, tc)
				if !EmittedToolStarts[idx] {
					EmitToolCallStart(sink, idx, tc.ID, tc.Name)
					EmittedToolStarts[idx] = true
					publicBegan = true
				}
				if tc.Arguments != "" {
					EmitToolCallArgsDelta(sink, idx, tc.Arguments)
				}
			} else {
				prev := acc.ToolCalls[idx].Arguments
				if len(tc.Arguments) > len(prev) {
					delta := tc.Arguments[len(prev):]
					acc.ToolCalls[idx].Arguments = tc.Arguments
					EmitToolCallArgsDelta(sink, idx, delta)
					publicBegan = true
				}
			}
		}
		if msgPart.FinishReason != "" {
			acc.FinishReason = msgPart.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		EmitError(sink, err.Error(), "server_error", "stream_interrupted")
		return ExecuteResult{
			AcceptanceKnown:   true,
			Acceptance:        AcceptanceUncertain,
			PublicOutputBegan: publicBegan,
			ErrorClass:        ErrorStreamInterrupted,
			ErrorMessage:      err.Error(),
		}, nil
	}
	if usage == nil {
		EmitError(sink, "missing final usage", "server_error", "usage_missing")
		return ExecuteResult{
			AcceptanceKnown:   true,
			Acceptance:        AcceptanceUncertain,
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
	}
	for i := range acc.ToolCalls {
		EmitToolCallCompleted(sink, i)
	}
	EmitFinish(sink, acc.FinishReason)
	EmitFinalUsage(sink, usage)
	publicJSON, err := BuildPublicResult(apiType, "", model, time.Now().Unix(), acc, usage)
	if err != nil {
		return ExecuteResult{
			AcceptanceKnown:   true,
			Acceptance:        AcceptanceUncertain,
			PublicOutputBegan: publicBegan,
			ErrorClass:        ErrorResponseInvalid,
			ErrorMessage:      err.Error(),
		}, nil
	}
	return ExecuteResult{
		AcceptanceKnown:   true,
		Acceptance:        AcceptanceAcceptedBillable,
		PublicOutputBegan: publicBegan,
		FinalResultJSON:   publicJSON,
		FinalUsage:        usage,
	}, nil
}
