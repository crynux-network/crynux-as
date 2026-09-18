# OpenAI-Compatible LLM API and Charging

This document specifies the private OpenAI-compatible LLM endpoints, the model catalog, VRAM limit resolution, Bridge forwarding, project Cost Level (`priority_gwei`), call records, and Task Fee Estimation.

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
* `POST /api/<endpoint_token>/v1/<vram_limit>/responses`
* `GET /api/<endpoint_token>/v1/models`
* `GET /api/<endpoint_token>/v1/models/<model>`

The chat, completions, and responses request and response formats MUST be OpenAI-compatible for the supported first-version fields.

Chat Completions MUST accept `chat_template_kwargs` and `reasoning_effort`. Responses MUST accept `chat_template_kwargs` and `reasoning.effort`. AS MUST map those public fields to canonical `template_args` as specified in [model-compatibility/architecture.md](./model-compatibility/architecture.md). `chat_template_kwargs` MUST carry model-template keys such as `enable_thinking` or `thinking`. `reasoning_effort` and `reasoning.effort` MUST only inject `enable_thinking`; they MUST NOT rename keys for other templates.

## Responses API

### `POST /api/<endpoint_token>/v1/responses`

Creates a persisted LLM job and returns an OpenAI Responses object.

The same create operation is also available at `POST /api/<endpoint_token>/v1/<vram_limit>/responses`. The path segment and body field `vram_limit` follow the VRAM Limit and Effective VRAM rules.

Supported request fields in the first version:

* `model`
* `input` as a string or array of input items. Supported array item shapes are:
  * message items with `type` set to `message`
  * EasyInputMessage items that omit `type` and provide `role` and `content`; these MUST be treated as message items
  * `function_call` items
  * `function_call_output` items
* `instructions`
* `previous_response_id`
* function `tools`
* function-call and function-call-output history in `input`
* `background` (`true` returns immediately with `queued` or `in_progress`; `false` waits for completion)
* `max_output_tokens`, `temperature`, `top_p`, `stop`, `seed`, `tool_choice`, `text.format`, and `vram_limit`
* `chat_template_kwargs`
* `reasoning.effort`

The service MUST reject unsupported fields with HTTP 400 and the OpenAI invalid-request error shape. Unsupported fields include `stream`, `conversation`, built-in tools, `cancel`, `delete`, and `structured_outputs`.

`previous_response_id` MUST follow [llm-job-processing.md](./llm-job-processing.md): only a same-project, still-retained, successfully completed Responses job is accepted; the new job MUST store the fully expanded `TaskArgsJSON`; previous instructions MUST NOT be inherited as this call's instructions.

When `background` is `true`, the handler MUST persist the job and return immediately after the job is stored. The returned `status` MUST be `queued` or `in_progress`. The client MUST poll `GET /responses/<response_id>` until the job reaches a terminal state.

When `background` is `false`, the handler MUST use the same persisted job flow and wait for the job to complete before returning the final Responses object. A terminal `failed` job MUST return HTTP 200 with a Responses object whose `status` is `failed` and whose `error` field contains the failure detail. A terminal `completed` job MUST return the formatted Responses object.

### `GET /api/<endpoint_token>/v1/responses/<response_id>`

Returns the persisted Responses object for the project's response ID. A missing, foreign, or retention-expired response ID MUST return HTTP 404.

Terminal `completed` responses MUST include formatted `output` and `usage` only after the raw Bridge result, formatted result, and billing settlement are stored. Terminal `failed` responses MUST include an OpenAI-format error object.

## Structured Output and Function Tools

Chat Completions `response_format` and Responses `text.format` MUST accept `text`, `json_object`, and `json_schema`. The service MUST omit the canonical constraint for `text`. It MUST map the other two forms to the same canonical `response_format` shape. A `json_schema` format MUST contain a non-empty name and an object-valued schema. Unknown format fields and the public vLLM `structured_outputs` extension MUST be rejected. Public `regex`, `choice`, `grammar`, `structural_tag`, whitespace, and additional-properties controls MUST NOT be accepted.

Function tools MUST have unique, non-empty names. Chat Completions tools MUST use the nested `{"type":"function","function":...}` shape. Responses tools MUST use the Responses flat function shape and MUST be converted to the canonical nested shape. Built-in tools MUST be rejected.

