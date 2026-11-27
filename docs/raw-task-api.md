# Raw Task API

## Scope and Authentication

Each project MUST expose the Raw Task API under `/api/<endpoint_token>/v1`. Every request MUST use `Authorization: Bearer <token>`, where `<token>` MUST be either the project API key or a valid JWT issued by Crynux AS for the wallet address that owns the project. The endpoint token and credentials MUST identify the same active project. The project API key and an owner JWT MUST grant the same permission to call the project's Raw Task endpoints.

The API MUST expose:

* `POST /inference_tasks`
* `POST /inference_tasks/estimate`
* `GET /inference_tasks/:client_task_id`
* `POST /inference_tasks/:client_task_id/abort`
* `GET /inference_tasks/:client_task_id/llm`
* `GET /inference_tasks/:client_task_id/images/:index`
* `GET /inference_tasks/:client_task_id/video`
* `POST /inference_tasks/batch`
* `POST /inference_tasks/batch/status`
* `POST /inference_tasks/batch/abort`
* `GET /models`
* `GET /models/chat`
* `GET /models/image`
* `GET /models/video`
* `GET /cost_level`
* `PUT /cost_level`
* `GET /cost_level/hints`

Crynux AS MUST also expose unauthenticated result download routes outside the project prefix:

* `GET /api/result/:result_download_hash/images/:index`
* `GET /api/result/:result_download_hash/video`

The public `client_task_id` MUST be the Crynux AS `task_jobs.id`. A Bridge ClientTask ID, Bridge client ID, or platform client ID MUST NOT appear in a public response. Every authenticated lookup and authenticated result download MUST select by both project ID and local task ID.

## Task Creation

`POST /inference_tasks` MUST accept the Bridge raw-task fields except `task_fee`:

```json
{
  "task_args": "{\"version\":\"3.7.1\",\"model\":\"example/model\",\"messages\":[]}",
  "task_type": 1,
  "min_vram": 24,
  "queue_timeout": 43200
}
```

`task_type` MUST be `0` for an Image task, `1` for an LLM task, or `3` for a Video task. Any other value, including `2`, MUST be rejected with HTTP 400. `task_args` MUST be a JSON string valid for the selected task type. The task runner version MUST be `task_args.version` and MUST be preserved unchanged for LLM, Image, and Video Raw tasks.

The request MAY include optional `queue_timeout` as an unsigned integer number of seconds. When present, the value MUST be greater than zero. AS MUST persist it on the job and MUST forward it to Bridge as raw task `queue_timeout`. When omitted, AS MUST omit the field so Relay applies its configured default queue wait. `queue_timeout` MUST NOT be placed inside `task_args` and MUST NOT change Cost Level or Credits estimation.

Before Credits reservation, AS MUST validate `task_args` against the configured JSON Schema for the task type (`task_schema.gpt_inference` for LLM, `task_schema.stable_diffusion_inference` for Image, `task_schema.video_inference` for Video). A schema validation failure MUST return HTTP 400 with a validation error on field `task_args` and MUST NOT reserve Credits or create a job.

After schema validation, model-name normalization, execution-parameter completion, and any local history expansion succeed, and before Credits reservation, AS MUST validate the final TaskArgs `quantization_config` for worker compatibility as specified in [model-compatibility/architecture.md](./model-compatibility/architecture.md). A failure MUST return HTTP 400 with an invalid-request error on `task_args` and MUST NOT reserve Credits or create a job.

After that check succeeds, AS MUST measure the final TaskArgs JSON byte length with `len(task_args)`. When that length exceeds `llm.max_crynux_task_args_bytes`, AS MUST return HTTP 400 with an invalid-request error whose message is exactly `task_args size exceeds the <limit>-byte limit`, and MUST NOT reserve Credits or create a job. The configured `llm.max_crynux_task_args_bytes` MUST be strictly less than `http.max_body_bytes`.

