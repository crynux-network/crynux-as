# OpenAI-Compatible LLM API and Charging

This document specifies the private OpenAI-compatible LLM endpoints, merged model catalog, Crynux model VRAM resolution, Bridge forwarding, project Cost Level (`priority_gwei`), call records, and Crynux Task Fee Estimation. Provider-model compatibility and execution are specified in [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md) and [provider-key-sharing/job-execution.md](./provider-key-sharing/job-execution.md).

Credits charging for LLM calls is specified only in [credits-billing.md](./credits-billing.md).

## Endpoints

Each project exposes a private base URL `/api/<endpoint_token>/v1`.

Implemented endpoints:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/completions`
* `POST /api/<endpoint_token>/v1/responses`
* `GET /api/<endpoint_token>/v1/responses/<response_id>`
* `POST /api/<endpoint_token>/v1/images/generations`
* `POST /api/<endpoint_token>/v1/<vram_limit>/images/generations`
* `POST /api/<endpoint_token>/v1/images/edits`
* `POST /api/<endpoint_token>/v1/<vram_limit>/images/edits`
* `POST /api/<endpoint_token>/v1/<vram_limit>/chat/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/responses`
* `GET /api/<endpoint_token>/v1/models`
* `GET /api/<endpoint_token>/v1/models/<model>`

The chat, completions, and responses request and response formats MUST be OpenAI-compatible for the supported first-version fields. Every configured Provider model MUST support Chat Completions and Responses through the Provider adapter behavior in [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md). Provider models MUST reject Completions. The Images generations and Images edits request and response formats MUST be OpenAI-compatible for the supported fields specified in **Images Generations API** and **Images Edits API**.

Chat Completions MUST accept `chat_template_kwargs` and `reasoning_effort`. Responses MUST accept `chat_template_kwargs` and `reasoning.effort`. For Crynux models, Chat Completions, Completions, and Responses MUST accept optional `dtype` and `quantization_config` body fields used to select a loaded-models row and populate canonical TaskArgs. Provider models MUST reject those Crynux execution fields. Chat Completions, Completions, Responses, Images generations, and Images edits MUST accept optional top-level `queue_timeout` as specified in Queue Timeout. AS MUST map applicable public fields to canonical `template_args` as specified in [model-compatibility/architecture.md](./model-compatibility/architecture.md). `chat_template_kwargs` MUST carry model-template keys such as `enable_thinking` or `thinking`. `reasoning_effort` and `reasoning.effort` MUST only inject `enable_thinking`; they MUST NOT rename keys for other templates.

## Images Generations API

### `POST /api/<endpoint_token>/v1/images/generations`

Creates one Image `task_jobs` row, waits for terminal settle, reads PNG files from `{llm.task_result_dir}/{task_jobs.id}/`, and returns an OpenAI Images response with `b64_json` values encoded from those files for the HTTP response only.

The same create operation is also available at `POST /api/<endpoint_token>/v1/<vram_limit>/images/generations`. The path segment and body field `vram_limit` follow the VRAM Limit and Effective VRAM rules for Images.

AS MUST NOT expose `POST /images/variations` or streaming Images responses.

Supported request fields:

* `prompt` (required; maximum 32000 characters)
* `model` (required; Hugging Face SD model id; normalized with trim and lowercase)
* `n` (optional; 1–10; default 1)
* `size` (optional; `auto`, a named size, or `WIDTHxHEIGHT`)
* `quality` (optional; maps to internal denoising steps)
* `response_format` (optional; only `b64_json` or omitted)
* `output_format` (optional; only `png` or omitted)
* `safety_identifier` and `user` (optional; accepted with no side effects)
* `quantization_config` (optional; object or `null`; written into Image TaskArgs)
* `components` (optional; object or `null`; written into Image TaskArgs)
* `vram_limit` (optional; unsigned integer GB; see VRAM Limit and Effective VRAM)
* `queue_timeout` (optional; unsigned integer seconds; see Queue Timeout)

AS MUST reject unknown fields and the following values with HTTP 400 and the OpenAI invalid-request error shape: `response_format=url`; `output_format` other than `png`; `output_compression`; `background`; `moderation`; `style`; `stream=true`; `partial_images`; `dtype`; and Crynux Raw Task fields such as `steps` or `negative_prompt`.

`size` mapping:

* omitted or `auto` MUST become `1024x1024`
* named sizes MUST accept `256x256`, `512x512`, `1024x1024`, `1024x1536`, `1536x1024`, `1792x1024`, and `1024x1792`
* arbitrary `WIDTHxHEIGHT` MUST require positive integers divisible by 8, aspect ratio in `[1/3, 3]`, each edge at most `3840`, and pixel count at most `3840 * 2160`

`quality` mapping to Image TaskArgs `task_config.steps`:

* `low` → `20`
* omitted, `auto`, `medium`, or `standard` → `30`
* `high`, `hd`, `xhigh`, or `max` → `50`

AS MUST build Image TaskArgs with `version` matching the configured stable-diffusion inference schema, `base_model.name` from `model`, `prompt`, and `task_config` containing `image_width`, `image_height`, `steps`, and `num_images_per_prompt`. The Images request does not accept `seed`. AS MUST set `task_config.seed` to a uniformly chosen integer from 0 through 2147483647 before the job is persisted. When the request includes `quantization_config`, AS MUST copy that value into TaskArgs. When the request includes `components`, AS MUST copy that value into TaskArgs. Before execution resolution, AS MUST lowercase Hugging Face repository names in TaskArgs (`base_model.name` and every `components.*.repo`) with the same rules as Raw Task create. AS MUST NOT accept client `dtype` or `lora` on this endpoint. After building TaskArgs, AS MUST resolve execution fields through the shared Image loaded-models resolver in auto mode: the selected row MUST have `model_id` equal to the request `model`, MUST have `lora` equal to `null`, MUST have `quantization_config` equal to the request value when the request includes `quantization_config`, and MUST have `components` equal to the request value when the request includes `components`. Component comparison MUST use `format`, lowercase `repo`, and `format_kwargs`, and MUST ignore `revision`. When several rows match, AS MUST select the row with the greatest `min_vram`, with ties broken by canonical `quantization_config` ascending, then `execution_dtype` ascending, then `model_cache_hash` ascending. AS MUST write the selected row's concrete `execution_dtype` into TaskArgs `dtype`, and MUST write the selected row's `quantization_config` and `components` into TaskArgs when the request omits them. When no row matches, or the matched row's `execution_dtype` is not concrete (`float16`, `bfloat16`, or `float32`), AS MUST return HTTP 400 with the OpenAI invalid-request error shape and MUST NOT reserve Credits or create a job. When the user specified a `vram_limit`, the effective VRAM MUST be the user value and MUST override the selected row's `min_vram`. When the user did not specify a `vram_limit`, the effective VRAM MUST be the selected row's `min_vram`. AS MUST estimate and settle Credits with the Image pixel-step rules in [credits-billing.md](./credits-billing.md) and [raw-task-api.md](./raw-task-api.md).

Clients MAY send `quantization_config`, `components`, `vram_limit`, and `queue_timeout` through the official OpenAI Python SDK `extra_body` merge or by placing the same top-level fields in the official OpenAI JavaScript SDK request body.

The HTTP response MUST include `created`, `data[].b64_json`, and echoed `size`, `quality`, and `output_format`. The response MUST omit `url`, `revised_prompt`, `usage`, `background`, and `safety_identifier`. AS MUST NOT persist the response body, PNG bytes, or base64 strings on the job row.

## Images Edits API

### `POST /api/<endpoint_token>/v1/images/edits`

Creates one Image `task_jobs` row with non-empty Image TaskArgs `images`, waits for terminal settle, reads PNG files from `{llm.task_result_dir}/{task_jobs.id}/`, and returns an OpenAI Images response with `b64_json` values encoded from those files for the HTTP response only.

The same create operation is also available at `POST /api/<endpoint_token>/v1/<vram_limit>/images/edits`. The path segment and body field `vram_limit` follow the VRAM Limit and Effective VRAM rules for Images.

The request MUST use either `application/json` or `multipart/form-data`.

JSON body input images:

* The request MUST include `images` as a non-empty array of objects.
* Each object MUST provide exactly one of `image_url` or `file_id`.
* `image_url` MUST be a `data:image/<type>;base64,...` data URL. Absolute `http` and `https` URLs MUST be rejected.
* `file_id` MUST be rejected.
* The array MUST contain at least 1 and at most 16 entries.

Multipart form input images:

* The request MUST include one or more file parts named `image` or `image[]`.
* AS MUST encode each uploaded file as a `data:image/<type>;base64,...` data URL using the part Content-Type when it is an `image/*` type, otherwise `image/png`.
* The total number of uploaded image files MUST be at least 1 and at most 16.
* A `mask` file part MUST be rejected.

Supported request fields (JSON body fields, or the same names as multipart text fields):

* `prompt` (required; maximum 32000 characters)
* `model` (required; Hugging Face SD model id; normalized with trim and lowercase)
* `n` (optional; 1–10; default 1)
* `size` (optional; same mapping as Images Generations)
* `quality` (optional; same mapping as Images Generations to `task_config.steps`)
* `response_format` (optional; only `b64_json` or omitted)
* `output_format` (optional; only `png` or omitted)
* `safety_identifier` and `user` (optional; accepted with no side effects)
* `quantization_config` (optional; object or `null`; written into Image TaskArgs)
* `components` (optional; object or `null`; written into Image TaskArgs)
* `vram_limit` (optional; unsigned integer GB; see VRAM Limit and Effective VRAM)
* `queue_timeout` (optional; unsigned integer seconds; see Queue Timeout)

For multipart `quantization_config` and `components`, AS MUST accept either:

* one form field whose value is the JSON object or `null` string; or
* OpenAI Python SDK bracket fields under that root, such as `quantization_config[quant_kwargs][load_in_4bit]` and `quantization_config[components_to_quantize][]`, and MUST reconstruct the nested object before TaskArgs conversion.

Bracket array fields MUST use a final empty `[]` segment. Scalar form values `true`, `false`, and `null` under bracket fields MUST be coerced to JSON boolean or null; integer and floating-point digit strings MUST be coerced to JSON numbers; all other scalars MUST remain JSON strings. AS MUST reject a request that mixes a JSON object form value and bracket fields for the same root. AS MUST reject bracket fields whose root is not `quantization_config` or `components`.

AS MUST reject unknown fields and the following values with HTTP 400 and the OpenAI invalid-request error shape: `mask`; `input_fidelity`; `response_format=url`; `output_format` other than `png`; `output_compression`; `background`; `moderation`; `stream=true`; `partial_images`; `file_id`; non-data-URL `image_url`; `dtype`; `strength`; and Crynux Raw Task fields such as `steps`, `negative_prompt`, or `lora`.

After building Image TaskArgs, AS MUST enforce the Raw Task `task_args.images` size limit: the sum of the byte lengths of every string in `images` as stored in the JSON (including any `data:image/...;base64,` prefix) MUST NOT exceed `1048576` (1mb). AS MUST NOT decode base64 payloads to measure size. When the sum exceeds the limit, AS MUST return HTTP 400 with an invalid-request error whose message is exactly `task_args.images total size exceeds the 1mb limit`, and MUST NOT reserve Credits or create a job.

AS MUST build Image TaskArgs with `version` matching the configured stable-diffusion inference schema, `base_model.name` from `model`, `prompt`, non-empty `images` as data URLs, and `task_config` containing `image_width`, `image_height`, `steps`, and `num_images_per_prompt`. The Images edits request does not accept `seed` or `strength`. AS MUST set `task_config.seed` to a uniformly chosen integer from 0 through 2147483647 before the job is persisted. When the request includes `quantization_config`, AS MUST copy that value into TaskArgs. When the request includes `components`, AS MUST copy that value into TaskArgs. AS MUST NOT accept client `dtype` or `lora` on this endpoint. Image loaded-models row selection, effective VRAM resolution, Credits estimation, and settlement MUST follow the same rules as Images Generations.

Clients MAY send `quantization_config`, `components`, `vram_limit`, and `queue_timeout` through the official OpenAI Python SDK `extra_body` merge or by placing the same top-level fields in the official OpenAI JavaScript SDK request body. Multipart clients MAY send `vram_limit` and `queue_timeout` as form values and MAY send `quantization_config` and `components` either as JSON form strings or as OpenAI SDK bracket-nested form fields.

The HTTP response MUST include `created`, `data[].b64_json`, and echoed `size`, `quality`, and `output_format`. The response MUST omit `url`, `revised_prompt`, `usage`, `background`, and `safety_identifier`. AS MUST NOT persist the response body, PNG bytes, or base64 strings on the job row.

## Responses API

### `POST /api/<endpoint_token>/v1/responses`

Creates a persisted LLM job and returns an OpenAI Responses object.

For Crynux models, the same create operation is also available at `POST /api/<endpoint_token>/v1/<vram_limit>/responses`. The path segment and body field `vram_limit` follow the VRAM Limit and Effective VRAM rules. A Provider model on a path-based VRAM endpoint MUST be rejected.

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
* `max_output_tokens`, `temperature`, `top_p`, `stop`, `seed`, `tool_choice`, `text.format`, and `queue_timeout`
* `vram_limit`, `dtype`, and `quantization_config` for Crynux models only; Provider models MUST reject them
* `chat_template_kwargs`
* `reasoning.effort`

The service MUST reject unsupported fields with HTTP 400 and the OpenAI invalid-request error shape. For Crynux models, unsupported fields include `stream`, `conversation`, built-in tools, `cancel`, `delete`, and `structured_outputs`. Provider models MUST accept or reject `stream` according to [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md); the other unsupported fields remain rejected.

`previous_response_id` MUST follow [llm-job-processing.md](./llm-job-processing.md): only a same-project, still-retained, successfully completed Responses job is accepted; the new job MUST store the fully expanded `TaskArgsJSON`; previous instructions MUST NOT be inherited as this call's instructions.

When `background` is `true`, the handler MUST persist the job and return immediately after the job is stored. The returned `status` MUST be `queued` or `in_progress`. The client MUST poll `GET /responses/<response_id>` until the job reaches a terminal state.

When `background` is `false`, the handler MUST use the same persisted job flow and wait for the job to complete before returning the final Responses object. A terminal `failed` job MUST return HTTP 200 with a Responses object whose `status` is `failed` and whose `error` field contains the failure detail. A terminal `completed` job MUST return the formatted Responses object.

### `GET /api/<endpoint_token>/v1/responses/<response_id>`

Returns the persisted Responses object for the project's response ID. A missing, foreign, or retention-expired response ID MUST return HTTP 404.

Terminal `completed` responses MUST include formatted `output` and `usage` only after the execution-source result, formatted result, and billing settlement are stored. Terminal `failed` responses MUST include an OpenAI-format error object.

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

## Task Job Execution

Chat completions, completions, and responses share the `task_jobs` table and common admission and terminal settlement services. Crynux jobs use the Bridge bounded-batch poller. Provider jobs use the independent Provider scheduler and one goroutine per successfully claimed Shared API Key slot. Table ownership, settle transactions, Responses retention, and Recent Requests merge rules are specified in [llm-job-processing.md](./llm-job-processing.md) and [provider-key-sharing/job-execution.md](./provider-key-sharing/job-execution.md).

For every Crynux LLM request, the service MUST:

1. Parse the public API request into canonical `GPTTaskArgs`, expanding `previous_response_id` history when present. During parsing, the request `model` MUST be normalized with surrounding whitespace trimmed and Hugging Face repository lowercasing (`NormalizeModelID`). Before execution resolution and job persistence, AS MUST also rewrite every Hugging Face model name field inside TaskArgs with the same rules as Raw Task create and Relay task create (LLM `model`; Image `base_model` / component repos and related fields). That normalized value MUST be used for fee estimation, VRAM lookup, `task_jobs.model`, `task_call_records.model`, canonical `task_args.model`, usage stats, and the public response `model` field. Canonical `GPTTaskArgs.version` MUST be `3.7.1`. When the request omits both `dtype` and `quantization_config`, AS MUST select the full-cache row with the greatest `min_vram` for that model and MUST write that row's `quantization_config` and `execution_dtype` into TaskArgs (`dtype` MUST prefer the row's `execution_dtype`, and MUST fall back to the adapter dtype resolver when the row dtype is empty). When the request supplies `dtype` and/or `quantization_config`, AS MUST exact-match a full-cache row when possible and MUST write the request values into TaskArgs. When the request omits `seed`, AS MUST set canonical `seed` to a uniformly chosen integer from 0 through 2147483647 before the job is persisted. When the request includes `seed`, including 0, AS MUST copy that integer unchanged.
2. Create a `task_jobs` row with status `pending_submit` and the fully expanded `TaskArgsJSON`.
3. Let the background worker advance unfinished jobs in batches: submit `pending_submit` jobs to Bridge, query ClientTask status for in-flight jobs, download the raw `GPTTaskResponse` for each successful job, format the public API result, and perform one-time Credits settlement before marking the job `completed`.
4. For chat completions and completions, wait synchronously on the HTTP request until the job reaches a terminal state, then return the formatted JSON body or simulated SSE stream.
5. For responses with `background=false`, wait synchronously on the HTTP request until the job reaches a terminal state.
6. For responses with `background=true`, return immediately after persistence and expose status through `GET /responses/<response_id>`.

A synchronous wait MUST use the following HTTP hold duration:

1. When the request includes a positive `X-Stainless-Timeout` header value in seconds, AS MUST wait that duration plus 15 seconds.
2. Otherwise, when the request includes a positive `X-Stainless-Read-Timeout` header value in seconds, AS MUST wait that duration plus 15 seconds.
3. Otherwise AS MUST wait 10 minutes.

That hold duration controls only when AS stops holding the client HTTP response. It MUST NOT set Relay queue wait, Relay execution timeout, or cancel the persisted job. Client disconnect during a synchronous wait MUST NOT cancel the persisted job or settlement.

The worker MUST keep one loop. Each tick MUST load a bounded batch of unfinished jobs (`pending_submit`, `submitted`, `in_progress`), advance every job in that batch by at most one step, sleep a fixed interval, and start the next tick. The worker MUST NOT wait inside one job until that job reaches a terminal Bridge status while other unfinished jobs remain unprocessed.

A `pending_submit` job whose age since `created_at` is greater than or equal to `llm.job_submit_timeout` seconds MUST be marked `failed` with a failed call record. The worker MUST NOT submit that job to Bridge after the timeout.

The stable `task_jobs.id` MUST be the billing settlement source while the job exists. Repeated polling, synchronous waits, and worker retries MUST NOT create duplicate Credits charges. After retention deletes the job, the permanent call record and any Credits event remain.

## Crynux Forwarding to the Crynux Bridge

The service MUST execute Crynux LLM jobs through the Crynux Bridge raw task APIs using the platform-level Bridge API key from the service configuration (`Authorization: Bearer <api_key>`). Provider jobs MUST not enter this forwarding path. The Bridge API key MUST have the `chat` role.

AS MUST NOT send `client_id` in Bridge raw task requests. Bridge MUST derive the client identity from the API key.

| AS operation | Bridge endpoint |
|--------------|-----------------|
| Create LLM raw tasks | `POST {bridge.base_url}/v1/inference_tasks/batch` |
| Query ClientTask statuses | `POST {bridge.base_url}/v1/inference_tasks/batch/status` |
| Download LLM JSON result | `GET {bridge.base_url}/v1/inference_tasks/<client_task_id>/llm` |

Single-task create and status endpoints MUST remain available on Bridge. The AS worker MUST use the batch create and batch status endpoints.

Each raw task submission MUST create a new Bridge ClientTask. AS MUST NOT send `request_id`. After Bridge returns a client task ID, AS MUST persist it on the LLM job and MUST use that ID for all later status queries and result downloads.

The resolved effective VRAM MUST be sent as `min_vram` on raw task creation.

When the request includes `queue_timeout`, AS MUST persist that value on the job and MUST send it as Bridge raw task `queue_timeout`. When omitted, AS MUST omit the field so Relay applies its configured default queue wait. `queue_timeout` MUST NOT enter TaskArgs. It is independent of project Cost Level / `priority_gwei`.

The LLM job's `task_fee_gwei` MUST be multiplied by `1,000,000,000` with integer arithmetic and sent as the required raw task `task_fee` in Wei. AS MUST NOT send the stored GWei value directly. Bridge MUST treat this value as the final task fee and MUST NOT apply task-size multiplication or unit conversion.

Bridge MUST return its whole-client-task aggregated `status` as one of `running`, `success`, or `failed`, and MUST also return a computed logical `phase` as specified by the Bridge raw status contract. AS MUST treat only `success` and `failed` as terminal. AS MUST NOT aggregate individual Bridge inference-task statuses. For Raw Task API responses, AS MUST persist and expose `phase` and optional `estimated_completion_at` as specified in [raw-task-api.md](./raw-task-api.md).

Bridge status queries and result download MUST remain authorized after the Bridge API key's creation quota is exhausted. Invalid, expired, wrong-role, and cross-client access MUST remain rejected by Bridge.

AS owns OpenAI-compatible request parsing, raw `GPTTaskResponse` normalization into chat completions, completions, and responses output shapes, and simulated SSE for Crynux chat completions and completions. Bridge OpenAI `/v1/llm/*` endpoints are not used by AS.

For streaming Crynux chat or completions requests (`stream: true`), the service MUST return simulated server-sent events after the persisted job completes. Provider Chat Completions and Responses requests MUST stream normalized upstream output in real time according to [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md). When the client sets `stream_options.include_usage: true`, the final chunk MUST include `usage`.

## Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. The `Authorization: Bearer <token>` header, where `<token>` MUST be either:
   * The project API key, which MUST match the located project's stored key hash, or
   * A valid JWT issued by Crynux AS for the wallet address that owns the located project.

The project MUST have status active. A request failing any of these checks MUST be rejected with HTTP 401. The project API key and an owner JWT MUST grant the same permission to call the project's private endpoints.

## Model Catalog

The public model catalog merges the in-memory Crynux LLM catalog snapshot with currently available configured Provider models. All projects share the same catalog. A Crynux cached row's `min_vram` is the minimum VRAM in GB observed across historical successful executions of that model configuration on the Crynux Network. The Crynux catalog MUST expose one list row per `(model_cache_hash, execution_dtype)` row from the Relay snapshot. The same `id` MAY appear more than once when quantization or dtype differs. SD / Image rows MUST NOT appear in this catalog.

Provider rows MUST come only from YAML declarations. A Provider row MUST appear only when its Service Provider has at least one enabled, non-deleted, system-valid Shared API Key. Busy slots and temporary cooldown MUST NOT hide a model. Provider model IDs MUST be exact official names without prefixes or aliases. Provider rows MUST use the field shapes in [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md). List and retrieve responses MUST remain OpenAI Models API compatible and MUST NOT expose key availability counts, Shared API Key identity, `model_cache_hash`, or `model_cache_config`.

The Crynux catalog snapshot MUST include only rows whose Relay `on_disk_node_count` is greater than or equal to `llm.min_catalog_on_disk_node_count`. The full loaded-models cache MUST retain every Relay LLM row for default `min_vram` and exact-configuration lookup. Catalog list and retrieve MUST read the catalog snapshot and MUST NOT re-filter the full cache on each request. The merged list MUST sort by `id` ascending, then execution source with Crynux before Provider, then Crynux `execution_dtype`, canonical `quantization_config`, and `model_cache_hash` ascending. Provider rows have no Crynux tie-breaker fields.

### `GET /api/<endpoint_token>/v1/models`

Returns the OpenAI list-models response built from the catalog snapshot:

```json
{
  "object": "list",
  "data": [
    {
      "id": "qwen/qwen3.6-7b",
      "object": "model",
      "created": 0,
      "owned_by": "crynux",
      "min_vram": 24,
      "on_disk_node_count": 3,
      "execution_dtype": "bfloat16",
      "quantization_config": null
    }
  ]
}
```

Requirements:

* The root `object` MUST be `"list"` and `data` MUST be an array.
* Each item MUST contain `id`, `object` (`"model"`), `created`, and `owned_by`.
* For a Crynux row, `id` MUST be the lowercase Hugging Face model ID from the Relay `model_id`. For a Provider row, `id` MUST be the exact configured official model name.
* `created` MUST be `0`; the Relay provides no creation time.
* `owned_by` MUST be `"crynux"` for Crynux rows and the Service Provider ID for Provider rows.
* A Crynux item MUST include `min_vram`, `on_disk_node_count`, `execution_dtype`, and `quantization_config` as specified above. A Provider item MUST omit these Crynux execution fields.
* Each item MUST NOT include `model_cache_hash` or `model_cache_config`.
* Each item MUST NOT include `in_memory_node_count`.
* The response MUST be the OpenAI JSON body directly, without the management-API `{"message": ...}` envelope.
* The legacy OpenAI fields `permission`, `root`, and `parent` MUST NOT be required.

### `GET /api/<endpoint_token>/v1/models/<model>`

Returns an OpenAI list envelope whose `data` contains every catalog row for that model ID. The lookup MUST be case-insensitive. When no catalog row matches, including when the model is present only in the full loaded-models cache because every row's `on_disk_node_count` is below `llm.min_catalog_on_disk_node_count`, the response MUST be HTTP 404 with the OpenAI error shape:

```json
{
  "error": {
    "message": "The model '<model>' does not exist",
    "type": "invalid_request_error",
    "code": "model_not_found"
  }
}
```

## Model ID Validation

Chat Completions, Completions, and Responses create requests MUST route and validate the request `model` before Credits reservation and job creation.

1. AS MUST trim surrounding whitespace and first exact-match the remaining case-sensitive value against the configured Provider model map.
2. On a Provider match, AS MUST select that Service Provider and route Chat Completions or Responses through its adapter. A Provider model on Completions MUST return HTTP 400. AS MUST NOT lowercase, prefix, alias, or query Hugging Face for that model.
3. Without a Provider match, AS MUST normalize the value with Hugging Face repository lowercasing (`NormalizeModelID`).
4. A Crynux model MUST be a Hugging Face model ID. Absolute URLs and any value containing `://` MUST be rejected with HTTP 400. Values with whitespace or characters outside the Hugging Face repo-id pattern MUST be rejected with HTTP 400.
5. When any row for the normalized Crynux model is present in the full LLM loaded-models cache, AS MUST skip the Hub lookup.
6. When the Crynux model is absent from that cache, AS MUST query Hugging Face Hub `GET {huggingface.api_base_url}/api/models/{model}`. A missing model MUST return HTTP 400. Hub timeouts and HTTP 5xx MUST return HTTP 500 and MUST NOT reserve Credits or create a job.

The catalog snapshot used by `GET /models` MUST NOT be used as a create-time allowlist.

## VRAM Limit and Effective VRAM

### Queue Timeout

Chat completions, completions, responses, images generations, and images edits MAY include top-level `queue_timeout` as an unsigned integer number of seconds. When present, the value MUST be greater than zero. Clients MAY send it through the official OpenAI Python SDK `extra_body` merge or as a top-level field in the official OpenAI JavaScript SDK request body.

For a Crynux model, `queue_timeout` sets only the Relay queue wait budget for the submitted task. For a Provider model, it sets only the Shared API Key slot wait budget and defaults as specified in [provider-key-sharing/job-execution.md](./provider-key-sharing/job-execution.md). It MUST NOT change Cost Level or Credits estimation.

### Crynux User-specified `vram_limit`

A Crynux chat, completions, responses, images generations, or images edits request specifies the VRAM limit in GB through either:

1. The URL path segment `<vram_limit>` (`POST .../v1/<vram_limit>/chat/completions`, `POST .../v1/<vram_limit>/completions`, `POST .../v1/<vram_limit>/responses`, `POST .../v1/<vram_limit>/images/generations`, or `POST .../v1/<vram_limit>/images/edits`).
2. The body field `vram_limit` (unsigned integer).

The path value MUST override the body value. A non-integer path value MUST be rejected with HTTP 400. When neither is set, the user has not specified a VRAM limit.

### Crynux Effective VRAM Resolution

For Crynux chat, completions, and responses, the effective VRAM of a request MUST be resolved as:

1. When the user specified a `vram_limit`, the effective VRAM is the user value. A user value lower than the cached `min_vram` of a known model MUST be accepted.
2. When the user did not specify a `vram_limit` and the request includes `quantization_config` and/or `dtype` that exact-match a full-cache row for the model, the effective VRAM is that row's `min_vram`.
3. When the user did not specify a `vram_limit` and the model has one or more rows in the full loaded-models cache without an exact match above, the effective VRAM is the greatest `min_vram` among those rows. Ties MUST break by canonical `quantization_config` ascending, then `execution_dtype` ascending, then `model_cache_hash` ascending.
4. When the user did not specify a `vram_limit` and the model is not in the cache, the effective VRAM is `llm.default_vram_limit`.

For images generations and images edits, the effective VRAM MUST be resolved as:

1. When the user specified a `vram_limit`, the effective VRAM is the user value. A user value lower than the Image loaded-models cache `min_vram` MUST be accepted.
2. When the user did not specify a `vram_limit`, the effective VRAM is the `min_vram` of the Image loaded-models row selected by the Images Generations auto-resolution rules above.

The model lookup MUST be case-insensitive. The effective VRAM is used for `vram_weight`, Relay execution-time selection (`min_vram`), and Bridge raw task `min_vram`.

## Project Cost Level

Each project stores a Cost Level configuration that determines the effective `priority_gwei` used for Credits and the submitted task fee as specified in [credits-billing.md](./credits-billing.md) and Task Fee Estimation.

### Stored Fields

| Field | Required | Meaning |
|-------|----------|---------|
| `cost_level_mode` | Yes | `static` or `auto`. Default at create and for migrated existing projects: `static`. |
| `priority_gwei` | Yes | Static-mode Cost Level as a decimal integer Gwei string. Independent of auto fields. |
| `auto_queue_position` | Nullable until Auto is configured | Integer in `[0, 100]`. Default at create: `50`. |
| `auto_max_credits_per_task` | Nullable until Auto is configured | Auto-mode upper Credits cap as a positive decimal integer string. |

Switching mode MUST NOT overwrite the other mode's stored values. Project update MUST reject `cost_level_mode` values other than `static` or `auto`. Project update MUST reject a `priority_gwei` outside the inclusive range `[llm.min_priority_gwei, llm.max_priority_gwei]`. Project update MUST reject an `auto_max_credits_per_task` that is not a positive decimal integer. Project update MUST reject an `auto_queue_position` outside `[0, 100]`. Setting `cost_level_mode` to `auto` MUST be rejected when `auto_max_credits_per_task` is missing or invalid after applying the same update payload.

### Create Defaults

When a project is created, the service MUST:

1. Set `cost_level_mode` to `static`.
2. Set `priority_gwei` to the Queue Median Hint resolved by `ResolveQueueMedianHint`, clamped into `[min_priority_gwei, max_priority_gwei]`.
3. Set `auto_queue_position` to `50`.
4. Set `auto_max_credits_per_task` from `llm.default_auto_max_credits_per_task`.

The create request MUST NOT require the client to supply Cost Level fields. After creation, only an explicit project update MUST change Cost Level fields.

The migration that replaces `auto_max_priority_gwei` with `auto_max_credits_per_task` MUST backfill every existing project to `100` and MUST drop `auto_max_priority_gwei`.

### Effective Priority Resolution

Before Credits precheck and Task Fee Estimation for an LLM request, the service MUST resolve one uncapped target `priority_gwei` for that request:

1. When `cost_level_mode` is `static`, the effective value MUST be the project's stored `priority_gwei`.
2. When `cost_level_mode` is `auto`:
   1. If the live queued-priority snapshot has `queued_task_count > 0`, both `lowest_priority_gwei` and `highest_priority_gwei` are present, `lowest_priority_gwei` is non-negative, `highest_priority_gwei` is positive, and `highest >= lowest`, compute:
      ```text
      effective = lowest + floor((highest - lowest) * auto_queue_position / 100)
      ```
      using integer arithmetic. The result MUST lie in `[lowest, highest]`. `lowest_priority_gwei` MAY be `0` when Relay truncates a sub-Gwei queue floor to zero; that value remains a usable bound.
   2. Otherwise the effective value MUST be `llm.min_priority_gwei`.
   3. The service MUST clamp the result into `[llm.min_priority_gwei, llm.max_priority_gwei]`.
   4. This resolution MUST NOT apply `auto_max_credits_per_task`. The Max Credits cap is applied later during task-fee estimation for the concrete request workload, as specified in [credits-billing.md](./credits-billing.md).
3. When `cost_level_mode` is `auto` and `auto_max_credits_per_task` is missing, the request MUST fail as a server error and MUST NOT forward to Bridge.

The final create-time `priority_gwei` after Max Credits capping and minimum-fee raise MUST be snapshotted onto the LLM job and call record. Settle MUST use that snapshot and MUST NOT re-resolve from the project or live queue. `auto_max_credits_per_task` MUST NOT be written into `billing_data` and MUST NOT change settlement formulas.

Project read APIs MUST return `cost_level_mode`, `priority_gwei`, `auto_queue_position` (null when unset), and `auto_max_credits_per_task` (null when unset). `priority_gwei` and `auto_max_credits_per_task` MUST be decimal integer strings when present. Project update MUST be a partial update: omitted Cost Level fields MUST remain unchanged, and a rename that omits Cost Level fields MUST NOT require or clear them.

## LLM Charging Rules

Credits charging, available-Credits precheck and soft lock, settle, Credits configuration, failed-call non-charging rules, and the pricing-examples management API are specified only in [credits-billing.md](./credits-billing.md).

The LLM-related service configuration items that remain shared with Task Fee Estimation and request handling are:

```yaml
huggingface:
  api_base_url: "https://huggingface.co"
  timeout_seconds: 5
  cache_ttl_seconds: 300
llm:
  default_max_tokens: 2048
  default_vram_limit: 24
  loaded_models_refresh_interval: 1800
  queued_priority_refresh_interval: 300
  execution_time_cache_ttl: 300
  min_catalog_on_disk_node_count: 3
  base_vram: 8
  empty_queue_median_priority_gwei: "34"
  min_priority_gwei: "1"
  max_priority_gwei: "1000000000"
  credits_per_gwei: "1"
  max_task_price_cnx:
    crynux: "0.001"
    openai: "0.001"
    anthropic: "0.001"
    google: "0.001"
  min_task_price_cnx:
    crynux: "0"
    openai: "0"
    anthropic: "0"
    google: "0"
  job_submit_timeout: 600
  job_retention_days: 30
  task_result_dir: "data/task_results"
  project_recent_requests_limit: 50
```

* `huggingface.api_base_url`, `huggingface.timeout_seconds`, and `huggingface.cache_ttl_seconds` configure create-time Hub model validation. Configuration loading MUST fail when any of these values is missing or non-positive where required. Hub not-found results MAY be cached for `cache_ttl_seconds`; Hub unavailable results MUST NOT be cached.
* `default_max_tokens` is used in the pre-forward balance estimate and Task Fee Estimation when the request omits both `max_tokens` and `max_completion_tokens`.
* `default_vram_limit` is the effective VRAM in GB for an unknown model when the user did not specify a `vram_limit`.
* `loaded_models_refresh_interval` is the loaded-models cache refresh interval in seconds. Every YAML configuration template MUST set it to `1800`.
* `queued_priority_refresh_interval` is the queued-task priority snapshot refresh interval in seconds. Every YAML configuration template MUST set it to `300`.
* `execution_time_cache_ttl` is the per-key TTL in seconds for LLM execution-time coefficients cached from Relay. Every YAML configuration template MUST set it to `300`.
* `min_catalog_on_disk_node_count` is the minimum Relay `on_disk_node_count` required for a model to enter the public LLM and Image catalog snapshots used by `GET /models`, `GET /models/<model>`, and `GET /models/image`. Every YAML configuration template MUST set it to `3`. Configuration loading MUST fail when the value is zero or missing. A changed value MUST take effect on the next successful loaded-models refresh.
* `base_vram` is the VRAM weight base in GB used by Task Fee Estimation and Credits. Operators MUST keep it aligned with Relay `task_pricing.base_vram`. Every YAML configuration template MUST set it to `8`.
* `empty_queue_median_priority_gwei` is the median priority hint used when the queued-priority cache has never observed a non-empty queue, and when creating a project with no usable live or remembered median. It MUST NOT enter `billable_gwei` except when it becomes the project's stored `priority_gwei` at project creation through the Queue Median Hint rules. Every YAML configuration template MUST set it to a positive decimal integer string. Every Gwei-denominated LLM configuration and response field whose name ends in `_gwei` MUST use a decimal integer string.
* `min_priority_gwei` and `max_priority_gwei` are the hard bounds for project Cost Level values. They MUST be positive decimal integer strings. Configuration loading MUST fail when `min_priority_gwei` is greater than `max_priority_gwei`.
* `credits_per_gwei` is specified in [credits-billing.md](./credits-billing.md). It MUST be a positive decimal string.
* `max_task_price_cnx` is specified in [credits-billing.md](./credits-billing.md). It MUST be a map of provider ID to positive decimal CNX string, MUST include `crynux`, and MUST include one entry for every configured Service Provider. Configuration loading MUST fail when any required key is missing, any value is non-positive, or any value does not convert to a positive Gwei amount. Every YAML configuration template MUST set it.
* `min_task_price_cnx` is specified in [credits-billing.md](./credits-billing.md). It MUST be a map of provider ID to non-negative decimal CNX string with the same keys as `max_task_price_cnx`. A value of `0` disables the admission floor for that provider. Static Cost Level requests below the floor MUST be rejected. Auto Cost Level requests MUST first lower the queue-target priority so estimated Credits stay within `auto_max_credits_per_task`, then raise priority within that Credits budget when needed to meet the floor; when that budget cannot meet the floor the request MUST be rejected. Configuration loading MUST fail when any required key is missing, any value is negative, any value does not convert to a non-negative Gwei amount, or any provider's minimum exceeds that provider's maximum. Every YAML configuration template MUST set it.
* `default_auto_max_credits_per_task` MUST be a positive decimal integer Credits string. It is the create default and the WebUI default for Auto Max Credits per task.
* `job_submit_timeout` is the maximum age in seconds of a `pending_submit` LLM job before the worker stops Bridge submit retries and marks the job failed. Every YAML configuration template MUST set it to a positive integer.
* `job_retention_days` is the number of days a terminal settled job remains readable for Responses lookup and `previous_response_id`. Every YAML configuration template MUST set it to a positive integer. Example configurations MUST use `30`. After that window, retention cleanup MUST delete the `task_jobs` row and the matching `{task_result_dir}/{id}` directory when present, and MUST NOT delete `task_call_records`, `credit_events`, or `credit_accounts`.
* `task_result_dir` is the local directory root for Image task PNG files at `{task_result_dir}/{task_jobs.id}/{index}.png`. Every YAML configuration template MUST set it to a non-empty path. Example configurations MUST use `data/task_results`. Configuration loading MUST fail when it is missing or empty.
* `project_recent_requests_limit` is the maximum number of Recent Requests rows returned for a project. Every YAML configuration template MUST set it to a positive integer.

Configuration loading MUST fail when any required LLM configuration value is zero or missing, including the Credits and Cost Level bound fields required by [credits-billing.md](./credits-billing.md). Configuration MUST NOT include `reference_priority_gwei` or `max_token_ratio`.

### Call Record Fields

Call-record ownership and the Credits amount ledger are specified in [llm-job-processing.md](./llm-job-processing.md).

Each text `task_call_records` row MUST contain:

* `project_id`
* `model`
* `prompt_tokens`, `completion_tokens`, `total_tokens`
* `priority_gwei` (the effective Cost Level used for the charge, as a decimal integer string)
* `status` (success or failed)
* `duration_ms`
* `billed_vram` (the resolved effective VRAM in GB used for VRAM weight and Bridge forwarding for a Crynux row; null for a Provider row)

When Task Fee Estimation succeeds before Bridge forwarding, a success `task_call_records` row MUST also contain:

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

When Task Fee Estimation fails and the request is aborted before Bridge forwarding, the failure `task_call_records` row MUST leave the four fee fields empty. Management query APIs MUST NOT expose the fee fields or recalculation snapshot fields.

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

When `task_fee_gwei` would exceed `floor(llm.max_task_price_cnx.crynux * 1e9)`, both the submitted `task_fee_gwei` and the Credits `billable_gwei` MUST be clamped as specified in [credits-billing.md](./credits-billing.md). The job and call-record `priority_gwei` snapshot MUST remain the uncapped effective Cost Level.
```

* `estimated_prompt_tokens` and `max_completion_tokens` MUST be the same values used by the Credits available-Credits precheck and soft lock.
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
2. Create one failure `task_call_records` row without the four fee fields.
3. Return HTTP 500 to the client.
4. MUST NOT forward the request to the Bridge.

## Billing Config Query API

`GET /v1/llm/billing_config`, `GET /v1/llm/pricing_examples`, and `GET /v1/llm/image_pricing_examples` are specified in [credits-billing.md](./credits-billing.md).