`tool_choice` MUST support `none`, `auto`, `required`, and one named function. A named choice MUST identify exactly one declared function. Responses named choices MUST be converted to the canonical Chat Completions named-choice shape. An omitted choice MUST become `auto` when tools are present and `none` otherwise.

The canonical behavior MUST be:

* `none`: tools remain available to the model input template, generation receives no tool constraint, and AS MUST NOT parse generated text as tool calls.
* `auto`: generation receives a tool constraint only when at least one tool has `strict=true`.
* `required`: generation MUST produce at least one declared function call.
* named function: generation MUST allow only the selected function.

A required, named, or strict-auto tool constraint MUST take precedence over `response_format` for that call. `response_format` output MUST remain assistant `content` or Responses `output_text`; AS MUST NOT parse or rewrite its JSON value.

AS MUST parse constrained raw tool output through an ordered syntax registry covering `llama`, `kimi`, `deepseek_r1`, `deepseek_v3_1`, `deepseek_v3_2`, `deepseek_v4`, `qwen_3`, `qwen_3_coder`, `qwen_3_5`, `glm_4_7`, `hermes`, `hy_v4`, and `kimi_k3`. Parser selection MUST use generated syntax and MUST NOT use the request model ID. A parsed function name MUST match a declared tool. Named-function output containing only a JSON arguments object MUST be restored with the selected function name.

Chat Completions MUST return parsed calls in `message.tool_calls` and set `finish_reason=tool_calls`. Responses MUST return parsed calls as `function_call` output items. Simulated SSE MUST use the same persisted formatted result. `previous_response_id` history reconstruction MUST use the previous job's canonical task arguments so `none`, named choice, and syntax parsing remain consistent.

## LLM Job Execution

Chat completions, completions, and responses share one `llm_jobs` table and one background worker loop. Table ownership, settle transactions, Responses retention, and Recent Requests merge rules are specified in [llm-job-processing.md](./llm-job-processing.md).

For every LLM request, the service MUST:

1. Parse the public API request into canonical `GPTTaskArgs`, expanding `previous_response_id` history when present. During parsing, the request `model` MUST be normalized to lowercase with surrounding whitespace trimmed. That normalized value MUST be used for fee estimation, VRAM lookup, `llm_jobs.model`, `llm_call_records.model`, canonical `task_args.model`, usage stats, and the public response `model` field.
2. Create an `llm_jobs` row with status `pending_submit` and the fully expanded `TaskArgsJSON`.
3. Let the background worker advance unfinished jobs in batches: submit `pending_submit` jobs to Bridge, query ClientTask status for in-flight jobs, download the raw `GPTTaskResponse` for each successful job, format the public API result, and perform one-time Credits settlement before marking the job `completed`.
4. For chat completions and completions, wait synchronously on the HTTP request until the job reaches a terminal state, then return the formatted JSON body or simulated SSE stream.
5. For responses with `background=false`, wait synchronously on the HTTP request until the job reaches a terminal state.
6. For responses with `background=true`, return immediately after persistence and expose status through `GET /responses/<response_id>`.

The worker MUST keep one loop. Each tick MUST load a bounded batch of unfinished jobs (`pending_submit`, `submitted`, `in_progress`), advance every job in that batch by at most one step, sleep a fixed interval, and start the next tick. The worker MUST NOT wait inside one job until that job reaches a terminal Bridge status while other unfinished jobs remain unprocessed.

A `pending_submit` job whose age since `created_at` is greater than or equal to `llm.job_submit_timeout` seconds MUST be marked `failed` with a failed call record. The worker MUST NOT submit that job to Bridge after the timeout.

Client disconnect during a synchronous wait MUST NOT cancel the persisted job or settlement.

The stable `llm_jobs.id` MUST be the billing settlement source while the job exists. Repeated polling, synchronous waits, and worker retries MUST NOT create duplicate Credits charges. After retention deletes the job, the permanent call record and any Credits event remain.

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

A chat, completions, or responses request specifies the VRAM limit in GB through either:

