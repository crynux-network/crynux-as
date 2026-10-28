# Service Provider Adapters

## Responsibility

The `provider/` package MUST isolate Service Provider transport and protocol differences.

An adapter owns:

* credential validation;
* request conversion from validated AS semantics;
* upstream authentication;
* HTTP and stream parsing;
* normalized public events;
* final result assembly;
* normalized usage;
* error classification;
* upstream request identity;
* rate-limit and quota metadata.

An adapter MUST NOT:

* authenticate an AS project;
* choose a Shared API Key;
* reserve or charge Credits;
* create earnings;
* change job state directly;
* expose raw Provider errors to clients;
* add models from an upstream model list;
* switch to another Key by itself.

## Common Interface

The internal interface MUST provide operations equivalent to:

```text
ValidateCredential(ctx, secret) -> validation result
BuildRequest(job, persisted request) -> provider request
Execute(ctx, secret, request, event sink) -> final result
ClassifyError(error or response) -> normalized error
```

`Execute` MUST return:

* whether request acceptance is known;
* upstream request ID when available;
* whether public output began;
* normalized final result when confirmed usage is present;
* normalized final usage when available;
* partial usage fragments;
* rate-limit and quota metadata;
* redacted error class and message.

When `Execute` reports confirmed final usage, it MUST also return a valid normalized final result. Safety stops, max-token truncation, empty assistant content, and tool-only output MUST be represented as a valid public result. `Execute` MUST NOT return confirmed usage without a normalized final result.

The event sink MUST be non-blocking from the adapter's perspective.

## Credential Validation

Credential validation MUST:

* use a non-billable authenticated endpoint;
* use a bounded adapter timeout;
* distinguish invalid authentication from temporary unavailability;
* avoid logging request headers or raw response bodies;
* ignore returned model membership for AS catalog purposes.

The result MUST be one of:

* valid;
* invalid authentication;
* temporarily unavailable.

Only valid credentials may be persisted.

## Common Usage Model

Normalized usage MUST be versioned and contain non-negative integer components.

Common fields:

* `input_tokens`;
* `output_tokens`;
* `cached_input_tokens`;
* `cache_write_input_tokens`;
* `reasoning_tokens`;
* `tool_use_prompt_tokens`;
* `total_tokens`.

Each adapter MUST document whether detailed fields are subsets of input or output totals. Billing normalization MUST avoid adding both a total and its included subset.

The adapter MUST preserve raw numeric usage fields in a redacted normalized JSON object, not the complete upstream response.

## Error Classes

Every adapter MUST classify failures into:

* `request_invalid`;
* `authentication_invalid`;
* `permission_denied`;
* `rate_limited`;
* `quota_exhausted`;
* `provider_unavailable`;
* `connection_failed_before_send`;
* `acceptance_uncertain`;
* `stream_interrupted`;
* `response_invalid`;
* `usage_missing`;
* `cancelled_by_shutdown`.

Classification MUST state:

* definitely unaccepted;
* accepted and billable;
* uncertain acceptance.

A `response_invalid` result MUST NOT include confirmed final usage. If usage can be verified, the adapter MUST assemble a valid public result.

Only definitely unaccepted key-specific failures may be considered for an automatic in-worker Key switch.

## OpenAI Adapter

The OpenAI adapter MUST support the corresponding OpenAI endpoints:

* Chat Completions;
* Responses.

It MUST:

* send the configured model name unchanged;
* use Bearer authentication;
* preserve validated OpenAI request semantics;
* request final stream usage even when the API Consumer did not request a public usage chunk;
* suppress the internal usage request option from public output when the client did not request it;
* parse Chat Completions SSE and Responses events incrementally;
* assemble one final normalized result;
* capture upstream request ID from headers or response fields when available.

Normalized usage MUST map:

* Chat Completions `prompt_tokens` to input;
* Chat Completions `completion_tokens` to output;
* Responses `input_tokens` to input;
* Responses `output_tokens` to output;
* cached token details to cached input;
* reasoning token details to reasoning.

Detail fields included in parent totals MUST replace the corresponding portion for pricing rather than being added on top of the parent total.