When the validated TaskArgs omit the generation seed, AS MUST set one before the job is persisted. An LLM task uses top-level `seed`. Image and Video tasks use `task_config.seed`. The value MUST be a uniformly chosen integer from 0 through 2147483647. A seed present in the client JSON, including 0, MUST be stored unchanged. The persisted TaskArgs JSON is the JSON submitted to Bridge.

After schema validation and before model validation, fee estimation, execution resolution, and Credits reservation, AS MUST normalize Hugging Face model name fields in TaskArgs the same way Relay does at task creation: trim is not applied inside repository strings; each Hugging Face repository name is lowercased; values that start with `http://` or `https://` are left unchanged. For an LLM task this MUST rewrite `task_args.model`. For an Image task this MUST rewrite `task_args.base_model` (string form or `base_model.name`), `lora.model`, `controlnet.model`, `refiner.model`, `unet`, `vae`, `textual_inversion`, and every `components.*.repo`. For a Video task this MUST rewrite `task_args.base_model` (string form or `base_model.name`), every `components.*.repo`, and every `loras[].model`. The normalized TaskArgs JSON MUST be the JSON used for Hub checks, loaded-models matching, job persistence, Bridge submit, call records, and usage stats. The job `model` field MUST equal the normalized LLM `model` or the normalized Image or Video `base_model.name`.

Before Credits reservation, AS MUST also validate the task model:

1. For an LLM task, the normalized `task_args.model` value MUST be a Hugging Face model id. For an Image or Video task, the normalized `task_args.base_model.name` MUST be a Hugging Face model id. Image model cache identity is the local `model_cache_hash` together with `dtype`, derived from `base_model`, `components`, `lora`, `dtype`, and `quantization_config`. Video model cache identity is the local `model_cache_hash` together with `dtype`, derived from `base_model`, `components`, `loras`, `dtype`, and `quantization_config`.
2. Absolute URLs and any model id containing `://` MUST be rejected with HTTP 400.
3. When any row for the model is present in the corresponding full loaded-models cache (LLM, Image, or Video keyed by `(model_cache_hash, dtype)`), AS MUST skip the Hub lookup.
4. When the model is absent from that cache, AS MUST query Hugging Face Hub. LLM validation MUST require the repo to exist. Image validation MUST require `model_index.json`. Video validation MUST require `model_index.json` or `modular_model_index.json` at the repository root or in a first-level subdirectory. A permanent Hub rejection MUST return HTTP 400. Hub timeouts and HTTP 5xx MUST return HTTP 500 and MUST NOT reserve Credits or create a job.

Hardware selection MUST use `min_vram` only. A request that includes `required_gpu` or `required_gpu_vram`, including when only one of the two fields is present, MUST return HTTP 400 and MUST NOT reserve Credits or create a job.

After schema validation and model-name normalization, and before Credits reservation, AS MUST resolve the TaskArgs execution fields. TaskArgs `dtype` MUST be a concrete dtype: `float16`, `bfloat16`, or `float32`, matched case-insensitively and persisted in lowercase. Any other present value, including `auto`, `null`, or a non-string value, MUST be rejected with HTTP 400 and MUST NOT reserve Credits. Batch create MUST apply that rejection per item. The persisted TaskArgs MUST always contain a concrete `dtype`.

* For an LLM task, a present `dtype` and a present non-null `quantization_config` MUST be kept unchanged. When either is omitted, AS MUST fill it from the full-cache row that matches the model id and every supplied field, selecting the greatest `min_vram` with ties broken by canonical `quantization_config` ascending, then `dtype` ascending, then `model_cache_hash` ascending. When no row matches, an omitted `dtype` MUST become `bfloat16` and an omitted `quantization_config` MUST become `null`.
* For an Image or Video task, AS MUST run the shared diffusion execution resolver. When `dtype` is present, AS MUST persist every client execution field unchanged. When `dtype` is omitted, AS MUST select the full-cache row with the greatest `min_vram` (same tie-break order) whose identity matches the model id and every supplied `quantization_config` and `components` value. Image rows MUST also match the request `lora`, and a request without `lora` MUST match only `lora=null` rows. Video rows MUST also match the request `loras` set, and a request without `loras` MUST match only rows without LoRAs. On a match, AS MUST write that row's `dtype` and MUST fill an omitted `quantization_config` and an omitted `components` from that same row; AS MUST NOT combine fields from different rows. When no row matches, AS MUST write `dtype` `bfloat16` and MUST keep every submitted field unchanged.