1. The URL path segment `<vram_limit>` (`POST .../v1/<vram_limit>/chat/completions`, `POST .../v1/<vram_limit>/completions`, or `POST .../v1/<vram_limit>/responses`).
2. The body field `vram_limit` (unsigned integer).

The path value MUST override the body value. A non-integer path value MUST be rejected with HTTP 400. When neither is set, the user has not specified a VRAM limit.

### Effective VRAM Resolution

The effective VRAM of a request MUST be resolved as:

1. When the user specified a `vram_limit`, the effective VRAM is the user value. A user value lower than the cached `min_vram` of a known model MUST be accepted.
2. When the user did not specify a `vram_limit` and the model is in the loaded-models cache, the effective VRAM is the cached `min_vram`.
3. When the user did not specify a `vram_limit` and the model is not in the cache, the effective VRAM is `llm.default_vram_limit`.

The model lookup MUST be case-insensitive. The effective VRAM is used for `vram_weight`, Relay execution-time selection (`min_vram`), and Bridge raw task `min_vram`.

## Project Cost Level

Each project stores a Cost Level configuration that determines the effective `priority_gwei` used for Credits and the submitted task fee as specified in [credits-billing.md](./credits-billing.md) and Task Fee Estimation.

### Stored Fields

| Field | Required | Meaning |
|-------|----------|---------|
| `cost_level_mode` | Yes | `static` or `auto`. Default at create and for migrated existing projects: `static`. |
| `priority_gwei` | Yes | Static-mode Cost Level as a decimal integer Gwei string. Independent of auto fields. |
| `auto_queue_position` | Nullable until Auto is configured | Integer in `[0, 100]`. Default at create: `50`. |
| `auto_max_priority_gwei` | Nullable until Auto is configured | Auto-mode upper cap as a decimal integer Gwei string within `[llm.min_priority_gwei, llm.max_priority_gwei]`. |

Switching mode MUST NOT overwrite the other mode's stored values. Project update MUST reject `cost_level_mode` values other than `static` or `auto`. Project update MUST reject a `priority_gwei` or `auto_max_priority_gwei` outside the inclusive range `[llm.min_priority_gwei, llm.max_priority_gwei]`. Project update MUST reject an `auto_queue_position` outside `[0, 100]`. Setting `cost_level_mode` to `auto` MUST be rejected when `auto_max_priority_gwei` is missing or invalid after applying the same update payload.

### Create Defaults

When a project is created, the service MUST:

1. Set `cost_level_mode` to `static`.
2. Set `priority_gwei` to the Queue Median Hint resolved by `ResolveQueueMedianHint`, clamped into `[min_priority_gwei, max_priority_gwei]`.
3. Set `auto_queue_position` to `50`.
4. Set `auto_max_priority_gwei` from one consistent queued-priority snapshot: the live `highest_priority_gwei` when the queue is non-empty and both bounds are present and valid; otherwise `llm.min_priority_gwei`. Clamp the result into `[min_priority_gwei, max_priority_gwei]`.

The create request MUST NOT require the client to supply Cost Level fields. After creation, only an explicit project update MUST change Cost Level fields.

Migration of existing projects MUST set `cost_level_mode` to `static` and MUST preserve the existing `priority_gwei`. Migration MUST NOT invent Auto values for existing projects; `auto_queue_position` and `auto_max_priority_gwei` MAY remain null until the first Auto configuration update.

### Effective Priority Resolution

Before Credits precheck and Task Fee Estimation for an LLM request, the service MUST resolve one effective `priority_gwei` for that request:

1. When `cost_level_mode` is `static`, the effective value MUST be the project's stored `priority_gwei`.
2. When `cost_level_mode` is `auto`:
   1. If the live queued-priority snapshot has `queued_task_count > 0` and both `lowest_priority_gwei` and `highest_priority_gwei` are present, positive, and `highest >= lowest`, compute:
      ```text
      effective = lowest + floor((highest - lowest) * auto_queue_position / 100)
      ```
      using integer arithmetic. The result MUST lie in `[lowest, highest]`.
   2. Otherwise the effective value MUST be `llm.min_priority_gwei`.
   3. The service MUST then set `effective = min(effective, auto_max_priority_gwei)`.
   4. The service MUST clamp the result into `[llm.min_priority_gwei, llm.max_priority_gwei]`.
