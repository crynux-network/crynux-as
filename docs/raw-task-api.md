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

The request MAY select hardware with `min_vram`, or with both `required_gpu` and `required_gpu_vram`. It MUST NOT combine `min_vram` with either required-GPU field. It MUST provide both required-GPU fields together.

When `min_vram` is omitted and the request does not select an exact GPU, AS MUST resolve the default `min_vram` as follows: for an Image task, look up the Image loaded-models cache by `task_args.base_model` name and `variant` (missing or empty `variant` is the empty string); for an LLM task, look up the LLM loaded-models cache by model id. When the lookup misses or the cached `min_vram` is zero, AS MUST use `llm.default_vram_limit`. AS MUST NOT rewrite a missing Image `variant` to `fp16` or any other default string.

`task_fee` is owned by Crynux AS. A request containing `task_fee`, including a null value, MUST be rejected. AS MUST resolve the project Cost Level, fetch the applicable Relay execution-time coefficients, calculate the task fee in Wei and the Credits estimate, then in one transaction reserve that estimate against available Credits (`balance - locked`) and create a `task_jobs` row with a `held` Credits lock, without waiting for Bridge. Insufficient available Credits MUST return HTTP 402.

The response MUST use the local task view:

```json
{
  "message": "success",
  "data": {
    "id": 123,
    "task_type": 1,
    "status": "running",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-01T00:00:00Z"
  }
}
```

Public status MUST be `running`, `success`, or `failed`. Pending submission, submitted, and in-progress jobs MUST map to `running`. When status is `failed` and the job has a stored error message, the task view MUST include `error_message` with that text. Running and success responses MUST omit `error_message`.

## Batch Operations

`POST /inference_tasks/batch` MUST accept `{"tasks":[...]}` with between 1 and 100 items. AS MUST validate and price items in request order. The response MUST contain one item result per input index in the same order. One invalid item MUST NOT prevent valid items from being accepted. Each item MUST contain `index` and either `client_task` or `error`.

The batch path MUST reserve each accepted item's estimated Credits in request order through the same per-item reserve-and-create transaction used by single create. An item whose estimate exceeds available Credits at its turn MUST return an item error and MUST NOT create a job.

`POST /inference_tasks/batch/status` MUST accept `{"client_task_ids":[...]}` with between 1 and 100 IDs. AS MUST deduplicate IDs for the database query and MUST restore the original order and duplicates in the response. A missing or cross-project ID MUST return an item error without revealing whether another project owns it.

## Results

An LLM result request MUST succeed only for a successful LLM job owned by the authenticated project. AS MUST proxy the authenticated Bridge JSON result and return `Content-Type: application/json`.

An Image result request MUST succeed only for a successful Image job owned by the authenticated project. AS MUST proxy the selected authenticated Bridge result and return `Content-Type: image/png` with a download `Content-Disposition`.

Image bytes MUST NOT be stored in MySQL. OpenAI-formatted results MAY remain on retained OpenAI jobs. Raw LLM clients MUST receive the raw Bridge JSON result.

## Models

`GET /models` and `GET /models/<model>` MUST continue to use only the LLM catalog snapshot and MUST keep the OpenAI-compatible model object fields without a `variant` field. Each LLM list and retrieve object MUST include `min_vram` and `on_disk_node_count` and MUST NOT include `in_memory_node_count`. `GET /models/image` MUST use the independent Image catalog snapshot and MUST return one list entry per `(model_id, variant)` pair, sorted by `id` ascending then `variant` ascending. Each Image list entry MUST include `id`, `object`, `created`, `owned_by`, `min_vram`, `variant`, and `on_disk_node_count`. `id` MUST be the lowercase HuggingFace model ID. `variant` MUST be the cached lowercase variant string, which MAY be empty. `on_disk_node_count` MUST be the cached Relay count of nodes that have the model files on disk. Image list entries MUST NOT include `in_memory_node_count`. Both catalog snapshots MUST include only models whose Relay `on_disk_node_count` is greater than or equal to `llm.min_catalog_on_disk_node_count`, and both list endpoints MUST use the `{"object":"list","data":[...]}` envelope.

## Pricing and Settlement

LLM tasks MUST use the LLM execution-time coefficients, precheck token estimate, and actual result usage specified in [credits-billing.md](./credits-billing.md).

Image tasks MUST fetch Relay `GET /v2/models/sd/execution-time` with the complete model execution and hardware selection. Image work MUST be:

```text
pixel_step_units = num_images * image_width * image_height * steps
estimated_node_seconds =
    max(overhead_seconds + seconds_per_sd_pixel_step * pixel_step_units, 1)
```

The multiplication MUST reject unsigned 64-bit overflow. Image task fee and Credits MUST use the shared `priority_gwei * estimated_node_seconds * vram_weight` amount. The submitted fee MUST be the task fee in Wei expected by Bridge. The create-time Image settlement Credits MUST remain unchanged by later configuration or Relay coefficient changes.

Successful settle MUST release the Credits hold, then debit `min(computed_or_fixed_credits, balance)`. A terminal failure MUST release the Credits hold, create a failed `task_call_records` row, and MUST NOT create a charge event or change `balance`. Settlement and failure retries MUST create at most one call record, one charge event, one balance debit, and one hold release.
