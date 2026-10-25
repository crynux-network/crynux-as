# Raw Task API

## Scope and Authentication

Each project MUST expose the Raw Task API under `/api/<endpoint_token>/v1`. Every request MUST use `Authorization: Bearer <token>`, where `<token>` MUST be either the project API key or a valid JWT issued by Crynux AS for the wallet address that owns the project. The endpoint token and credentials MUST identify the same active project. The project API key and an owner JWT MUST grant the same permission to call the project's Raw Task endpoints.

The API MUST expose:

* `POST /inference_tasks`
* `GET /inference_tasks/:client_task_id`
* `GET /inference_tasks/:client_task_id/llm`
* `GET /inference_tasks/:client_task_id/images/:index`
* `POST /inference_tasks/batch`
* `POST /inference_tasks/batch/status`
* `GET /models/image`

The public `client_task_id` MUST be the Crynux AS `task_jobs.id`. A Bridge ClientTask ID, Bridge client ID, or platform client ID MUST NOT appear in a public response. Every lookup and result download MUST select by both project ID and local task ID.

## Task Creation

`POST /inference_tasks` MUST accept the Bridge raw-task fields except `task_fee`:

```json
{
  "task_args": "{\"model\":\"example/model\",\"messages\":[]}",
  "task_type": 1,
  "task_version": "2.0.0",
  "min_vram": 24,
  "queue_timeout": 43200
}
```

`task_type` MUST be `0` for an Image task or `1` for an LLM task. `task_args` MUST be a JSON string valid for the selected task type.

The request MAY include optional `queue_timeout` as an unsigned integer number of seconds. When present, the value MUST be greater than zero. AS MUST persist it on the job and MUST forward it to Bridge as raw task `queue_timeout`. When omitted, AS MUST omit the field so Relay applies its configured default queue wait. `queue_timeout` MUST NOT be placed inside `task_args` and MUST NOT change Cost Level or Credits estimation.

Before Credits reservation, AS MUST validate `task_args` against the configured JSON Schema for the task type (`task_schema.gpt_inference` for LLM, `task_schema.stable_diffusion_inference` for Image). A schema validation failure MUST return HTTP 400 with a validation error on field `task_args` and MUST NOT reserve Credits or create a job.

For an Image task, after schema validation succeeds, AS MUST sum the byte lengths of every string in `task_args.images` as stored in the JSON (including any `data:image/...;base64,` prefix). AS MUST NOT decode base64 payloads to measure size. When that sum exceeds `1048576` (1mb), AS MUST return HTTP 400 with an invalid-request error whose message is exactly `task_args.images total size exceeds the 1mb limit`, and MUST NOT reserve Credits or create a job. An omitted or empty `images` array MUST pass this check.

When the validated TaskArgs omit the generation seed, AS MUST set one before the job is persisted. An LLM task uses top-level `seed`. An Image task uses `task_config.seed`. The value MUST be a uniformly chosen integer from 0 through 2147483647. A seed present in the client JSON, including 0, MUST be stored unchanged. The persisted TaskArgs JSON is the JSON submitted to Bridge.

After schema validation and before model validation, fee estimation, Image execution resolution, and Credits reservation, AS MUST normalize Hugging Face model name fields in TaskArgs the same way Relay does at task creation: trim is not applied inside repository strings; each Hugging Face repository name is lowercased; values that start with `http://` or `https://` are left unchanged. For an LLM task this MUST rewrite `task_args.model`. For an Image task this MUST rewrite `task_args.base_model` (string form or `base_model.name`), `lora.model`, `controlnet.model`, `refiner.model`, `unet`, `vae`, `textual_inversion`, and every `components.*.repo`. The normalized TaskArgs JSON MUST be the JSON used for Hub checks, loaded-models matching, job persistence, Bridge submit, call records, and usage stats. The job `model` field MUST equal the normalized LLM `model` or Image `base_model.name`.