When `min_vram` is omitted, the default MUST be the `min_vram` of the row matched by execution resolution. For an LLM task without a matched row, the default MUST be the greatest `min_vram` among full-cache rows for the model id, else `llm.default_vram_limit`. For an Image or Video task with a client `dtype`, only an exact full-identity row MAY supply the default. An Image or Video task without a matched row MUST use `llm.default_vram_limit`. The resolved TaskArgs JSON MUST pass schema validation for its task type again and MUST be the JSON submitted to Bridge.

`task_fee` is owned by Crynux AS. A request containing `task_fee`, including a null value, MUST be rejected. AS MUST resolve the project Cost Level, fetch the applicable Relay execution-time coefficients, calculate the task fee in Wei and the Credits estimate, then in one transaction reserve that estimate against available Credits (`balance - locked`) and create a `task_jobs` row with a `held` Credits lock, without waiting for Bridge. When request validation, fee estimation, or Credits reserve fails before a `task_jobs` row is created, AS MUST return the corresponding HTTP error and MUST NOT write a `task_call_records` row. Insufficient available Credits MUST return HTTP 402.

After the job row is created, AS MUST evaluate Crynux node model inventory from the full loaded-models cache using the same readiness rules as [llm-api.md](./llm-api.md) (LLM by normalized `model_id` with any `on_disk_node_count >= 1`; Image and Video by normalized base `model_id` plus matching `components` identity with any `on_disk_node_count >= 1` in the cache of the same task type; `lora` and `loras` ignored). When the model is not ready, create MUST return HTTP 400 with:

```json
{
  "error": {
    "message": "This model is not yet available on any node in the network. The network has notified nodes to start downloading it. Downloads take some time, so this task cannot complete on time. Please try again in <minutes> minutes.",
    "type": "invalid_request_error",
    "code": "model_not_ready",
    "param": "model"
  },
  "client_task_id": 123
}
```

`<minutes>` MUST follow `llm.loaded_models_refresh_interval` as in [llm-api.md](./llm-api.md). `client_task_id` MUST be the local `task_jobs.id`. The job MUST remain queued for Bridge submission; AS MUST NOT mark it failed for this response alone. Later GET/status responses MUST reflect the real job state.

When the model is ready, the response MUST use the local task view:

```json
{
  "message": "success",
  "data": {
    "id": 123,
    "task_type": 1,
    "status": "running",
    "phase": "queued",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
}
```

Public status MUST be `running`, `success`, or `failed`. Pending submission, submitted, and in-progress jobs MUST map to `running`. When status is `failed` and the job has a stored error message, the task view MUST include `error_message` with that text. Running and success responses MUST omit `error_message`.

The task view MUST include `phase`. Allowed values are `queued`, `running`, `validating`, `uploading`, `finalizing`, `success`, and `failed`. When `status` is `running`, `phase` MUST be one of `queued`, `running`, `validating`, `uploading`, or `finalizing`. When `status` is `success`, `phase` MUST be `success`. When `status` is `failed`, `phase` MUST be `failed`. A newly created job MUST use `phase` `queued` until the background worker advances it from Bridge status. When `phase` is `running` and an estimated execution completion time is known, the task view MUST include `estimated_completion_at`. Otherwise the task view MUST omit `estimated_completion_at`.