3. When `cost_level_mode` is `auto` and `auto_max_priority_gwei` is missing, the request MUST fail as a server error and MUST NOT forward to Bridge.

The resolved effective value MUST be snapshotted onto the LLM job and call record. Settle MUST use that snapshot and MUST NOT re-resolve from the project or live queue.

Project read APIs MUST return `cost_level_mode`, `priority_gwei`, `auto_queue_position` (null when unset), and `auto_max_priority_gwei` (null when unset). `priority_gwei` and `auto_max_priority_gwei` MUST be decimal integer strings when present. Project update MUST be a partial update: omitted Cost Level fields MUST remain unchanged, and a rename that omits Cost Level fields MUST NOT require or clear them.

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
  min_priority_gwei: "1"
  max_priority_gwei: "1000000000"
  credits_per_gwei: "1"
  job_submit_timeout: 600
  job_retention_days: 30
  project_recent_requests_limit: 50
```

* `default_max_tokens` is used in the pre-forward balance estimate and Task Fee Estimation when the request omits both `max_tokens` and `max_completion_tokens`.
* `default_vram_limit` is the effective VRAM in GB for an unknown model when the user did not specify a `vram_limit`.
* `loaded_models_refresh_interval` is the loaded-models cache refresh interval in seconds. Every YAML configuration template MUST set it to `1800`.
* `queued_priority_refresh_interval` is the queued-task priority snapshot refresh interval in seconds. Every YAML configuration template MUST set it to `300`.
* `execution_time_cache_ttl` is the per-key TTL in seconds for LLM execution-time coefficients cached from Relay. Every YAML configuration template MUST set it to `300`.
* `base_vram` is the VRAM weight base in GB used by Task Fee Estimation and Credits. Operators MUST keep it aligned with Relay `task_pricing.base_vram`. Every YAML configuration template MUST set it to `8`.
* `empty_queue_median_priority_gwei` is the median priority hint used when the queued-priority cache has never observed a non-empty queue, and when creating a project with no usable live or remembered median. It MUST NOT enter `billable_gwei` except when it becomes the project's stored `priority_gwei` at project creation through the Queue Median Hint rules. Every YAML configuration template MUST set it to a positive decimal integer string. Every Gwei-denominated LLM configuration and response field whose name ends in `_gwei` MUST use a decimal integer string.
* `min_priority_gwei` and `max_priority_gwei` are the hard bounds for project Cost Level values. They MUST be positive decimal integer strings. Configuration loading MUST fail when `min_priority_gwei` is greater than `max_priority_gwei`.
* `credits_per_gwei` is specified in [credits-billing.md](./credits-billing.md). It MUST be a positive decimal string.
* `job_submit_timeout` is the maximum age in seconds of a `pending_submit` LLM job before the worker stops Bridge submit retries and marks the job failed. Every YAML configuration template MUST set it to a positive integer.
* `job_retention_days` is the number of days a terminal settled job remains readable for Responses lookup and `previous_response_id`. Every YAML configuration template MUST set it to a positive integer. Example configurations MUST use `30`.
* `project_recent_requests_limit` is the maximum number of Recent Requests rows returned for a project. Every YAML configuration template MUST set it to a positive integer.

Configuration loading MUST fail when any required LLM configuration value is zero or missing, including the Credits and Cost Level bound fields required by [credits-billing.md](./credits-billing.md). Configuration MUST NOT include `reference_priority_gwei` or `max_token_ratio`.

### Call Record Fields

Call-record ownership and the Credits amount ledger are specified in [llm-job-processing.md](./llm-job-processing.md).

Each `llm_call_records` row MUST contain:

* `project_id`
* `model`
* `prompt_tokens`, `completion_tokens`, `total_tokens`
* `priority_gwei` (the effective Cost Level used for the charge, as a decimal integer string)
* `status` (success or failed)
* `duration_ms`
* `billed_vram` (the resolved effective VRAM in GB used for VRAM weight and Bridge forwarding)

When Task Fee Estimation succeeds before Bridge forwarding, a success `llm_call_records` row MUST also contain:

* `task_fee_gwei`
* `median_priority_gwei` (Queue Median Hint value resolved at estimation time; MUST NOT have been used to compute `task_fee_gwei`)
* `estimated_node_seconds` (pre-forward estimate from precheck token counts; MUST NOT be the settle-time Credits recomputation)
* `vram_weight`

A settled success call record MUST also contain the Credits recalculation snapshot:

* `constant_seconds`
* `seconds_per_input_token`
* `seconds_per_output_token`
* `credits_per_gwei`

The call record MUST NOT store the charged Credits amount. The call record MUST NOT store `reference_priority_gwei`. Actual Credits changes MUST exist only on processed `credit_events` rows.

When Task Fee Estimation fails and the request is aborted before Bridge forwarding, the failure `llm_call_records` row MUST leave the four fee fields empty. Management query APIs MUST NOT expose the fee fields or recalculation snapshot fields.

## Task Fee Estimation

Task fee estimation is part of the shared pre-forward path in [credits-billing.md](./credits-billing.md). The service MUST compute `billable_gwei` once for Credits precheck and task fee, then set `task_fee_gwei = floor(billable_gwei)`. The worker MUST convert the persisted Gwei fee to Wei and send it as the Bridge raw task's final `task_fee`.

The task fee formula MUST use the shared `billable_gwei` defined in [credits-billing.md](./credits-billing.md). Task fee MUST NOT use the live queued-task `median_priority_gwei` as a multiplier. For static mode, task fee MUST NOT use live queue bounds. For auto mode, live queue bounds MAY determine the effective `priority_gwei` only at job create before the snapshot.

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
    priority_gwei * estimated_node_seconds * vram_weight

task_fee_gwei =
    floor(billable_gwei)
```

