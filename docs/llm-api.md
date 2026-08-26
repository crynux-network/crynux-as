# OpenAI-Compatible LLM API and Charging

This document specifies the private OpenAI-compatible LLM endpoints, the model catalog, VRAM limit resolution, Bridge forwarding, project token-ratio billing, and Credits charging rules.

## Endpoints

Each project exposes a private base URL `/api/<endpoint_token>/v1`.

Implemented endpoints:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/chat/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/completions`
* `GET /api/<endpoint_token>/v1/models`
* `GET /api/<endpoint_token>/v1/models/<model>`

The request and response formats MUST be OpenAI-compatible.

## Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. The project API key in the `Authorization: Bearer <api_key>` header, which MUST match the located project's stored key hash.

The project MUST have status active. A request failing any of these checks MUST be rejected with HTTP 401.

## Model Catalog

The model catalog is the in-memory LLM loaded-models cache refreshed from the Relay `GET /v2/loaded-models` API (see [architecture.md](./architecture.md)). All projects share the same catalog. A cached model's `min_vram` is the minimum VRAM in GB observed across historical successful executions of the model on the Crynux Network.

### `GET /api/<endpoint_token>/v1/models`

Returns the OpenAI list-models response built from the cache:

```json
{
  "object": "list",
  "data": [
    {
      "id": "qwen/qwen3.6-7b",
      "object": "model",
      "created": 0,
      "owned_by": "crynux",
      "min_vram": 24
    }
  ]
}
```

Requirements:

* The root `object` MUST be `"list"` and `data` MUST be an array.
* Each item MUST contain `id`, `object` (`"model"`), `created`, and `owned_by`.
* `id` MUST be the lowercase HuggingFace model ID from the Relay `model_id`, usable directly as the `model` field in chat and completions requests.
* `created` MUST be `0`; the Relay provides no creation time.
* `owned_by` MUST be `"crynux"`.
* Each item MUST include the extra field `min_vram` with the cached minimum VRAM in GB.
* The response MUST be the OpenAI JSON body directly, without the management-API `{"message": ...}` envelope.
* The legacy OpenAI fields `permission`, `root`, and `parent` MUST NOT be required.

### `GET /api/<endpoint_token>/v1/models/<model>`

Returns the single model object described above for a known model ID. The lookup MUST be case-insensitive. An unknown model MUST return HTTP 404 with the OpenAI error shape:

```json
{
  "error": {
    "message": "The model '<model>' does not exist",
    "type": "invalid_request_error",
    "code": "model_not_found"
  }
}
```

## VRAM Limit and Effective VRAM

### User-specified `vram_limit`

A chat or completions request specifies the VRAM limit in GB through either:

1. The URL path segment `<vram_limit>` (`POST .../v1/<vram_limit>/chat/completions`).
2. The body field `vram_limit` (unsigned integer).

The path value MUST override the body value. A non-integer path value MUST be rejected with HTTP 400. When neither is set, the user has not specified a VRAM limit.

### Effective VRAM Resolution

The effective VRAM of a request MUST be resolved as:

1. When the user specified a `vram_limit`, the effective VRAM is the user value. A user value lower than the cached `min_vram` of a known model MUST be accepted.
2. When the user did not specify a `vram_limit` and the model is in the loaded-models cache, the effective VRAM is the cached `min_vram`.
3. When the user did not specify a `vram_limit` and the model is not in the cache, the effective VRAM is `llm.default_vram_limit`.

The model lookup MUST be case-insensitive. The effective VRAM is used for both the charge tier selection and Bridge forwarding.

## Forwarding to the Crynux Bridge

The service MUST forward LLM requests to the Crynux Bridge using the platform-level Bridge API key from the service configuration (`Authorization: Bearer <api_key>`). The Bridge API key MUST have the `chat` role.

The resolved effective VRAM MUST be sent to the Bridge in the URL path:

| Client endpoint | Bridge endpoint |
|-----------------|-----------------|
| `POST .../v1/chat/completions` and `POST .../v1/<vram_limit>/chat/completions` | `POST {bridge.base_url}/v1/llm/<effective_vram>/chat/completions` |
| `POST .../v1/completions` and `POST .../v1/<vram_limit>/completions` | `POST {bridge.base_url}/v1/llm/<effective_vram>/completions` |

For streaming requests (`stream: true`), the Bridge returns server-sent events after the task completes, and includes the `usage` payload in the final chunk only when the request contains `stream_options.include_usage: true`.

When forwarding a streaming request, the service MUST:

1. Inject `stream_options.include_usage: true` into the request sent to the Bridge.
2. Return the streamed response to the client in the shape the client requested.
3. Omit the injected usage-only final chunk when the client did not request `include_usage`.

## Project Token Ratio

Each project stores a `token_ratio` that scales billed tokens relative to Bridge-reported consumed tokens.

### Allowed values

The API exposes `token_ratio` as a floating-point number. The allowed set is exactly 19 values:

* `0.1` through `1.0` in steps of `0.1`
* `2` through `10` in steps of `1`

The default value is `1.0`.

Values below `1` MUST use exactly one decimal place. Values at or above `1` MUST be whole numbers from the allowed set.

### Storage

The database column `projects.token_ratio` MUST store the ratio as an unsigned integer equal to the display value multiplied by `10`:

| Display | Stored |
|---------|--------|
| `0.1` | `1` |
| `1.0` | `10` |
| `2` | `20` |
| `10` | `100` |

Project create and update APIs MUST accept the display float, validate it against the allowed set, and persist the stored integer. Project read APIs MUST return the display float.

## LLM Charging Rules

This chapter is the complete billing specification. Each LLM call is charged from the Credits balance of the owning account.

### Charge Inputs

| Input | Symbol | Source |
|-------|--------|--------|
| Prompt tokens | `P` | Bridge `usage.prompt_tokens` |
| Completion tokens | `C` | Bridge `usage.completion_tokens` |
| Token ratio | `R` | `projects.token_ratio`, stored as display × 10 |
| VRAM tier ratio | `V` | `llm.vram_ratios` tier selected by the effective VRAM, stored as display × 10 |
| Prompt unit price | `Pp` | `llm.prompt_credits_per_token` |
| Completion unit price | `Cp` | `llm.completion_credits_per_token` |

The global unit prices and VRAM billing configuration come from the service configuration:

```yaml
llm:
  prompt_credits_per_token: 1
  completion_credits_per_token: 1
  default_max_tokens: 2048
  default_vram_limit: 24
  loaded_models_refresh_interval: 1800
  queued_priority_refresh_interval: 300
  execution_time_cache_ttl: 300
  base_vram: 8
  empty_queue_median_priority_gwei: 1
  vram_ratios:
    - max_vram: 24
      ratio: 0.5
    - max_vram: 96
      ratio: 1.5
