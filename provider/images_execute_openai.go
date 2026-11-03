package provider

import (
	"bytes"
	"context"
	"crynux_as/models"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (a *OpenAIAdapter) executeImages(ctx context.Context, secret []byte, apiType models.TaskAPIType, model string, requestJSON []byte, jobID uint, tryIndex int) (ExecuteResult, error) {
	canonical, err := ParseCanonicalImagesJSON(requestJSON, apiType)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	canonical.Model = model
	payload, err := ValidateAndBuildOpenAIImagesRequest(canonical)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	path := "/v1/images/generations"
	if apiType == models.TaskAPITypeImagesEdits {
		path = "/v1/images/edits"
	}
	reqCtx, cancel := withNonStreamRequestTimeout(ctx, false, a.cfg.RequestTimeoutSeconds)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: "failed to create request"}, nil
	}
	req.Header.Set("Authorization", "Bearer "+string(secret))
	req.Header.Set("Content-Type", "application/json")
	if tryIndex >= 0 {
		req.Header.Set("Idempotency-Key", ProviderIdempotencyKey(jobID, tryIndex))
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if reqCtx.Err() != nil {
			return cancelledExecuteResult(reqCtx), nil
		}
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: "provider connection failed"}, nil
	}
	defer resp.Body.Close()
	upstreamID := resp.Header.Get("x-request-id")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: "failed to read provider response", UpstreamRequestID: upstreamID}, nil
	}
	if resp.StatusCode >= 400 {
		return classifyOpenAIHTTPErrorBytes(resp.StatusCode, body, upstreamID)
	}
	var parsed struct {
		Created int64 `json:"created"`
		Data    []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
		Usage *struct {
			InputTokens        uint64 `json:"input_tokens"`
			OutputTokens       uint64 `json:"output_tokens"`
			TotalTokens        uint64 `json:"total_tokens"`
			InputTokensDetails *struct {
				TextTokens  uint64 `json:"text_tokens"`
				ImageTokens uint64 `json:"image_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails *struct {
				TextTokens  uint64 `json:"text_tokens"`
				ImageTokens uint64 `json:"image_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: "invalid images response", UpstreamRequestID: upstreamID}, nil
	}
	if parsed.Usage == nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorUsageMissing, ErrorMessage: "images usage missing", UpstreamRequestID: upstreamID}, nil
	}
	if len(parsed.Data) == 0 {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: "images response has no data", UpstreamRequestID: upstreamID}, nil
	}
	format := strings.TrimSpace(canonical.OutputFormat)
	if format == "" {
		format = "png"
	}
	outputs := make([]ImageOutputBytes, 0, len(parsed.Data))
	for i, item := range parsed.Data {
		raw, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(item.B64JSON)
			if err != nil {
				return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: fmt.Sprintf("image %d base64 is invalid", i), UpstreamRequestID: upstreamID}, nil
			}
		}
		outputs = append(outputs, ImageOutputBytes{Format: format, Data: raw})
	}
	// Images API bills image output tokens. Aggregate output_tokens is the
	// image count when output_tokens_details is absent. Text output tokens are
	// not billed separately because OpenAI does not publish an Images text
	// output price.
	usage := NormalizedUsage{
		InputTokens:       parsed.Usage.InputTokens,
		OutputTokens:      0,
		ImageOutputTokens: parsed.Usage.OutputTokens,
		TotalTokens:       parsed.Usage.TotalTokens,
	}
	if parsed.Usage.InputTokensDetails != nil {
		usage.InputTokens = parsed.Usage.InputTokensDetails.TextTokens
		usage.ImageInputTokens = parsed.Usage.InputTokensDetails.ImageTokens
		if usage.InputTokens+usage.ImageInputTokens == 0 {
			usage.InputTokens = parsed.Usage.InputTokens
		}
	}
	if parsed.Usage.OutputTokensDetails != nil {
		usage.ImageOutputTokens = parsed.Usage.OutputTokensDetails.ImageTokens
	}
	meta, _ := json.Marshal(map[string]any{
		"created":       parsed.Created,
		"size":          canonical.EchoSize,
		"quality":       canonical.Quality,
		"output_format": format,
		"n":             len(outputs),
	})
	return ExecuteResult{
		AcceptanceKnown:   true,
		Acceptance:        AcceptanceAcceptedBillable,
		UpstreamRequestID: upstreamID,
		FinalResultJSON:   meta,
		FinalUsage:        &usage,
		ImageOutputs:      outputs,
	}, nil
}

func classifyOpenAIHTTPErrorBytes(status int, body []byte, upstreamID string) (ExecuteResult, error) {
	msg := "provider request failed"
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &parsed)
	if parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	result := ExecuteResult{
		AcceptanceKnown:   true,
		UpstreamRequestID: upstreamID,
		ErrorMessage:      msg,
	}
	switch status {
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
	case http.StatusBadRequest:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorRequestInvalid
	default:
		if status >= 500 {
			result.Acceptance = AcceptanceUncertain
			result.ErrorClass = ErrorProviderUnavailable
		} else {
			result.Acceptance = AcceptanceDefinitelyUnaccepted
			result.ErrorClass = ErrorRequestInvalid
		}
	}
	return result, nil
}
