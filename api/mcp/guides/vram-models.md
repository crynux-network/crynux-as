# VRAM tiers and mainstream models

Use this guide after reading the model-selection rules in `crynux://guides/raw-task`. Prefer a matching `list_models` row with `on_disk_node_count >= 1`, then set create `min_vram` to that row's `min_vram` (or a higher floor if you intentionally restrict hardware).

These tables are coverage samples. They are not an allowlist. Peak VRAM still depends on resolution, frame count, context length, and KV cache. When a catalog row exists, copy that row's `dtype` / `quantization_config` / `components` instead of inventing a mix.

`min_vram` is the hardware floor for matching. A 96 GB node can run a `min_vram=24` task; a 24 GB node cannot run `min_vram=80`.

Platform notes:

- BitsAndBytes / GGUF: Windows CUDA and Linux CUDA
- LLM AWQ / GPTQ / Image Nunchaku Lite: Linux / Docker CUDA only (Relay will not schedule those to Windows)

TaskArgs JSON for every pick below lives in the type-specific guides:

- LLM: `crynux://guides/task-args/llm`
- Image: `crynux://guides/task-args/image`
- Video: `crynux://guides/task-args/video`

## Primary picks by VRAM tier

One primary LLM / Image / Video pick per tier. Start here unless you need another video mode (see the video table).

| Tier | Typical cards | LLM | Image | Video (T2V) |
|------|---------------|-----|-------|-------------|
| 24 GB | RTX 4090 class | `Qwen/Qwen3.5-9B` bf16 (no quant) | `Qwen/Qwen-Image-2.1-Turbo` transformer GGUF Q4 + text_encoder BitsAndBytes NF4 | `Wan-AI/Wan2.2-TI2V-5B-Diffusers` BitsAndBytes NF4 |
| 32 GB | RTX 5090 class | `Qwen/Qwen3.8-27B` BitsAndBytes INT8 | `Qwen/Qwen-Image-2.1` BitsAndBytes NF4 | `Wan-AI/Wan2.2-T2V-A14B-Diffusers` BitsAndBytes NF4 |
| 80 GB | A100 80GB class | `Qwen/Qwen3.8-27B` bf16 (no quant) | `black-forest-labs/FLUX.2-klein-4B` bf16 (no quant) | `Wan-AI/Wan2.2-T2V-A14B-Diffusers` BitsAndBytes NF4 |
| 96 GB | Pro 6000 class | `Qwen/Qwen3.8-27B` bf16 (no quant; `min_vram=96`) | `Qwen/Qwen-Image-2.1` bf16 (no quant) | `Wan-AI/Wan2.2-T2V-A14B-Diffusers` bf16 (no quant) |

## Video modes by VRAM tier

Input mode is selected by media fields: T2V = prompt only; I2V = prompt + `image`; FLF2V = prompt + `image` + `last_image`.

| Tier | T2V | I2V | FLF2V |
|------|-----|-----|-------|
| 24 GB | `Wan2.2-TI2V-5B` BitsAndBytes NF4 | Same TI2V-5B + `image`, BitsAndBytes NF4 | `Wan2.1-FLF2V-14B-720P` BitsAndBytes NF4 |
| 32 GB | `Wan2.2-T2V-A14B` BitsAndBytes NF4 | `Wan2.2-I2V-A14B` BitsAndBytes NF4 | `Wan2.2-I2V-A14B` BitsAndBytes NF4 + both frames (or keep Wan2.1 FLF2V NF4) |
| 80 GB | Same A14B BitsAndBytes NF4 as 32 GB | Same I2V-A14B BitsAndBytes NF4 as 32 GB | Same I2V-A14B BitsAndBytes NF4 FLF as 32 GB |
| 96 GB | `Wan2.2-T2V-A14B` bf16 (no quant) | `Wan2.2-I2V-A14B` bf16 (no quant) | `Wan2.2-I2V-A14B` bf16 (no quant) + both frames |

Do not use Wan 2.2 A14B on 24 GB: BitsAndBytes NF4 peak is about 22–26 GB and it can still OOM on a single 24 GB card without CPU offload.

Checkpoint names in full:

- T2V 5B: `Wan-AI/Wan2.2-TI2V-5B-Diffusers`
- T2V A14B: `Wan-AI/Wan2.2-T2V-A14B-Diffusers`
- I2V / FLF A14B: `Wan-AI/Wan2.2-I2V-A14B-Diffusers`
- FLF 24 GB: `Wan-AI/Wan2.1-FLF2V-14B-720P-Diffusers`

## How to apply this with MCP tools

1. Call `list_models` (`catalog` `chat`, `image`, or `video`).
2. Prefer a row whose model and quant match a tier above and `on_disk_node_count >= 1`.
3. Copy that entire row into TaskArgs using the matching task-args guide for the JSON shape; set create `min_vram` from the row or from the tier table.
4. For video, pick the checkpoint for T2V / I2V / FLF2V from the video table, then set media fields for that mode.
5. Optional: call `estimate_task_credits` before `create_task`.