Before Credits reservation, AS MUST also validate the task model:

1. For an LLM task, the normalized `task_args.model` value MUST be a Hugging Face model id. For an Image task, the normalized `task_args.base_model.name` MUST be a Hugging Face model id. Image model cache identity is the Relay `model_cache_hash` together with `execution_dtype`, derived from `base_model`, `components`, `lora`, `dtype`, and `quantization_config`.
2. Absolute URLs and any model id containing `://` MUST be rejected with HTTP 400.
3. When any row for the model is present in the corresponding full loaded-models cache (LLM or Image keyed by `(model_cache_hash, execution_dtype)`), AS MUST skip the Hub lookup.
4. When the model is absent from that cache, AS MUST query Hugging Face Hub. LLM validation MUST require the repo to exist. Image validation MUST require `model_index.json`. A permanent Hub rejection MUST return HTTP 400. Hub timeouts and HTTP 5xx MUST return HTTP 500 and MUST NOT reserve Credits or create a job.

The request MAY select hardware with `min_vram`, or with both `required_gpu` and `required_gpu_vram`. It MUST NOT combine `min_vram` with either required-GPU field. It MUST provide both required-GPU fields together.

When `min_vram` is omitted and the request does not select an exact GPU, AS MUST resolve the default `min_vram` as follows: for an LLM task, prefer an exact loaded-models match using the TaskArgs model id plus `quantization_config` and `dtype` when present; otherwise select the full-cache row with the greatest `min_vram` for that model id, with ties broken by canonical `quantization_config` ascending, then `execution_dtype` ascending, then `model_cache_hash` ascending. For an Image task, after schema validation and before Credits reservation, AS MUST run the shared Image execution resolver. When TaskArgs supply a concrete `dtype` (`float16`, `bfloat16`, or `float32`), AS MUST persist that dtype and every other client execution field unchanged; default `min_vram` MAY come only from an exact full-identity loaded-models hit, otherwise AS MUST use `llm.default_vram_limit`. When TaskArgs omit `dtype` or set `dtype` to `auto`, AS MUST select one coherent loaded-models row that agrees with every client-supplied `quantization_config`, `components`, and `lora` constraint (a request without `lora` MUST match only `lora=null` rows), MUST persist the selected row's concrete `execution_dtype` and any omitted `quantization_config` / `components` from that same row, and MUST use that row's `min_vram` as the default. When auto mode finds no matching row or the matched row lacks a concrete `execution_dtype`, AS MUST reject the item with an invalid-request error and MUST NOT reserve Credits. Batch create MUST apply that rejection per item. The resolved TaskArgs JSON MUST pass Image schema validation again and MUST be the JSON submitted to Bridge.

`task_fee` is owned by Crynux AS. A request containing `task_fee`, including a null value, MUST be rejected. AS MUST resolve the project Cost Level, fetch the applicable Relay execution-time coefficients, calculate the task fee in Wei and the Credits estimate, then in one transaction reserve that estimate against available Credits (`balance - locked`) and create a `task_jobs` row with a `held` Credits lock, without waiting for Bridge. Insufficient available Credits MUST return HTTP 402.

The response MUST use the local task view:

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

## Batch Operations

`POST /inference_tasks/batch` MUST accept `{"tasks":[...]}` with between 1 and 100 items. AS MUST validate and price items in request order. The response MUST contain one item result per input index in the same order. One invalid item MUST NOT prevent valid items from being accepted. Each item MUST contain `index` and either `client_task` or `error`.

The batch path MUST reserve each accepted item's estimated Credits in request order through the same per-item reserve-and-create transaction used by single create. An item whose estimate exceeds available Credits at its turn MUST return an item error and MUST NOT create a job.

`POST /inference_tasks/batch/status` MUST accept `{"client_task_ids":[...]}` with between 1 and 100 IDs. AS MUST deduplicate IDs for the database query and MUST restore the original order and duplicates in the response. A missing or cross-project ID MUST return an item error without revealing whether another project owns it.

