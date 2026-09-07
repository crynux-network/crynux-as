# OpenAI-Compatible LLM API and Charging

This document specifies the private OpenAI-compatible LLM endpoints, the model catalog, VRAM limit resolution, Bridge forwarding, project token ratio, call records, and Task Fee Estimation.

Credits charging for LLM calls is specified only in [credits-billing.md](./credits-billing.md).

## Endpoints

Each project exposes a private base URL `/api/<endpoint_token>/v1`.

Implemented endpoints:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/completions`
* `POST /api/<endpoint_token>/v1/responses`
* `GET /api/<endpoint_token>/v1/responses/<response_id>`
* `POST /api/<endpoint_token>/v1/<vram_limit>/chat/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/completions`
* `GET /api/<endpoint_token>/v1/models`
* `GET /api/<endpoint_token>/v1/models/<model>`

The chat, completions, and responses request and response formats MUST be OpenAI-compatible for the supported first-version fields.

## Responses API

### `POST /api/<endpoint_token>/v1/responses`

Creates a persisted LLM job and returns an OpenAI Responses object.

Supported request fields in the first version:

* `model`
* `input` as a string or array of input items
* `instructions`
* function `tools`
* function-call and function-call-output history in `input`
* `background` (`true` returns immediately with `queued` or `in_progress`; `false` waits for completion)
* `max_output_tokens`, `temperature`, `top_p`, `stop`, `seed`, `tool_choice`, and `vram_limit`

The first version MUST reject unsupported fields with HTTP 400 and the OpenAI invalid-request error shape. Unsupported fields include `stream`, `previous_response_id`, `conversation`, built-in tools, `cancel`, and `delete`.

When `background` is `true`, the handler MUST persist the job and return immediately after the job is stored. The returned `status` MUST be `queued` or `in_progress`. The client MUST poll `GET /responses/<response_id>` until the job reaches a terminal state.

When `background` is `false`, the handler MUST use the same persisted job flow and wait for the job to complete before returning the final Responses object. A terminal `failed` job MUST return HTTP 200 with a Responses object whose `status` is `failed` and whose `error` field contains the failure detail. A terminal `completed` job MUST return the formatted Responses object.

### `GET /api/<endpoint_token>/v1/responses/<response_id>`

Returns the persisted Responses object for the project's response ID. A missing or foreign response ID MUST return HTTP 404.

Terminal `completed` responses MUST include formatted `output` and `usage` only after the raw Bridge result, formatted result, usage, and billing settlement are stored. Terminal `failed` responses MUST include an OpenAI-format error object.

## LLM Job Execution

Chat completions, completions, and responses share one persisted `llm_jobs` table and one background worker loop.

For every LLM request, the service MUST:

1. Parse the public API request into canonical `GPTTaskArgs`.
2. Create an `llm_jobs` row with status `pending_submit`.
3. Let the background worker advance unfinished jobs in batches: submit `pending_submit` jobs to Bridge, query ClientTask status for in-flight jobs, download the raw `GPTTaskResponse` for each successful job, format the public API result, and perform one-time Credits settlement before marking the job `completed`.
4. For chat completions and completions, wait synchronously on the HTTP request until the job reaches a terminal state, then return the formatted JSON body or simulated SSE stream.
5. For responses with `background=false`, wait synchronously on the HTTP request until the job reaches a terminal state.
6. For responses with `background=true`, return immediately after persistence and expose status through `GET /responses/<response_id>`.

The worker MUST keep one loop. Each tick MUST load a bounded batch of unfinished jobs (`pending_submit`, `submitted`, `in_progress`), advance every job in that batch by at most one step, sleep a fixed interval, and start the next tick. The worker MUST NOT wait inside one job until that job reaches a terminal Bridge status while other unfinished jobs remain unprocessed.

A `pending_submit` job whose age since `created_at` is greater than or equal to `llm.job_submit_timeout` seconds MUST be marked `failed` with a failed call record. The worker MUST NOT submit that job to Bridge after the timeout.

Client disconnect during a synchronous wait MUST NOT cancel the persisted job or settlement.

The stable `llm_jobs.id` MUST be the billing settlement source. Repeated polling, synchronous waits, and worker retries MUST NOT create duplicate Credits charges.

## Forwarding to the Crynux Bridge

The service MUST execute LLM jobs through the Crynux Bridge raw task APIs using the platform-level Bridge API key from the service configuration (`Authorization: Bearer <api_key>`). The Bridge API key MUST have the `chat` role.

AS MUST NOT send `client_id` in Bridge raw task requests. Bridge MUST derive the client identity from the API key.

| AS operation | Bridge endpoint |
|--------------|-----------------|
| Create LLM raw tasks | `POST {bridge.base_url}/v1/inference_tasks/batch` |
| Query ClientTask statuses | `POST {bridge.base_url}/v1/inference_tasks/batch/status` |
| Download LLM JSON result | `GET {bridge.base_url}/v1/inference_tasks/<client_task_id>/llm` |

Single-task create and status endpoints MUST remain available on Bridge. The AS worker MUST use the batch create and batch status endpoints.

Each raw task submission MUST create a new Bridge ClientTask. AS MUST NOT send `request_id`. After Bridge returns a client task ID, AS MUST persist it on the LLM job and MUST use that ID for all later status queries and result downloads.

The resolved effective VRAM MUST be sent as `min_vram` on raw task creation.

The LLM job's `task_fee_gwei` MUST be multiplied by `1,000,000,000` with integer arithmetic and sent as the required raw task `task_fee` in Wei. AS MUST NOT send the stored GWei value directly. Bridge MUST treat this value as the final task fee and MUST NOT apply task-size multiplication or unit conversion.

Bridge MUST return its whole-client-task aggregated status as one of `running`, `success`, or `failed`. AS MUST treat only `success` and `failed` as terminal. AS MUST NOT aggregate individual Bridge inference-task statuses.

Bridge status queries and result download MUST remain authorized after the Bridge API key's creation quota is exhausted. Invalid, expired, wrong-role, and cross-client access MUST remain rejected by Bridge.

AS owns OpenAI-compatible request parsing, raw `GPTTaskResponse` normalization into chat completions, completions, and responses output shapes, and simulated SSE for chat completions and completions. Bridge OpenAI `/v1/llm/*` endpoints are not used by AS.

For streaming chat or completions requests (`stream: true`), the service MUST return simulated server-sent events after the persisted job completes. When the client sets `stream_options.include_usage: true`, the final chunk MUST include `usage`.

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

The model lookup MUST be case-insensitive. The effective VRAM is used for `vram_weight`, Relay execution-time selection (`min_vram`), and Bridge raw task `min_vram`.

## Authentication

Each project stores a `token_ratio` that is the user cost level. It scales Credits and the submitted task fee as specified in [credits-billing.md](./credits-billing.md) and Task Fee Estimation.

### Allowed values

The API exposes `token_ratio` as a floating-point number. The allowed set is:

* `0.1` through `1.0` in steps of `0.1`
* `2` through `llm.max_token_ratio` in steps of `1`

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
| `30` | `300` |

Project create and update APIs MUST accept the display float, validate it against the allowed set, and persist the stored integer. Project read APIs MUST return the display float.

## LLM Charging Rules

Credits charging, balance precheck, settle, Credits configuration, failed-call non-charging rules, and the pricing-examples management API are specified only in [credits-billing.md](./credits-billing.md).

The LLM-related service configuration items that remain shared with Task Fee Estimation and request handling are:

```yaml
llm:
  default_max_tokens: 2048
  default_vram_limit: 24
  loaded_models_refresh_interval: 1800
  queued_priority_refresh_interval: 300
  execution_time_cache_ttl: 300
  base_vram: 8
  empty_queue_median_priority_gwei: "34"
  reference_priority_gwei: "34"
  credits_per_gwei: 1
  max_token_ratio: 30
  job_submit_timeout: 600
```

* `default_max_tokens` is used in the pre-forward balance estimate and Task Fee Estimation when the request omits both `max_tokens` and `max_completion_tokens`.
* `default_vram_limit` is the effective VRAM in GB for an unknown model when the user did not specify a `vram_limit`.
* `loaded_models_refresh_interval` is the loaded-models cache refresh interval in seconds. Every YAML configuration template MUST set it to `1800`.
* `queued_priority_refresh_interval` is the queued-task priority snapshot refresh interval in seconds. Every YAML configuration template MUST set it to `300`.
* `execution_time_cache_ttl` is the per-key TTL in seconds for LLM execution-time coefficients cached from Relay. Every YAML configuration template MUST set it to `300`.
* `base_vram` is the VRAM weight base in GB used by Task Fee Estimation and Credits. Operators MUST keep it aligned with Relay `task_pricing.base_vram`. Every YAML configuration template MUST set it to `8`.
* `empty_queue_median_priority_gwei` is the median priority hint used when the queued-priority cache has never observed a non-empty queue. It MUST NOT enter task fee or Credits. Every YAML configuration template MUST set it to a positive decimal integer string. Every Gwei-denominated LLM configuration and response field whose name ends in `_gwei` MUST use a decimal integer string.
* `reference_priority_gwei` and `credits_per_gwei` are specified in [credits-billing.md](./credits-billing.md). `reference_priority_gwei` MUST be a decimal integer string. `credits_per_gwei` MUST be an unsigned integer.
* `max_token_ratio` is the maximum allowed project cost level display value. The allowed `token_ratio` set is `0.1` through `1.0` in steps of `0.1`, then `2` through `max_token_ratio` in steps of `1`. Configuration loading MUST fail when `max_token_ratio` is less than `2`.
* `job_submit_timeout` is the maximum age in seconds of a `pending_submit` LLM job before the worker stops Bridge submit retries and marks the job failed. Every YAML configuration template MUST set it to a positive integer.

Configuration loading MUST fail when any required LLM configuration value is zero or missing, including the Credits and reference-priority fields required by [credits-billing.md](./credits-billing.md).

### Call Record Fields

Each `llm_call_records` row MUST contain:

* `project_id`
* `model`
* `prompt_tokens`, `completion_tokens`, `total_tokens`
* `token_ratio` (the project cost level used for the charge, stored as display × 10)
* `status` (success or failed)
* `credits` (charged Credits; `0` when not charged)
* `duration_ms`
* `billed_vram` (the resolved effective VRAM in GB used for VRAM weight and Bridge forwarding)

When Task Fee Estimation succeeds before Bridge forwarding, a success `llm_call_records` row MUST also contain:

* `task_fee_gwei`
* `median_priority_gwei` (Queue Median Hint value resolved at estimation time; MUST NOT have been used to compute `task_fee_gwei`)
* `estimated_node_seconds` (pre-forward estimate from precheck token counts; MUST NOT be the settle-time Credits recomputation)
* `vram_weight`

When Task Fee Estimation fails and the request is aborted before Bridge forwarding, the failure `llm_call_records` row MUST leave those four fields empty. Management query APIs MUST NOT expose these four fields.

## Task Fee Estimation

Task fee estimation is part of the shared pre-forward path in [credits-billing.md](./credits-billing.md). The service MUST compute `billable_gwei` once for Credits precheck and task fee, then set `task_fee_gwei = floor(billable_gwei)`. The worker MUST convert the persisted Gwei fee to Wei and send it as the Bridge raw task's final `task_fee`.

The task fee formula MUST use the shared `billable_gwei` defined in [credits-billing.md](./credits-billing.md). Task fee MUST NOT use the live queued-task `median_priority_gwei` as a multiplier.

### Formula

Version 1 covers text work only. The model-switch term MUST be zero. Image work MUST NOT be included.

```text
estimated_node_seconds =
    constant_seconds
    + seconds_per_input_token * estimated_prompt_tokens
    + seconds_per_output_token * max_completion_tokens

vram_weight =
    max(effective_vram, base_vram) / base_vram

billable_gwei =
    reference_priority_gwei * token_ratio_display * estimated_node_seconds * vram_weight

task_fee_gwei =
    floor(billable_gwei)
```

* `estimated_prompt_tokens` and `max_completion_tokens` MUST be the same values used by the Credits balance precheck.
* `token_ratio_display` MUST be the project token ratio display float (`projects.token_ratio` stored integer divided by `10`).
* `effective_vram` MUST be the resolved effective VRAM of the request.
* `base_vram` MUST come from `llm.base_vram`.
* `vram_weight` MUST be computed locally by Crynux AS. The service MUST NOT query Relay for `vram_weight`.
* `constant_seconds`, `seconds_per_input_token`, and `seconds_per_output_token` MUST come from Relay `GET /v2/models/llm/execution-time` with query `model=<request model>` and `min_vram=<effective_vram>`.
* `reference_priority_gwei` MUST come from `llm.reference_priority_gwei` as a decimal integer string.

`task_fee_gwei` MUST be computed with floating-point intermediates and MUST truncate toward zero to a non-negative integer Gwei value, persisted as a decimal integer string where stored as text.

The service MUST resolve `median_priority_gwei` with the Queue Median Hint rules below and MUST persist that value on the job and call record for comparison. That snapshot MUST NOT enter `billable_gwei`. Resolution MUST always produce a value.

### Queue Median Hint

When the service needs a current queue median hint for `billing_config`, WebUI display, or the job/call-record snapshot:

1. If the latest priority snapshot has a non-null `median_priority_gwei` and `queued_task_count > 0`, the service MUST use that value.
2. Else if the cache still holds the most recent non-empty `median_priority_gwei`, the service MUST use that value.
3. Otherwise the service MUST use `llm.empty_queue_median_priority_gwei`.

The resolved hint MUST be a decimal integer string.

### Estimation Failure

Task Fee Estimation MUST fail when:

1. Relay `GET /v2/models/llm/execution-time` cannot be completed successfully for the request model and effective VRAM; or
2. The returned coefficients fail validation: any of `constant_seconds`, `seconds_per_input_token`, or `seconds_per_output_token` is missing or is not a finite number greater than or equal to zero.

Reading or resolving the queue median hint MUST NOT cause Task Fee Estimation to fail.

After configuration `reference_priority_gwei`, project `token_ratio`, validated coefficients, and `vram_weight` are available, computing `billable_gwei` and `task_fee_gwei` MUST succeed. Ordinary arithmetic on those validated inputs is not a business failure mode.

When Task Fee Estimation fails, the service MUST:

1. Log the concrete error cause.
2. Create one failure `llm_call_records` row without the four fee fields.
3. Return HTTP 500 to the client.
4. MUST NOT forward the request to the Bridge.

## Billing Config Query API

`GET /v1/llm/billing_config` and `GET /v1/llm/pricing_examples` are specified in [credits-billing.md](./credits-billing.md).
