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
  "min_vram": 24
}
```

`task_type` MUST be `0` for an Image task or `1` for an LLM task. `task_args` MUST be a JSON string valid for the selected task type.

Before Credits reservation, AS MUST validate `task_args` against the configured JSON Schema for the task type (`task_schema.gpt_inference` for LLM, `task_schema.stable_diffusion_inference` for Image). A schema validation failure MUST return HTTP 400 with a validation error on field `task_args` and MUST NOT reserve Credits or create a job.

Before Credits reservation, AS MUST also validate the task model:

1. For an LLM task, the `task_args.model` value MUST be a Hugging Face model id. For an Image task, `task_args.base_model.name` MUST be a Hugging Face model id. Image `base_model` MUST NOT use `variant`; AS MUST ignore any Relay `variant` field for Image identity.
2. Absolute URLs and any model id containing `://` MUST be rejected with HTTP 400.
3. When any row for the model is present in the corresponding full loaded-models cache (LLM or Image keyed by `(model_id, quantization_config, execution_dtype)`), AS MUST skip the Hub lookup.
4. When the model is absent from that cache, AS MUST query Hugging Face Hub. LLM validation MUST require the repo to exist. Image validation MUST require `model_index.json`. A permanent Hub rejection MUST return HTTP 400. Hub timeouts and HTTP 5xx MUST return HTTP 500 and MUST NOT reserve Credits or create a job.

The request MAY select hardware with `min_vram`, or with both `required_gpu` and `required_gpu_vram`. It MUST NOT combine `min_vram` with either required-GPU field. It MUST provide both required-GPU fields together.

When `min_vram` is omitted and the request does not select an exact GPU, AS MUST resolve the default `min_vram` as follows: prefer an exact loaded-models match using the TaskArgs model id plus `quantization_config` and `dtype` when present; otherwise select the full-cache row with the greatest `min_vram` for that model id, with ties broken by canonical `quantization_config` ascending then `execution_dtype` ascending. When the lookup misses or the cached `min_vram` is zero, AS MUST use `llm.default_vram_limit`. AS MUST NOT rewrite the caller's TaskArgs to inject a default `quantization_config` or `dtype` on the Raw path.

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

An Image result request MUST succeed only for a successful Image job owned by the authenticated project. AS MUST proxy the selected authenticated Bridge result and return `Content-Type: image/png` with a download `Content-Disposition`.

Image bytes MUST NOT be stored in MySQL. OpenAI-formatted results MAY remain on retained OpenAI jobs. Raw LLM clients MUST receive the raw Bridge JSON result.

## Models

`GET /models` and `GET /models/<model>` MUST continue to use only the LLM catalog snapshot. Each LLM list object MUST include `min_vram`, `on_disk_node_count`, `execution_dtype`, and `quantization_config`, and MUST NOT include `variant` or `in_memory_node_count`. The same `id` MAY appear on multiple rows when quantization or dtype differs. `GET /models/<model>` MUST return the list envelope of every catalog row for that model id, or HTTP 404 when none match. `GET /models/image` MUST use the independent Image catalog snapshot and MUST return one list entry per `(model_id, quantization_config, execution_dtype)` row, sorted by `id` ascending, then `execution_dtype` ascending, then canonical `quantization_config` ascending. Each Image list entry MUST include `id`, `object`, `created`, `owned_by`, `min_vram`, `on_disk_node_count`, `execution_dtype`, and `quantization_config`. `id` MUST be the lowercase HuggingFace model ID. Image list entries MUST NOT include `variant` or `in_memory_node_count`. Both catalog snapshots MUST include only rows whose Relay `on_disk_node_count` is greater than or equal to `llm.min_catalog_on_disk_node_count`, and both list endpoints MUST use the `{"object":"list","data":[...]}` envelope.

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