For an Image or Video job, create MUST generate one unguessable random `result_download_hash` (cryptographically random, at least 32 bytes encoded as lowercase hex) and persist it on the `task_jobs` row before returning. Create, get, batch status, and abort task views for that job MUST include the same `result_download_hash` string. The hash MUST NOT be the local task id, Bridge client task id, or any Bridge/Relay `task_id_commitment`. LLM jobs MUST omit `result_download_hash`.

When status is `success`, the task view MUST also include ready-to-use public download URLs built by AS from configured `http.public_origin` and that hash. Clients MUST use these fields and MUST NOT assemble download URLs themselves:

* Image: `result_download_urls` is a JSON array of absolute URLs, one per image index from `0` through `num_images_per_prompt - 1` (or Provider `n - 1`), each equal to `{http.public_origin}/api/result/{result_download_hash}/images/{index}`.
* Video: `result_download_url` is the absolute URL `{http.public_origin}/api/result/{result_download_hash}/video`.

`http.public_origin` is specified in [configuration.md](./configuration.md). Non-success Image and Video views MUST omit `result_download_urls` and `result_download_url`. LLM views MUST omit both fields.

## Task Credits Estimate

`POST /inference_tasks/estimate` MUST accept the same body fields as create for `task_args`, `task_type` (`0`, `1`, or `3`), and optional `min_vram`. Optional `queue_timeout` MUST be validated with the same rules as create when present, and MUST NOT appear in the estimate response. A body that includes `task_fee` MUST be rejected. The request MUST NOT accept Cost Level override fields; estimation MUST use the project's current Cost Level settings.

AS MUST run the same create preparation path used before Credits reservation: schema validation, model-name normalization, execution resolution, Hub checks when needed, Relay execution-time coefficients, fee and Credits estimation, and Cost Level admission (including Max Credits and minimum fee). Preparation failures MUST return the same HTTP 400 messages as create preparation. Estimate MUST NOT return HTTP 402.

Estimate MUST NOT create a `task_jobs` row, MUST NOT write call records, and MUST NOT change `credit_accounts.locked`. AS MUST read available Credits as `balance - locked` (clamped at zero) and set `sufficient` to whether available Credits are greater than or equal to `estimated_credits`. Insufficient available Credits MUST still return HTTP 200 with `sufficient` false.

Estimate MUST NOT apply the create-time `model_not_ready` gate. Response `data` MUST include a read-only `model_ready` boolean using the same inventory rules as create, and MUST NOT block estimation when false.

On success the response MUST be:

```json
{
  "message": "success",
  "data": {
    "estimated_credits": "123",
    "priority_gwei": "34000",
    "task_fee_gwei": "123",
    "task_fee_wei": "123000000000",
    "estimated_node_seconds": 1.5,
    "billed_vram": 20,
    "vram_weight": 2.5,
    "model": "org/llm",
    "model_ready": true,
    "available_credits": "999900",
    "sufficient": true,
    "prompt_tokens_estimate": 10,
    "completion_tokens_estimate": 256
  }
}
```

`estimated_credits`, `priority_gwei`, `task_fee_gwei`, and `task_fee_wei` MUST match the create-time values that create would use to lock Credits for the same request under the current Cost Level. `prompt_tokens_estimate` and `completion_tokens_estimate` MUST be present for LLM estimates only, using the same precheck rules as create: `(utf8(task_args) + 3) / 4` and `generation_config.max_new_tokens` or the configured default. Image and Video estimates MUST omit those two fields. This estimate is create-time only. Image and Video settlement uses the same create-time Credits; LLM settlement MAY differ when actual token usage differs from the precheck estimates.

## Batch Operations

`POST /inference_tasks/batch` MUST accept `{"tasks":[...]}` with between 1 and 100 items. AS MUST validate and price items in request order. The response MUST contain one item result per input index in the same order. One invalid item MUST NOT prevent valid items from being accepted. Each item MUST contain `index` and either `client_task` or `error`. A model-not-ready item MUST use `error` with the model-not-ready message and MUST include `client_task_id` for the created job; it MUST NOT include `client_task`.