## Anthropic Adapter

The Anthropic adapter MUST translate validated Chat Completions and Responses semantics to the Anthropic Messages contract.

It MUST:

* construct system and message content without exposing AS-only fields;
* convert function tools and tool choices;
* convert tool-use and tool-result history;
* translate content-block stream events into the selected OpenAI-compatible stream;
* assemble text and tool-call output;
* preserve stop reason semantics through the AS public mapping;
* capture the Anthropic request ID when available.

Normalized usage MUST map:

* `input_tokens` to ordinary input;
* `output_tokens` to output;
* `cache_read_input_tokens` to cached input;
* `cache_creation_input_tokens` to cache-write input.

Cache token fields MUST not be added twice when the parent input field includes them.

## Google Adapter

The Google adapter MUST translate validated Chat Completions and Responses semantics to the configured Google generative-language API.

It MUST:

* send the configured model name through the provider's required model path or field without changing the public name;
* convert system instructions, messages, tools, tool choices, and tool results;
* parse streaming candidate events incrementally;
* assemble one final normalized result;
* convert safety or policy termination into a normalized public outcome;
* capture request identity when available.

Normalized usage MUST map:

* `promptTokenCount` to input;
* `candidatesTokenCount` to output;
* `cachedContentTokenCount` to cached input;
* `thoughtsTokenCount` to reasoning;
* `toolUsePromptTokenCount` to tool-use prompt;
* `totalTokenCount` to total.

The adapter MUST treat detail fields as subsets when the provider reports them within parent totals.

## Streaming Conversion

Every adapter MUST emit semantic events rather than raw upstream bytes:

* response created;
* content delta;
* tool-call start;
* tool-call arguments delta;
* tool-call completed;
* finish reason;
* final usage;
* normalized error.

The API layer converts semantic events into Chat Completions or Responses SSE.

Adapters MUST:

* preserve event order;
* retain stable tool-call indexes;
* reject malformed sequence transitions;
* continue reading upstream after downstream detaches;
* avoid unbounded buffering;
* report whether any public event was emitted.

## Timeouts

Each Service Provider configuration MUST define:

* connection timeout;
* response-header timeout;
* idle stream timeout;
* total non-streaming request timeout;
* credential-validation timeout.

The adapter MUST distinguish connection failure before any request bytes are sent from timeout after acceptance may have occurred.

An idle stream timeout after acceptance is uncertain and MUST NOT be retried automatically.

## Health and Cooldown

Adapters MUST identify whether an error is key-specific for cooldown counting.

The following errors MUST increment the selected key's consecutive unavailable count:

* authentication invalid;
* permission denied;
* rate limited;
* quota exhausted.

Provider 5xx responses, connection failures, timeouts, interrupted streams, malformed responses, and missing usage MUST NOT increment the key's unavailable count.

A qualifying failure MUST be recorded atomically before the Provider job selects another Key. The failed Key MUST remain excluded from that job even when it has not reached the cooldown threshold. A successful Provider call MUST clear the count. On the third consecutive unavailable result, AS MUST reset the count to zero and set `cooldown_until` to the current time plus `provider.key_cooldown_seconds`. Cooldown expiry MUST restore scheduling eligibility without a background worker. Adapters MUST NOT modify owner enable state.

## Idempotency

When a Service Provider supports an idempotency key, the adapter MUST derive a stable key from the AS job ID and the in-memory try index for that job and MUST send it with the request. Intermediate tries MUST remain in memory.

An idempotency key does not permit automatic replay unless the adapter can prove the provider's idempotency contract covers the complete requested operation.

Provider-specific retry libraries MUST be disabled or configured so they cannot create hidden billable upstream requests. AS owns every Key-switch decision inside the Provider job goroutine.

## Redaction

Adapters MUST redact:

* Authorization headers;
* API keys in URLs or bodies;
* complete request bodies;
* complete response bodies;
* provider account and organization secrets.

Redacted errors MAY retain HTTP status, provider error code, request ID, retry-after value, and a bounded non-secret diagnostic message.
