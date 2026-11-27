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

### Related guides (VRAM vs TaskArgs)

These resources split **which model to pick** from **how to write TaskArgs**. Read only what you need.

| Resource | What it contains | What it does not contain |
|----------|------------------|--------------------------|
| `crynux://guides/vram-models` | One primary LLM / Image / Video (T2V) pick per 24 / 32 / 80 / 96 GB tier; video T2V / I2V / FLF2V checkpoint table; platform notes (BitsAndBytes / GGUF / AWQ) | Field definitions; full TaskArgs JSON; Cost Level / Credits |
| `crynux://guides/task-args/llm` | LLM field reference (schema link, defaults, AS create rules) and complete example TaskArgs JSON for each VRAM-tier LLM pick | Image / Video shapes; cross-type VRAM comparison tables |
| `crynux://guides/task-args/image` | Image field reference and complete example TaskArgs JSON for each VRAM-tier Image pick (including `components` / GGUF / BitsAndBytes) | LLM / Video shapes; cross-type VRAM comparison tables |
| `crynux://guides/task-args/video` | Video field reference (media modes, `task_config`, `components`, quant) and complete example TaskArgs JSON for T2V / I2V / FLF2V per VRAM tier | LLM / Image shapes; the short VRAM pick tables (those stay in `vram-models`) |
| `crynux://guides/cost-level` | Cost Level modes, hints, create-time Credits estimate | Model / VRAM picks; TaskArgs field docs |

Typical order: choose a tier row in `vram-models` → open the matching task-args guide for the full JSON → set create `min_vram` from that row (or higher). Do not invent a large-model + low-`min_vram` combination outside those guides. Do not use Wan 2.2 A14B on 24 GB.

Short summary of the primary picks (full tables and video modes: `vram-models`):

| Tier | LLM | Image | Video (T2V) |
|------|-----|-------|-------------|
| 24 GB | `Qwen/Qwen3.5-9B` bf16 | `Qwen/Qwen-Image-2.1-Turbo` GGUF Q4 + text_encoder BitsAndBytes NF4 | `Wan-AI/Wan2.2-TI2V-5B-Diffusers` BitsAndBytes NF4 |
| 32 GB | `Qwen/Qwen3.8-27B` BitsAndBytes INT8 | `Qwen/Qwen-Image-2.1` BitsAndBytes NF4 | `Wan-AI/Wan2.2-T2V-A14B-Diffusers` BitsAndBytes NF4 |
| 80 GB | `Qwen/Qwen3.8-27B` bf16 | `black-forest-labs/FLUX.2-klein-4B` bf16 | `Wan-AI/Wan2.2-T2V-A14B-Diffusers` BitsAndBytes NF4 |
| 96 GB | `Qwen/Qwen3.8-27B` bf16 (`min_vram` 96) | `Qwen/Qwen-Image-2.1` bf16 | `Wan-AI/Wan2.2-T2V-A14B-Diffusers` bf16 |

### Changing a config

- Change **`components`** → changes the required model file set. If that set has no on-disk coverage, create returns `model_not_ready`.
- Change only **`quantization_config`** or **`dtype`** → does **not** cause `model_not_ready`; it is a different run config, so the node must load it if that config is not already in memory.
- Change only **LoRA** → does **not** change the required model file set and does **not** cause `model_not_ready`; it is a different run config for in-memory load.

### Models not in the list

You can submit a HuggingFace model that is not in `list_models` when TaskArgs match the schema. Create still returns `model_not_ready` when that model's required file set has no on-disk coverage; keep `client_task_id` and poll with `get_task`. When coverage exists, later creates for the same file set do not get `model_not_ready` from this gate.

## TaskArgs overview

`task_args` is a JSON string validated against the task schema before the job is created. Field-by-field rules and **complete example JSON** (including the VRAM-tier picks above) live only in the type-specific guides.

- LLM schema: https://github.com/crynux-network/gpt-task/blob/main/schema/gpt-inference-task.json
- Image schema: https://github.com/crynux-network/stable-diffusion-task/blob/main/schema/image-task.json
- Video schema: https://github.com/crynux-network/stable-diffusion-task/blob/main/schema/video-task.json

| Type | Resource | Required highlights |
|------|----------|---------------------|
| LLM (`task_type` 1) | `crynux://guides/task-args/llm` | `version`, `model`, `messages`; optional `dtype`, `quantization_config`, `generation_config`, tools, seed |
| Image (`task_type` 0) | `crynux://guides/task-args/image` | `version`, `base_model`, `prompt`, `task_config`; optional `components`, `quantization_config`, `lora`, `images` |
| Video (`task_type` 3) | `crynux://guides/task-args/video` | `version`, `base_model`; optional prompt/image inputs, `task_config`, `components`, `quantization_config`, `loras` |

## Cost Level

**Cost Level** is a **project** setting that sets each create's effective priority, task fee, and Credits charge. Read resource `crynux://guides/cost-level` for modes, field values, how to judge settings with `get_cost_level_hints`, and how to dry-run Credits with `estimate_task_credits`. Use `get_cost_level` / `set_cost_level` to read or change the project settings. MCP cannot set `task_fee` on create.

Create fails when fee/Credits fail admission or exceed Max Credits, or when available Credits are insufficient.