The batch path MUST reserve each accepted item's estimated Credits in request order through the same per-item reserve-and-create transaction used by single create. An item whose estimate exceeds available Credits at its turn MUST return an item error and MUST NOT create a job.

The batch HTTP status MUST be 200 only when every item contains `client_task` and no item contains `error`. When any item contains `error` (validation failure, insufficient Credits, create failure, or model-not-ready), the HTTP status MUST be 400. The response body MUST still list every item result in request order.

`POST /inference_tasks/batch/status` MUST accept `{"client_task_ids":[...]}` with between 1 and 100 IDs. AS MUST deduplicate IDs for the database query and MUST restore the original order and duplicates in the response. A missing or cross-project ID MUST return an item error without revealing whether another project owns it.

## Task Abort

`POST /inference_tasks/:client_task_id/abort` MUST abort one Raw Task job owned by the authenticated project. The path `client_task_id` MUST be the local `task_jobs.id`. OpenAI-compatible routes MUST NOT expose abort.

Abort MUST use this outcome rule:

1. A missing or cross-project ID MUST return HTTP 404 without revealing whether another project owns the ID.
2. A job already `failed` with error message `cancelled by client` MUST succeed idempotently and return the failed task view.
3. A job already `completed` MUST return HTTP 409 and MUST NOT change Credits or the job row.
4. A job in `pending_submit` with no Bridge client task ID MUST run the failure record transaction with error message `cancelled by client`, unlock the Credits hold, and MUST NOT call Bridge.
5. A job that already has a Bridge client task ID MUST call Bridge `POST /v1/inference_tasks/:id/abort` first. Only after Bridge reports abort success MAY AS run the failure record transaction with error message `cancelled by client`. When Bridge reports `not_cancellable`, AS MUST return HTTP 409 and MUST leave the job and Credits hold unchanged. When Bridge reports `temporary_error` or a transport failure, AS MUST return HTTP 503 and MUST leave the job and Credits hold unchanged.

`POST /inference_tasks/batch/abort` MUST accept `{"client_task_ids":[...]}` with between 1 and 100 IDs. AS MUST return one item per requested ID in request order. Each item MUST contain `client_task_id` and either `client_task` or `error`. One item failure MUST NOT change another item's outcome. A missing or cross-project ID MUST appear as an item error without revealing ownership.

Client disconnect during abort MUST NOT itself cancel the job. Abort is an explicit API action only.

## Results

An LLM result request MUST succeed only for a successful LLM job owned by the authenticated project. AS MUST proxy the authenticated Bridge JSON result and return `Content-Type: application/json`.

An Image result request MUST succeed only for a successful Image job owned by the authenticated project. AS MUST read the PNG from the local task result directory at `{llm.task_result_dir}/{task_jobs.id}/{index}.png` and return `Content-Type: image/png` with a download `Content-Disposition`. A missing local file for a completed Image job MUST return HTTP 500.

A Video result request MUST succeed only for a successful Video job owned by the authenticated project. A Video task produces exactly one MP4 video. After Bridge reports success, the worker MUST download it from Bridge `GET /v1/inference_tasks/:id/video` and store it at `{llm.task_result_dir}/{task_jobs.id}/0.mp4` before completing the job. The result request MUST read that file and return `Content-Type: video/mp4` with `Content-Disposition: attachment; filename=0.mp4`. A missing or empty local file for a completed Video job MUST return HTTP 500.

A result request whose path does not match the job task type MUST return HTTP 400. A result request for a job that is not successful MUST return HTTP 409.

### Public hash downloads

`GET /api/result/:result_download_hash/images/:index` and `GET /api/result/:result_download_hash/video` MUST NOT require authentication. AS MUST look up the job by exact `result_download_hash`. When the hash is unknown, the job task type does not match the path, the job is not successful, the local result file is missing or empty, or the job row has been removed by retention cleanup, the response MUST be HTTP 404. A successful Image hash download MUST return the same PNG bytes and headers as the authenticated Image result route. A successful Video hash download MUST return the same MP4 bytes and headers as the authenticated Video result route. One Image job MUST use one `result_download_hash` for every `images/{index}` under that job.