## Results

An LLM result request MUST succeed only for a successful LLM job owned by the authenticated project. AS MUST proxy the authenticated Bridge JSON result and return `Content-Type: application/json`.

An Image result request MUST succeed only for a successful Image job owned by the authenticated project. AS MUST read the PNG from the local task result directory at `{llm.task_result_dir}/{task_jobs.id}/{index}.png` and return `Content-Type: image/png` with a download `Content-Disposition`. A missing local file for a completed Image job MUST return HTTP 500.

Image PNG bytes MUST be stored only on the local filesystem under `llm.task_result_dir`. Image PNG bytes, base64 encodings, and result directory paths MUST NOT be stored in MySQL. OpenAI Images API responses MUST encode PNG files from disk into the HTTP response body and MUST NOT persist that encoded body on the job row. Raw LLM clients MUST receive the raw Bridge JSON result.

## Models

`GET /models` and `GET /models/<model>` MUST continue to use only the LLM catalog snapshot. Each LLM list object MUST include `min_vram`, `on_disk_node_count`, `execution_dtype`, and `quantization_config`, and MUST NOT include `model_cache_hash`, `model_cache_config`, or `in_memory_node_count`. The same `id` MAY appear on multiple rows when quantization or dtype differs. `GET /models/<model>` MUST return the list envelope of every catalog row for that model id, or HTTP 404 when none match. `GET /models/image` MUST use the independent Image catalog snapshot and MUST return one list entry per `(model_cache_hash, execution_dtype)` row, sorted by `id` ascending, then `execution_dtype` ascending, then canonical `quantization_config` ascending, then `model_cache_hash` ascending. Each Image list entry MUST include `id`, `object`, `created`, `owned_by`, `min_vram`, `on_disk_node_count`, `execution_dtype`, `quantization_config`, `model_cache_hash` (lowercase hex), `components` (an object from usage name to `{format, repo, format_kwargs}`, empty when the row has no components), and `lora` (an object with `model`, `weight_file_name`, and `weight`, or `null`). The `execution_dtype`, `quantization_config`, `components`, `lora`, and `min_vram` of one entry MUST describe one cache identity and MUST be usable together as executable TaskArgs fields. `id` MUST be the lowercase HuggingFace model ID. Image list entries MUST NOT include `model_cache_config` or `in_memory_node_count`. Both catalog snapshots MUST include only rows whose Relay `on_disk_node_count` is greater than or equal to `llm.min_catalog_on_disk_node_count`, and both list endpoints MUST use the `{"object":"list","data":[...]}` envelope.

## Pricing and Settlement

LLM tasks MUST use the LLM execution-time coefficients, precheck token estimate, and actual result usage specified in [credits-billing.md](./credits-billing.md).

Image tasks MUST fetch Relay `GET /v2/models/sd/execution-time` with the complete model execution and hardware selection, including `quantization_config` when present in TaskArgs. Image work MUST be:

```text
pixel_step_units = num_images_per_prompt * image_width * image_height * steps
estimated_node_seconds =
    max(overhead_seconds + seconds_per_sd_pixel_step * pixel_step_units, 1)
```

The multiplication MUST reject unsigned 64-bit overflow. Image task fee and Credits MUST use the shared `priority_gwei * estimated_node_seconds * vram_weight` amount. The submitted fee MUST be the task fee in Wei expected by Bridge. The create-time Image settlement Credits MUST remain unchanged by later configuration or Relay coefficient changes.

Successful settle MUST release the Credits hold, then debit `min(computed_or_fixed_credits, balance)`. A terminal failure MUST release the Credits hold, create a failed `task_call_records` row, and MUST NOT create a charge event or change `balance`. Settlement and failure retries MUST create at most one call record, one charge event, one balance debit, and one hold release.