```

* `prompt_credits_per_token` is the Credits charged per billed prompt token before integer division by the ratio scale.
* `completion_credits_per_token` is the Credits charged per billed completion token before integer division by the ratio scale.
* `default_max_tokens` is used in the pre-forward balance estimate when the request omits both `max_tokens` and `max_completion_tokens`.
* `default_vram_limit` is the effective VRAM in GB for an unknown model when the user did not specify a `vram_limit`.
* `loaded_models_refresh_interval` is the loaded-models cache refresh interval in seconds. Every YAML configuration template MUST set it to `1800`.
* `queued_priority_refresh_interval` is the queued-task priority snapshot refresh interval in seconds. Every YAML configuration template MUST set it to `300`.
* `execution_time_cache_ttl` is the per-key TTL in seconds for LLM execution-time coefficients cached from Relay. Every YAML configuration template MUST set it to `300`.
* `base_vram` is the VRAM weight base in GB used by Task Fee Estimation. Operators MUST keep it aligned with Relay `task_pricing.base_vram`. Every YAML configuration template MUST set it to `8`.
* `empty_queue_median_priority_gwei` is the median priority in Gwei used when the queued-priority cache has never observed a non-empty queue. Every YAML configuration template MUST set it to a positive integer.
* `vram_ratios` is the ordered list of VRAM billing tiers.

Configuration loading MUST fail when any of these values is zero or missing, when `vram_ratios` is empty or not sorted by strictly ascending `max_vram`, or when any tier `ratio` is not a positive number with at most one decimal place.

### VRAM Tier Selection

The effective VRAM is resolved by the rules in the Effective VRAM Resolution section. A tier `ratio` is exposed as a display float and stored as the unsigned integer `ratio × 10`, the same scheme as `token_ratio`.

The effective VRAM `v` maps to the first tier, in ascending `max_vram` order, where `v <= max_vram`. When `v` is greater than the last tier's `max_vram`, the last tier applies.

### Charge Formula

The charged Credits MUST be computed with integer arithmetic; the division truncates toward zero. The divisor `100` removes the two × 10 scale factors of `R` and `V`:

```text
credits = (P * R * V * Pp + C * R * V * Cp) / 100
```

### Balance Precheck

Before forwarding a request to the Bridge, the service MUST estimate an upper-bound charge and reject the request with HTTP 402 when the account balance is strictly less than the estimate.

Estimate inputs:

1. Prompt token estimate: UTF-8 rune count of the request prompt or chat message text content, divided by 4 and rounded up. A non-empty text whose estimate would be zero MUST use `1`.
2. Completion token estimate: `max_completion_tokens` if present and positive; otherwise `max_tokens` if present and positive; otherwise `llm.default_max_tokens`.

The estimate MUST use the same charge formula as the actual settle, with the same `V` selected from the resolved effective VRAM, substituting the estimated prompt and completion token counts.

### Settle After Bridge Response

After a successful Bridge response:

1. The service MUST read `usage.prompt_tokens`, `usage.completion_tokens`, and `usage.total_tokens`.
2. The service MUST compute Credits with the charge formula, using the same `V` resolved before forwarding.
3. The service MUST create one `llm_call_records` row with success status, token counts, the project `token_ratio` used for the charge, charged Credits, the billed effective VRAM, call duration, and the Task Fee Estimation fields computed before forwarding.
4. When the computed Credits are greater than zero and the account balance is sufficient, the service MUST create one `credit_events` row of type LLM charge referencing the call record ID and MUST decrease the account balance by the same amount in the same database transaction.

When the Bridge call succeeds but the account balance is insufficient for the computed Credits at settle time, the service MUST still create a success `llm_call_records` row with charged Credits set to `0`, MUST NOT create a Credits ledger event, and MUST emit an error log for operators.

### Failed Calls

Every failed LLM call MUST be recorded as one `llm_call_records` row with failure status. A failed call MUST NOT create a Credits ledger event.

Failure includes Bridge transport errors, Bridge HTTP status greater than or equal to 400, response parse failures, and streaming responses that end without a usable `usage` payload.

### Call Record Fields

Each `llm_call_records` row MUST contain:

* `project_id`
* `model`
* `prompt_tokens`, `completion_tokens`, `total_tokens`
* `token_ratio` (the project cost level used for the charge, stored as display × 10)
* `status` (success or failed)
* `credits` (charged Credits; `0` when not charged)
* `duration_ms`
* `billed_vram` (the resolved effective VRAM in GB used for tier selection and Bridge forwarding)

When Task Fee Estimation succeeds before Bridge forwarding, a success `llm_call_records` row MUST also contain:

* `task_fee_gwei`
* `median_priority_gwei`
* `estimated_node_seconds`
* `vram_weight`

When Task Fee Estimation fails and the request is aborted before Bridge forwarding, the failure `llm_call_records` row MUST leave those four fields empty. Management query APIs MUST NOT expose these four fields.

## Task Fee Estimation

After the Credits balance precheck succeeds and before the request is forwarded to the Bridge, the service MUST estimate a task fee in Gwei. The estimate is recorded on successful call rows. The estimate MUST NOT be sent to the Bridge. The Credits charge formula MUST NOT use the task fee.

### Formula

Version 1 covers text work only. The model-switch term MUST be zero. Image work MUST NOT be included.

```text
estimated_node_seconds =
    constant_seconds
    + seconds_per_input_token * estimated_prompt_tokens
    + seconds_per_output_token * max_completion_tokens