* `estimated_prompt_tokens` and `max_completion_tokens` MUST be the same values used by the Credits balance precheck.
* `priority_gwei` MUST be the effective Cost Level resolved for the request from the project Cost Level mode, snapshotted onto the job at create.
* `effective_vram` MUST be the resolved effective VRAM of the request.
* `base_vram` MUST come from `llm.base_vram`.
* `vram_weight` MUST be computed locally by Crynux AS. The service MUST NOT query Relay for `vram_weight`.
* `constant_seconds`, `seconds_per_input_token`, and `seconds_per_output_token` MUST come from Relay `GET /v2/models/llm/execution-time` with query `model=<request model>` and `min_vram=<effective_vram>`.

`task_fee_gwei` MUST be computed with floating-point intermediates and MUST truncate toward zero to a non-negative integer Gwei value, persisted as a decimal integer string where stored as text.

The service MUST resolve `median_priority_gwei` with the Queue Median Hint rules below and MUST persist that value on the job and call record for comparison. That snapshot MUST NOT enter `billable_gwei`. Resolution MUST always produce a value.

### Queue Median Hint

When the service needs a current queue median hint for `billing_config`, WebUI display, project creation default Cost Level, or the job/call-record snapshot:

1. If the latest priority snapshot has a non-null `median_priority_gwei` and `queued_task_count > 0`, the service MUST use that value.
2. Else if the cache still holds the most recent non-empty `median_priority_gwei`, the service MUST use that value.
3. Otherwise the service MUST use `llm.empty_queue_median_priority_gwei`.

The resolved hint MUST be a decimal integer string.

### Estimation Failure

Task Fee Estimation MUST fail when:

1. Relay `GET /v2/models/llm/execution-time` cannot be completed successfully for the request model and effective VRAM; or
2. The returned coefficients fail validation: any of `constant_seconds`, `seconds_per_input_token`, or `seconds_per_output_token` is missing or is not a finite number greater than or equal to zero.

Reading or resolving the queue median hint MUST NOT cause Task Fee Estimation to fail.

After project `priority_gwei`, validated coefficients, and `vram_weight` are available, computing `billable_gwei` and `task_fee_gwei` MUST succeed. Ordinary arithmetic on those validated inputs is not a business failure mode.

When Task Fee Estimation fails, the service MUST:

1. Log the concrete error cause.
2. Create one failure `llm_call_records` row without the four fee fields.
3. Return HTTP 500 to the client.
4. MUST NOT forward the request to the Bridge.

## Billing Config Query API

`GET /v1/llm/billing_config` and `GET /v1/llm/pricing_examples` are specified in [credits-billing.md](./credits-billing.md).