Image PNG bytes and Video MP4 bytes MUST be stored only on the local filesystem under `llm.task_result_dir`. Image PNG bytes, Video MP4 bytes, base64 encodings, and result directory paths MUST NOT be stored in MySQL. OpenAI Images API responses MUST encode PNG files from disk into the HTTP response body and MUST NOT persist that encoded body on the job row. Raw LLM clients MUST receive the raw Bridge JSON result.

MCP access to Raw Task create, status, abort, models, Cost Level, LLM result text, and local save tools is specified in [mcp.md](./mcp.md).

## Project Cost Level

The project-authenticated Cost Level routes MUST use the same `ProjectAuth` rules as Raw Task. Stored fields, Static bounds, Auto resolution, and Max Credits rules MUST match [llm-api.md](./llm-api.md) Project Cost Level. The JWT management APIs `PUT /v1/projects/:project_id` and `GET /v1/llm/billing_config` remain for the WebUI; ProjectAuth Cost Level routes MUST apply the same Cost Level validation as project update.

### `GET /cost_level`

Response `data` MUST contain:

* `cost_level_mode` (`static` or `auto`)
* `priority_gwei` (decimal integer Gwei string)
* `auto_queue_position` (integer in `[0, 100]`, or `null` when unset)
* `auto_max_credits_per_task` (positive decimal integer Credits string, or `null` when unset)

### `PUT /cost_level`

The body MUST be a partial update with at least one of `cost_level_mode`, `priority_gwei`, `auto_queue_position`, or `auto_max_credits_per_task`. Omitted fields MUST remain unchanged. Validation MUST match JWT project Cost Level update: `priority_gwei` MUST be a positive multiple of `1000` Gwei inside Static project bounds; `auto_queue_position` MUST be in `[0, 100]`; `auto_max_credits_per_task` MUST be a positive decimal integer; setting `cost_level_mode` MUST require a valid Max Credits on the project after applying the same payload; switching to `auto` with a missing queue position MUST set `auto_queue_position` to `50`. On success the response `data` MUST match `GET /cost_level`.

### `GET /cost_level/hints`

Response `data` MUST contain Crynux Network reasonableness fields only:

* `base_vram`
* `credits_per_gwei`
* `min_priority_gwei` and `max_priority_gwei` (Static project bounds)
* `median_priority_gwei`
* `highest_priority_gwei` and `lowest_priority_gwei` (live non-empty Crynux queue bounds, or both `null`)
* `default_auto_max_credits_per_task`

The response MUST NOT include Provider queue or Provider admission hint fields. Those remain on JWT `GET /v1/llm/billing_config` as specified in [credits-billing.md](./credits-billing.md).

## Models

`GET /models` MUST return the merged chat, image, and video catalogs. `GET /models/chat` MUST return chat models only. `GET /models/image` MUST return image models only. `GET /models/video` MUST return video models only. `GET /models/<model>` MUST return every matching chat, image, or video catalog row for that model id, including a visible Provider chat or image model row when present, or HTTP 404 when none match.

