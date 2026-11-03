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

## Maximum Output Tokens

When converting a public request into an upstream Provider body, adapters MUST resolve the maximum generated-token limit with the same rules used for Credits admission:

* use the request's positive `max_completion_tokens`, `max_tokens`, or `max_output_tokens` field;
* otherwise use `llm.default_max_tokens`.

OpenAI upstream bodies MUST carry that value as `max_completion_tokens`. Anthropic upstream bodies MUST carry it as `max_tokens`. Google upstream bodies MUST carry it as `generationConfig.maxOutputTokens`. Adapters MUST NOT use a separate hardcoded default that differs from `llm.default_max_tokens`.

## Reasoning Effort

Public Chat Completions `reasoning_effort` and Responses `reasoning.effort` share one validated OpenAI-compatible enum: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`. When the field is omitted or empty, adapters MUST NOT inject a native effort or thinking-control field, so the upstream model default remains in effect.

Adapters MUST map a present value as follows:

* OpenAI: send the validated value unchanged as Chat Completions `reasoning_effort`.
* Anthropic:
  * `none` MUST omit both `thinking` and `output_config.effort`;
  * every other value MUST set `thinking.type` to `adaptive`;
  * `minimal` and `low` MUST set `output_config.effort` to `low`;
  * `medium`, `high`, `xhigh`, and `max` MUST set `output_config.effort` to the same value;
  * when structured output already requires `output_config`, effort MUST be merged into that object.
* Google:
  * `none` MUST omit `generationConfig.thinkingConfig`;
  * `minimal` and `low` MUST set `thinkingConfig.thinkingLevel` to `low`;
  * `medium` MUST set `thinkingLevel` to `medium`;
  * `high`, `xhigh`, and `max` MUST set `thinkingLevel` to `high`.

## Canonical Multimodal Content

The shared canonical request MUST keep ordered text and image content parts. Image parts MUST store MIME type, base64 payload, optional OpenAI `detail`, and MUST NOT keep a decoded image bitmap after width and height are read from the header.

Adapters MUST convert those parts as follows:

* OpenAI Chat Completions: `image_url` data URLs with `detail` passed through when present;
* Anthropic Messages: `{"type":"image","source":{"type":"base64","media_type":"...","data":"..."}}` and drop `detail`;
* Google `generateContent`: `{"inlineData":{"mimeType":"...","data":"..."}}` and drop `detail`.

Image input-token estimation MUST use the same validated image metadata and MUST live beside the adapters in `provider/`. Estimation MUST follow each Service Provider's current official model-family rules and MUST NOT be configured in YAML. Settlement MUST use the adapter's normalized actual input tokens, which already include visual tokens when the upstream reports them.

## Images Adapters

Images generations and edits MUST use a shared OpenAI-shaped canonical Images parse, then a per-Service-Provider converter that validates fields and builds the upstream body. Chat Completions and Responses converters MUST NOT handle Images API types.

OpenAI Images MUST call `POST /v1/images/generations` or `POST /v1/images/edits` with the converter body, parse `data[].b64_json` and `usage`, and normalize usage as:

* text input tokens to `input_tokens`;
* image input tokens from usage details to `image_input_tokens`;
* `output_tokens_details.image_tokens` to `image_output_tokens` when present;
* aggregate `output_tokens` to `image_output_tokens` when output details are absent;
* Images text output tokens MUST NOT be billed through `output_tokens`.

Google Images MUST call `generateContent` with `responseModalities` containing `IMAGE`, extract the final non-thought inline image bytes, detect format by magic, apply local `output_format` / `output_compression` rules from [API Compatibility](api-compatibility.md), and normalize usage as:

* prompt tokens to `input_tokens`;
* IMAGE modality candidate tokens to `image_output_tokens`, or `candidatesTokenCount` when modality details are absent;
* `thoughtsTokenCount` to `reasoning_tokens`.

Anthropic MUST reject Images API types.

Successful Images adapters MUST return image bytes for AS to store under `llm.task_result_dir` using the final encoded format. Public HTTP handlers MUST encode those files as `b64_json`.

## OpenAI Adapter

The OpenAI adapter MUST call OpenAI Chat Completions for both public Chat Completions and public Responses requests. It MUST NOT call OpenAI Responses.

It MUST:

* send the configured model name unchanged;
* use Bearer authentication;
* translate validated public Chat Completions and Responses semantics into one Chat Completions upstream body through the shared canonical request shape;
* pass through validated `reasoning_effort` when present;
* restore canonical image parts as Chat Completions `image_url` data URLs and pass through a valid `detail` value;
* omit AS-local fields such as `previous_response_id` from the upstream body;
* request final stream usage even when the API Consumer did not request a public usage chunk;
* suppress the internal usage request option from public output when the client did not request it;
* parse Chat Completions SSE incrementally;
* assemble one final normalized public result for the selected public API type;
* capture upstream request ID from headers or response fields when available;
* return redacted transport-error messages that exclude request URLs and Shared API Key plaintext.

Normalized usage MUST map:

* Chat Completions `prompt_tokens` to input;
* Chat Completions `completion_tokens` to output;
* cached token details to cached input;
* reasoning token details to reasoning.

OpenAI reasoning tokens are a subset of completion tokens. Settlement MUST subtract them from ordinary output before applying both weights. Detail fields included in parent totals MUST replace the corresponding portion for pricing rather than being added on top of the parent total.

## Anthropic Adapter

The Anthropic adapter MUST translate validated Chat Completions and Responses semantics to the Anthropic Messages contract.

It MUST:

* convert validated public effort into Anthropic `thinking` and `output_config.effort` as specified in Reasoning Effort;
* construct system and message content without exposing AS-only fields;
* convert canonical image parts to Anthropic base64 image source blocks and drop OpenAI `detail`;
* convert function tools and tool choices;
* convert tool-use and tool-result history;
* translate content-block stream events into the selected OpenAI-compatible stream;
* assemble text and tool-call output;
* preserve stop reason semantics through the AS public mapping;
* capture the Anthropic request ID when available;
* return redacted transport-error messages that exclude request URLs and Shared API Key plaintext.

Normalized usage MUST map:

* `input_tokens` to ordinary input;
* `output_tokens` to output;
* `cache_read_input_tokens` to cached input;
* `cache_creation_input_tokens` to cache-write input.

Cache token fields MUST not be added twice when the parent input field includes them.

## Google Adapter

The Google adapter MUST translate validated Chat Completions and Responses semantics to the configured Google generative-language API.

It MUST:

* authenticate with the `x-goog-api-key` header and MUST NOT place the Shared API Key in the request URL;
* send the configured model name through the provider's required model path or field without changing the public name;
* convert validated public effort into `generationConfig.thinkingConfig.thinkingLevel` as specified in Reasoning Effort;
* convert system instructions, messages, tools, tool choices, and tool results;
* convert canonical image parts to Google `inlineData` and drop OpenAI `detail`;
* parse streaming candidate events incrementally;
* assemble one final normalized result;
* convert safety or policy termination into a normalized public outcome;
* capture request identity when available;
* return redacted transport-error messages that exclude request URLs and Shared API Key plaintext.

Normalized usage MUST map:

* `promptTokenCount` to input;
* `candidatesTokenCount` to output;
* `cachedContentTokenCount` to cached input;
* `thoughtsTokenCount` to reasoning;
* `toolUsePromptTokenCount` to tool-use prompt;
* `totalTokenCount` to total.

Google thoughts tokens are billed in addition to candidates output tokens. Settlement MUST NOT subtract `thoughtsTokenCount` from `candidatesTokenCount`. Other detail fields that the provider reports within parent totals MUST replace the corresponding portion for pricing rather than being added twice.

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