vram_weight =
    max(effective_vram, base_vram) / base_vram

target_priority_gwei =
    median_priority_gwei * token_ratio_display

task_fee_gwei =
    floor(target_priority_gwei * estimated_node_seconds * vram_weight)
```

* `estimated_prompt_tokens` and `max_completion_tokens` MUST be the same values used by the Credits balance precheck.
* `token_ratio_display` MUST be the project token ratio display float (`projects.token_ratio` stored integer divided by `10`).
* `effective_vram` MUST be the resolved effective VRAM of the request.
* `base_vram` MUST come from `llm.base_vram`.
* `vram_weight` MUST be computed locally by Crynux AS. The service MUST NOT query Relay for `vram_weight`.
* `constant_seconds`, `seconds_per_input_token`, and `seconds_per_output_token` MUST come from Relay `GET /v2/models/llm/execution-time` with query `model=<request model>` and `min_vram=<effective_vram>`.
* `median_priority_gwei` MUST come from the queued-task priority snapshot cache fed by Relay `GET /v2/tasks/queued/priority`.

`task_fee_gwei` MUST be computed with floating-point intermediates and MUST truncate toward zero to a non-negative integer Gwei value.

### Empty Queue Median

When the latest priority snapshot has `queued_task_count = 0` or a null `median_priority_gwei`:

1. If the cache still holds the most recent non-empty `median_priority_gwei`, the service MUST use that value.
2. Otherwise the service MUST use `llm.empty_queue_median_priority_gwei`.

### Estimation Failure

When Task Fee Estimation fails, the service MUST:

1. Log the concrete error cause.
2. Create one failure `llm_call_records` row without the four fee fields.
3. Return HTTP 500 to the client.
4. MUST NOT forward the request to the Bridge.

## Billing Config Query API

`GET /v1/llm/billing_config` is a management API that requires a valid JWT token. It MUST NOT be exposed on the private LLM surface. It returns the configured prompt and completion unit prices together with the VRAM billing tiers (display float ratios):

```json
{
  "message": "success",
  "data": {
    "prompt_credits_per_token": 1,
    "completion_credits_per_token": 1,
    "vram_ratios": [
      { "max_vram": 24, "ratio": 0.5 },
      { "max_vram": 96, "ratio": 1.5 }
    ]
  }
}
```
