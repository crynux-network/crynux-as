package provider

import (
	"context"
	"crynux_as/models"
)

type ErrorClass string

const (
	ErrorRequestInvalid           ErrorClass = "request_invalid"
	ErrorAuthenticationInvalid    ErrorClass = "authentication_invalid"
	ErrorPermissionDenied         ErrorClass = "permission_denied"
	ErrorRateLimited              ErrorClass = "rate_limited"
	ErrorQuotaExhausted           ErrorClass = "quota_exhausted"
	ErrorProviderUnavailable      ErrorClass = "provider_unavailable"
	ErrorConnectionFailedBeforeSend ErrorClass = "connection_failed_before_send"
	ErrorAcceptanceUncertain      ErrorClass = "acceptance_uncertain"
	ErrorStreamInterrupted        ErrorClass = "stream_interrupted"
	ErrorResponseInvalid          ErrorClass = "response_invalid"
	ErrorUsageMissing             ErrorClass = "usage_missing"
	ErrorCancelledByShutdown      ErrorClass = "cancelled_by_shutdown"
)

type AcceptanceClass string

const (
	AcceptanceDefinitelyUnaccepted AcceptanceClass = "definitely_unaccepted"
	AcceptanceAcceptedBillable     AcceptanceClass = "accepted_billable"
	AcceptanceUncertain            AcceptanceClass = "uncertain"
)

type CredentialValidationResult string

const (
	CredentialValid                 CredentialValidationResult = "valid"
	CredentialInvalidAuthentication CredentialValidationResult = "invalid_authentication"
	CredentialTemporarilyUnavailable CredentialValidationResult = "temporarily_unavailable"
)

type NormalizedUsage struct {
	InputTokens           uint64 `json:"input_tokens"`
	OutputTokens          uint64 `json:"output_tokens"`
	CachedInputTokens     uint64 `json:"cached_input_tokens"`
	CacheWriteInputTokens uint64 `json:"cache_write_input_tokens"`
	ReasoningTokens       uint64 `json:"reasoning_tokens"`
	ToolUsePromptTokens   uint64 `json:"tool_use_prompt_tokens"`
	TotalTokens           uint64 `json:"total_tokens"`
}

type StreamEventKind string

const (
	StreamEventCreated           StreamEventKind = "created"
	StreamEventContentDelta      StreamEventKind = "content_delta"
	StreamEventToolCallStart     StreamEventKind = "tool_call_start"
	StreamEventToolCallArgsDelta StreamEventKind = "tool_call_args_delta"
	StreamEventToolCallCompleted StreamEventKind = "tool_call_completed"
	StreamEventFinish            StreamEventKind = "finish"
	StreamEventFinalUsage        StreamEventKind = "final_usage"
	StreamEventError             StreamEventKind = "error"
)

type StreamEvent struct {
	Kind    StreamEventKind
	Payload []byte
}

type EventSink func(event StreamEvent)

type ExecuteResult struct {
	AcceptanceKnown   bool
	Acceptance        AcceptanceClass
	UpstreamRequestID string
	PublicOutputBegan bool
	FinalResultJSON   []byte
	FinalUsage        *NormalizedUsage
	PartialUsage      *NormalizedUsage
	ErrorClass        ErrorClass
	ErrorMessage      string
	KeySpecific       bool
}

func (r ExecuteResult) HasConfirmedUsage() bool {
	return r.FinalUsage != nil && r.Acceptance == AcceptanceAcceptedBillable
}

type Adapter interface {
	ValidateCredential(ctx context.Context, secret []byte) (CredentialValidationResult, error)
	Execute(ctx context.Context, secret []byte, apiType models.TaskAPIType, model string, requestJSON []byte, tryIndex int, sink EventSink) (ExecuteResult, error)
}
