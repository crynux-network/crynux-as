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
	"net/url"
	"time"
)

func (a *GoogleAdapter) executeImages(ctx context.Context, secret []byte, apiType models.TaskAPIType, model string, requestJSON []byte, jobID uint, tryIndex int) (ExecuteResult, error) {
	_ = jobID
	_ = tryIndex
	canonical, err := ParseCanonicalImagesJSON(requestJSON, apiType)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}
	canonical.Model = model
	built, err := ValidateAndBuildGoogleImagesRequest(canonical)
	if err != nil {
		return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorRequestInvalid, ErrorMessage: err.Error()}, nil
	}

	outputs := make([]ImageOutputBytes, 0, built.N)
	var usage NormalizedUsage
	var upstreamID string
	for i := 0; i < built.N; i++ {
		one, id, errResult, err := a.callGoogleImageOnce(ctx, secret, model, built.Body)
		if err != nil {
			return ExecuteResult{}, err
		}
		if errResult != nil {
			return *errResult, nil
		}
		upstreamID = id
		encoded, format, err := ApplyGoogleOutputFormat(one.Data, built.OutputFormat, built.OutputCompress)
		if err != nil {
			return ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: err.Error(), UpstreamRequestID: upstreamID}, nil
		}
		outputs = append(outputs, ImageOutputBytes{Format: format, Data: encoded})
		usage.InputTokens += one.Usage.InputTokens
		usage.OutputTokens += one.Usage.OutputTokens
		usage.ImageInputTokens += one.Usage.ImageInputTokens
		usage.ImageOutputTokens += one.Usage.ImageOutputTokens
		usage.ReasoningTokens += one.Usage.ReasoningTokens
		usage.TotalTokens += one.Usage.TotalTokens
	}
	meta, _ := json.Marshal(map[string]any{
		"created":       time.Now().Unix(),
		"size":          canonical.EchoSize,
		"quality":       canonical.Quality,
		"output_format": outputs[0].Format,
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

type googleImageOnceResult struct {
	Data  []byte
	Usage NormalizedUsage
}

func (a *GoogleAdapter) callGoogleImageOnce(ctx context.Context, secret []byte, model string, body []byte) (googleImageOnceResult, string, *ExecuteResult, error) {
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", a.baseURL, url.PathEscape(model))
	reqCtx, cancel := withNonStreamRequestTimeout(ctx, false, a.cfg.RequestTimeoutSeconds)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: "failed to create request"}
		return googleImageOnceResult{}, "", &res, nil
	}
	req.Header.Set("x-goog-api-key", string(secret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		if reqCtx.Err() != nil {
			res := cancelledExecuteResult(reqCtx)
			return googleImageOnceResult{}, "", &res, nil
		}
		res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceDefinitelyUnaccepted, ErrorClass: ErrorConnectionFailedBeforeSend, ErrorMessage: "provider connection failed"}
		return googleImageOnceResult{}, "", &res, nil
	}
	defer resp.Body.Close()
	upstreamID := resp.Header.Get("x-goog-request-id")
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorAcceptanceUncertain, ErrorMessage: "failed to read provider response", UpstreamRequestID: upstreamID}
		return googleImageOnceResult{}, upstreamID, &res, nil
	}
	if resp.StatusCode >= 400 {
		res := classifyGoogleImagesHTTPError(resp.StatusCode, raw, upstreamID)
		return googleImageOnceResult{}, upstreamID, &res, nil
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     uint64 `json:"promptTokenCount"`
			CandidatesTokenCount uint64 `json:"candidatesTokenCount"`
			TotalTokenCount      uint64 `json:"totalTokenCount"`
			ThoughtsTokenCount   uint64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: "invalid google images response", UpstreamRequestID: upstreamID}
		return googleImageOnceResult{}, upstreamID, &res, nil
	}
	if parsed.UsageMetadata == nil {
		res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorUsageMissing, ErrorMessage: "google images usage missing", UpstreamRequestID: upstreamID}
		return googleImageOnceResult{}, upstreamID, &res, nil
	}
	var imageB64 string
	for _, cand := range parsed.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				imageB64 = part.InlineData.Data
				break
			}
		}
		if imageB64 != "" {
			break
		}
	}
	if imageB64 == "" {
		res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: "google images response has no image", UpstreamRequestID: upstreamID}
		return googleImageOnceResult{}, upstreamID, &res, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(imageB64)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(imageB64)
		if err != nil {
			res := ExecuteResult{AcceptanceKnown: true, Acceptance: AcceptanceUncertain, ErrorClass: ErrorResponseInvalid, ErrorMessage: "google image base64 is invalid", UpstreamRequestID: upstreamID}
			return googleImageOnceResult{}, upstreamID, &res, nil
		}
	}
	usage := NormalizedUsage{
		InputTokens:       parsed.UsageMetadata.PromptTokenCount,
		OutputTokens:      0,
		ImageOutputTokens: parsed.UsageMetadata.CandidatesTokenCount,
		ReasoningTokens:   parsed.UsageMetadata.ThoughtsTokenCount,
		TotalTokens:       parsed.UsageMetadata.TotalTokenCount,
	}
	return googleImageOnceResult{Data: decoded, Usage: usage}, upstreamID, nil, nil
}

func classifyGoogleImagesHTTPError(status int, body []byte, upstreamID string) ExecuteResult {
	msg := "provider request failed"
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &parsed)
	if parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	result := ExecuteResult{AcceptanceKnown: true, UpstreamRequestID: upstreamID, ErrorMessage: msg}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Acceptance = AcceptanceDefinitelyUnaccepted
		result.ErrorClass = ErrorAuthenticationInvalid
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
	return result
}
