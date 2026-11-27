# Crynux AS Raw Task MCP

## What you can do

With Crynux AS you can use open-source models from Hugging Face to:

- **Chat and text:** talk with LLMs, or generate many text outputs in one go
- **Images:** create or edit pictures
- **Video:** generate videos

## Workflow

1. Choose a model and full run config. Call the `list_models` tool, prefer a returned row with `on_disk_node_count >= 1`, and copy that row's fields into TaskArgs as one set.
2. Call `create_task` or `batch_create_tasks` (`task_type`: `0` Image, `1` LLM, `3` Video).
3. Poll with `get_task` or `batch_get_task_status` until `status` is `success` or `failed`.
4. On success:
   - LLM: call the `get_task_llm` tool
   - Image: present `result_download_urls` to the user
   - Video: present `result_download_url` to the user

Image and video results come back as download links in the conversation. To save many images, videos, or large text dumps to local files, install the local Crynux AS MCP package.

### Errors and what to do next

- **`model_not_ready`:** The required model files are not on disk yet, but the task **was created** and **keeps running** on Crynux AS. Keep the returned `client_task_id` and poll with `get_task` / batch status until `success` or `failed`. Do not treat this as a simple invalid-argument failure and drop the id.
- **TaskArgs / schema validation failure:** Create fails and there is no usable task id. Fix `task_args` and retry.
- **Credits / Cost Level rejection:** Create fails (insufficient Credits, estimate over Max Credits, or fee below admission). Check Credits; for Cost Level read `crynux://guides/cost-level` and use `get_cost_level_hints` / `set_cost_level`.
- **`failed` with `error_message`:** read the message and act on it. Current Crynux Network job values:
  - `timeout` — queue wait ended before a ready run could start, or execution timed out after start (raise Cost Level with `set_cost_level`, pick a catalog row that is already ready, and/or set a larger `queue_timeout` on a **new** create)
  - `result_upload_timeout` — result upload timed out after validation; create again
  - `group_timeout` — validation group timed out; create again
  - `incorrect_result` / `error_reported` — execution or validation failed; change TaskArgs or model config and create again

## Model overview

The same HuggingFace model name can appear as **several** `list_models` rows. Different `quantization_config`, `dtype`, Image/Video `components`, and LoRA are different run configs. Each row has its own `min_vram` and `on_disk_node_count`.

Two checks matter, and they are not the same:

1. **Required model files (download / on-disk readiness).**  
   - LLM: the base model repository only.  
   - Image/Video: the base model repository plus every repository under `components`.  
   - `quantization_config`, `dtype`, and LoRA (`lora` / `loras`) are **not** required model files and do **not** change this file set.

2. **Exact run config (in-memory warm load).**  
   `quantization_config`, `dtype`, `components`, and LoRA together identify one run config. Same on-disk files with a different quantization are a different run config. Matching prefers a node that already has that exact config in memory; otherwise the selected node must load it before running, which takes longer.

Create returns `model_not_ready` when the **required model file set** has no on-disk coverage yet:

- LLM: no full-cache row for that base model with `on_disk_node_count >= 1`
- Image/Video: no full-cache row for that base model **and the same `components` set** with `on_disk_node_count >= 1`

That AS check **ignores** `quantization_config`, `dtype`, and LoRA. Changing only those fields does **not** cause `model_not_ready`. Always copy the full `list_models` row into TaskArgs; do not change quantization just because create did not return `model_not_ready`.

### Picking a `list_models` row

Prefer rows with `on_disk_node_count >= 1`. Copy **one entire row** into TaskArgs — do not mix fields across rows.

- **LLM:** `id` → `task_args.model`; copy that row's `dtype` and `quantization_config`; use that row's `min_vram` unless you intentionally change the hardware floor.
- **Image:** `id` → `base_model.name`; copy `dtype`, `quantization_config`, `components`, `lora`, and that row's `min_vram`.
- **Video:** `id` → `base_model.name`; copy `dtype`, `quantization_config`, `components`, `loras`, and that row's `min_vram`.

### Changing a config

- Change **`components`** → changes the required model file set. If that set has no on-disk coverage, create returns `model_not_ready`.
- Change only **`quantization_config`** or **`dtype`** → does **not** cause `model_not_ready`; it is a different run config, so the node must load it if that config is not already in memory.
- Change only **LoRA** → does **not** change the required model file set and does **not** cause `model_not_ready`; it is a different run config for in-memory load.

### Models not in the list

You can submit a HuggingFace model that is not in `list_models` when TaskArgs match the schema. Create still returns `model_not_ready` when that model's required file set has no on-disk coverage; keep `client_task_id` and poll with `get_task`. When coverage exists, later creates for the same file set do not get `model_not_ready` from this gate.

## TaskArgs overview

`task_args` is a JSON string validated against the task schema before the job is created.

- LLM schema: https://github.com/crynux-network/gpt-task/blob/main/schema/gpt-inference-task.json
- Image schema: https://github.com/crynux-network/stable-diffusion-task/blob/main/schema/image-task.json
- Video schema: https://github.com/crynux-network/stable-diffusion-task/blob/main/schema/video-task.json

Top-level shape (details and examples live in separate resources — read only what you need):

| Type | Resource | Required highlights |
|------|----------|---------------------|
| LLM (`task_type` 1) | `crynux://guides/task-args/llm` | `version`, `model`, `messages`; optional `dtype`, `quantization_config`, `generation_config`, tools, seed |
| Image (`task_type` 0) | `crynux://guides/task-args/image` | `version`, `base_model`, `prompt`, `task_config`; optional `components`, `quantization_config`, `lora`, `images` |
| Video (`task_type` 3) | `crynux://guides/task-args/video` | `version`, `base_model`; optional prompt/image inputs, `task_config`, `components`, `quantization_config`, `loras` |

## Cost Level

**Cost Level** is a **project** setting that sets each create's effective priority, task fee, and Credits charge. Read resource `crynux://guides/cost-level` for modes, field values, how to judge settings with `get_cost_level_hints`, and how to dry-run Credits with `estimate_task_credits`. Use `get_cost_level` / `set_cost_level` to read or change the project settings. MCP cannot set `task_fee` on create.

Create fails when fee/Credits fail admission or exceed Max Credits, or when available Credits are insufficient.