Each Crynux LLM list object MUST include `min_vram`, `on_disk_node_count`, `dtype`, and `quantization_config`, and MUST NOT include `model_cache_hash`, `model_cache_config`, or `in_memory_node_count`. `dtype` MUST be the concrete dtype reported by Relay for that row. The same `id` MAY appear on multiple rows when quantization or dtype differs. Crynux Image rows MUST return one list entry per Relay Image catalog row and MUST include `id`, `object`, `created`, `owned_by`, `min_vram`, `on_disk_node_count`, `dtype`, `quantization_config`, `components` (an object from usage name to `{format, repo, format_kwargs}`, empty when the row has no components), and `lora` (an object with `model`, `weight_file_name`, and `weight`, or `null`). The `dtype`, `quantization_config`, `components`, `lora`, and `min_vram` of one Crynux Image entry MUST be usable together as executable TaskArgs fields. Crynux Video rows MUST return one list entry per Relay Video catalog row with the same fields as Image rows, except that `lora` MUST be replaced by `loras` (an array of objects with `model`, `weight_file_name`, `weight`, and `target`, empty when the row has no LoRAs). The `dtype`, `quantization_config`, `components`, `loras`, and `min_vram` of one Crynux Video entry MUST be usable together as executable Video TaskArgs fields. Crynux `id` MUST be the lowercase HuggingFace model ID. Catalog list and retrieve responses MUST NOT include `model_cache_hash`, `model_cache_config`, or `in_memory_node_count` on any row. Provider rows MUST use the Provider catalog shape from [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md) and MUST omit Crynux-only fields. Merged lists MUST sort by `id` ascending, then execution source with Crynux before Provider. All Crynux catalog snapshots MUST include only rows whose Relay `on_disk_node_count` is greater than or equal to `llm.min_catalog_on_disk_node_count`. List and retrieve endpoints MUST return the public models catalog cache rebuilt every `llm.models_catalog_refresh_interval` seconds and MUST NOT query Shared API Key visibility on each request. List endpoints MUST use the `{"object":"list","data":[...]}` envelope. A Provider row MUST appear only while its Service Provider has at least one enabled, non-deleted Shared API Key at the time of the latest successful catalog refresh.

## Pricing and Settlement

LLM tasks MUST use the LLM execution-time coefficients, precheck token estimate, and actual result usage specified in [credits-billing.md](./credits-billing.md).

Image tasks MUST fetch Relay `GET /v2/models/sd/execution-time` with the complete model execution and hardware selection, including `quantization_config` when present in TaskArgs. Image work MUST be:

```text
pixel_step_units = num_images_per_prompt * image_width * image_height * steps
estimated_node_seconds =
    max(overhead_seconds + seconds_per_sd_pixel_step * pixel_step_units, 1)
```

Video tasks MUST fetch Relay `GET /v2/models/video/execution-time` with the complete model execution and hardware selection, including `quantization_config` when present in TaskArgs. Video work MUST be:

```text
video_units = width * height * num_frames * steps
estimated_node_seconds =
    max(overhead_seconds + seconds_per_video_unit * video_units, 1)
```

`width`, `height`, `num_frames`, and `steps` MUST come from TaskArgs `task_config`. An omitted `num_frames` MUST be `81` and an omitted `steps` MUST be `30`. When `width` or `height` is omitted, AS MUST take it from the decoded dimensions of the first conditioning image in this order: `image`, `last_image`, `keyframes[0].image`, then the first `references` entry with `type` `image`. Without a conditioning image, the omitted value MUST be `832` for `width` and `480` for `height`. A conditioning image used for dimensions MUST be a base64 image data URL; an undecodable image or a zero `num_frames` or `steps` MUST be rejected with HTTP 400.

The multiplications MUST reject unsigned 64-bit overflow. Image and Video task fee and Credits MUST use the shared `priority_gwei * estimated_node_seconds * vram_weight` amount. The submitted fee MUST be the task fee in Wei expected by Bridge. The create-time Image and Video settlement Credits MUST remain unchanged by later configuration or Relay coefficient changes.

Successful settle MUST release the Credits hold, then debit `min(computed_or_fixed_credits, available)` where `available = max(balance - remaining_locked, 0)`. The debit MUST NOT consume Credits still locked by other jobs. When the charged amount is less than the computed or fixed amount, the platform MUST absorb the shortfall. A terminal failure MUST release the Credits hold, create a failed `task_call_records` row, and MUST NOT create a charge event or change `balance`. Settlement and failure retries MUST create at most one call record, one charge event, one balance debit, and one hold release.
