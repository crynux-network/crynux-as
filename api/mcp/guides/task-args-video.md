# Video TaskArgs

Schema (main): https://github.com/crynux-network/stable-diffusion-task/blob/main/schema/video-task.json

`task_type` for create tools is `3`. Prefer copying `dtype`, `quantization_config`, `components`, and `loras` from one `list_models` video row together with `base_model.name`.

## Fields

| Field | Notes |
|-------|--------|
| `version` | Runner TaskArgs version string (required). |
| `base_model` | `{ "name", "revision"? }` (required). |
| `dtype` | On Crynux AS Raw create: must be concrete `float16`, `bfloat16`, or `float32`. Prefer the catalog row. |
| `prompt` | Optional string. At least one of a non-empty `prompt` or conditioning media is required. |
| `negative_prompt` | Optional string or `null`. |
| `image` / `last_image` | Optional data URLs. `image` alone → image-to-video; `image` + `last_image` → first-last-frame. |
| `keyframes` | Optional array of `{ image, frame_index, strength? }`. Must not be combined with `image` / `last_image`. |
| `references` | Optional array of `{ type: "image"\|"video"\|"audio", data }`. Must not be only audio without visual conditioning. |
| `video` / `mask_video` / `audio` | Optional data URLs. `mask_video` requires `video` or `animate`. `audio` requires `image` or `references`. |
| `strength` | Optional number in `[0, 1]`. |
| `conditioning_scale` | Optional number in `[0, 2]`. |
| `animate` | Optional object. Nested fields below. |
| `task_config` | Object (schema default empty object). Nested fields below. |
| `pipeline_args` | Optional object for model-family-specific kwargs not covered by `task_config`. Must not use reserved keys listed below. |
| `loras` | Optional list. Nested fields below. Prefer the catalog row. |
| `components` | Optional map. Nested fields below. Prefer the catalog row. Changes the required model file set. |
| `quantization_config` | Optional object or `null`. Nested fields below. Prefer the catalog row. Crynux AS accepts only `quant_method` `bitsandbytes` here; GGUF uses `components`. |

Input mode is derived from which media fields you set: `t2v`, `i2v`, `flf2v`, `keyframes`, `r2v`, `v2v`, `animate`, or `a2v`.

### `task_config`

| Field | Handling |
|-------|----------|
| `width` | Optional positive integer, maximum `4096`. For Crynux AS fee: when omitted, AS uses the decoded width of the first conditioning image (`image`, then `last_image`, then `keyframes[0].image`, then the first `references` entry with `type` `image`); with no conditioning image, AS uses `832`. A conditioning image used for dimensions must be a base64 image data URL. |
| `height` | Optional positive integer, maximum `4096`. Same AS fill order as `width`; default without an image is `480`. |
| `num_frames` | Optional positive integer, maximum `1025`. Schema / AS fee default when omitted: `81`. `0` fails create. |
| `fps` | Optional number in `(0, 120]`. Not used in AS fee. When omitted, the runner uses the pipeline default. |
| `steps` | Optional positive integer, maximum `100`. Schema / AS fee default when omitted: `30`. `0` fails create. |
| `seed` | Integer in `[0, 2147483647]`. Optional: when omitted, AS writes a random value in that range before persist. When present (including `0`), AS keeps it unchanged. |
| `guidance_scale` | Optional number `>= 0`, or `null`. When omitted or `null`, the runner uses the pipeline default / guider. Model-specific secondary guidance belongs in `pipeline_args` (for example `guidance_scale_2`), not here. Not used in AS fee. |

AS fee units: `width * height * num_frames * steps`.

### `animate`

| Field | Handling |
|-------|----------|
| `mode` | `"animate"` (default) or `"replace"`. |
| `pose_video` | Optional data URL. |
| `face_video` | Optional data URL. |
| `background_video` | Required when `mode` is `"replace"`. |
| `prev_segment_conditioning_frames` | Optional integer in `(0, 16]`. |

When `mode` is `"replace"`, `mask_video` is also required on the top-level TaskArgs.

### `pipeline_args`

Optional JSON object for checkpoint-specific generation kwargs (dual-expert guidance, audio guidance, and similar). Must not contain reserved keys that already have first-class TaskArgs fields, including: `prompt`, `negative_prompt`, `image`, `last_image`, `video`, `mask_video`, `width`, `height`, `num_frames`, `fps`, `num_inference_steps`, `guidance_scale`, `true_cfg_scale`, `strength`, `conditioning_scale`, `generator`, and the other keys reserved in the video schema / runner.

### `loras`

Omit the field or use `[]` for no LoRA. Otherwise a list of objects:

| Field | Handling |
|-------|----------|
| `model` | Required. HuggingFace LoRA repository id (normalized to lowercase by AS). |
| `revision` | Optional string or `null`. |
| `weight_file_name` | Optional string. Default `""`. |
| `weight` | Optional integer in `(0, 100]`. Default `100`. |
| `target` | Optional. One of `auto` (default), `transformer`, `transformer_2`, `transformer_ref`, `both`. Routes the adapter onto MoE / dual-transformer checkpoints. |

LoRA entries are **not** part of the required model file set and do **not** cause `model_not_ready`. They are part of the run-config identity.

### `components`

Omit the field or use `{}` for no overrides. Otherwise a map from **pipeline component name** (must exist in the base repo `model_index.json` or `modular_model_index.json`) to a source object. Each entry replaces that component; overridden weights are **not** loaded from `base_model`.

| Field | Handling |
|-------|----------|
| `format` | Required non-empty string. Current adapters: `gguf`, `diffusers`. |
| `repo` | Required. HuggingFace repository for the replacement. |
| `revision` | Optional string, or `null`. |
| `format_kwargs` | Optional object (default `{}`). Keys depend on `format`. |

**`format: "gguf"`**

| `format_kwargs` key | Handling |
|---------------------|----------|
| `filename` | Required. Exact GGUF file name inside `repo`. |

Current GGUF coverage includes `transformer`, and on dual-expert / ref pipelines also `transformer_2` and `transformer_ref` when those names exist in the index. GGUF is a **component replacement**, not `quantization_config`. Set `quantization_config` to `null` (or omit) when only using GGUF, unless you also BitsAndBytes-quantize a **different** unquantized component. The same component must not receive two quantization methods.

**`format: "diffusers"`**

| `format_kwargs` key | Handling |
|---------------------|----------|
| `subfolder` | Optional. Non-empty subdirectory inside `repo`. Do not set `filename`. |

`components` participates in the required model file set: a different `repo` / `format` / relevant `format_kwargs` can change on-disk readiness and can cause `model_not_ready`.

### `quantization_config`

Task-time dynamic quantization for Diffusers/Transformers components.

| Value | Handling |
|-------|----------|
| omit / `null` | No task-time quantization. Use for plain float pipelines, GGUF-only via `components`, or pipelines that already embed quantization — do not duplicate embedded backends here. |
| object | Crynux AS requires `quant_method` = `bitsandbytes` for Video tasks. Any other method (including `gguf` here) fails create with HTTP 400. |

Object fields (all required when the object is present):

| Field | Handling |
|-------|----------|
| `quant_method` | Must be `"bitsandbytes"` on Crynux AS Video create. |
| `components_to_quantize` | Non-empty array of unique component names from the pipeline index. Coverage includes `transformer`, pairs such as `transformer` + `text_encoder`, and dual-expert sets such as `transformer` + `transformer_2` when those names exist. One config applies the same `quant_kwargs` to every listed name. |
| `quant_kwargs` | Object. Only the keys below are allowed; unknown keys fail AS create. Must set **exactly one** of `load_in_4bit: true` or `load_in_8bit: true`. |

#### BitsAndBytes 4-bit (`load_in_4bit: true`)

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `load_in_4bit` | `true` (required for this mode). |
| `load_in_8bit` | Must be absent or `false`. |
| `bnb_4bit_quant_type` | Optional. Allowed: `nf4`, `fp4`. |
| `bnb_4bit_compute_dtype` | Optional. Allowed: `float16`, `bfloat16`, `float32`. When omitted, the runner uses the task's concrete `dtype`. |
| `bnb_4bit_use_double_quant` | Optional boolean. |
| `bnb_4bit_quant_storage` | Optional. Allowed: `float16`, `bfloat16`, `float32`, `float64`, `int8`, `uint8`. |
| `llm_int8_*` | If present, still subject to the shared key allowlist / false-only rules below. |

#### BitsAndBytes INT8 (`load_in_8bit: true`)

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `load_in_8bit` | `true` (required for this mode). |
| `load_in_4bit` | Must be absent or `false`. |
| `llm_int8_threshold` | Optional. JSON number whose literal has a fractional part or exponent (write `6.0`, not `6`). |
| `llm_int8_skip_modules` | Optional. `null` or an array of strings. |
| `llm_int8_enable_fp32_cpu_offload` | Optional; must be `false` if present. |
| `llm_int8_has_fp16_weight` | Optional; must be `false` if present. |

`quantization_config` does **not** change the required download file set by itself. It does change run-config identity. Prefer copying the full `list_models` row.

## Examples

### Text to video with Bitsandbytes

```json
{
  "version": "3.8.0",
  "base_model": {"name": "Wan-AI/Wan2.2-TI2V-5B-Diffusers"},
  "dtype": "bfloat16",
  "prompt": "A golden retriever runs across a sunny meadow, cinematic natural lighting",
  "negative_prompt": "blurry, low quality, static, deformed",
  "task_config": {
    "width": 832,
    "height": 480,
    "num_frames": 49,
    "fps": 24,
    "steps": 20,
    "seed": 0,
    "guidance_scale": 5.0
  },
  "quantization_config": {
    "quant_method": "bitsandbytes",
    "components_to_quantize": ["transformer", "text_encoder"],
    "quant_kwargs": {
      "load_in_4bit": true,
      "bnb_4bit_quant_type": "nf4",
      "bnb_4bit_compute_dtype": "bfloat16",
      "bnb_4bit_use_double_quant": false
    }
  }
}
```

### Image to video

Same as text-to-video, plus an `image` data URL (and optionally keep the same quantization / components as the catalog row).

```json
{
  "version": "3.8.0",
  "base_model": {"name": "Wan-AI/Wan2.2-TI2V-5B-Diffusers"},
  "dtype": "bfloat16",
  "prompt": "The camera slowly pushes in while the subject turns to smile",
  "image": "data:image/png;base64,...",
  "task_config": {
    "width": 832,
    "height": 480,
    "num_frames": 49,
    "fps": 24,
    "steps": 20,
    "seed": 0,
    "guidance_scale": 5.0
  },
  "quantization_config": {
    "quant_method": "bitsandbytes",
    "components_to_quantize": ["transformer", "text_encoder"],
    "quant_kwargs": {
      "load_in_4bit": true,
      "bnb_4bit_quant_type": "nf4",
      "bnb_4bit_compute_dtype": "bfloat16",
      "bnb_4bit_use_double_quant": false
    }
  }
}
```

### First-last-frame style

Use a model that supports FLF2V, set both `image` and `last_image`, and keep the row's `dtype` / `quantization_config` / `components` when taken from `list_models`.

```json
{
  "version": "3.8.0",
  "base_model": {"name": "Wan-AI/Wan2.1-FLF2V-14B-720P-Diffusers"},
  "dtype": "bfloat16",
  "prompt": "Smooth motion between the first and last frames",
  "image": "data:image/png;base64,...",
  "last_image": "data:image/png;base64,...",
  "task_config": {
    "width": 832,
    "height": 480,
    "num_frames": 49,
    "fps": 16,
    "steps": 20,
    "seed": 0,
    "guidance_scale": 5.0
  },
  "quantization_config": {
    "quant_method": "bitsandbytes",
    "components_to_quantize": ["transformer", "text_encoder"],
    "quant_kwargs": {
      "load_in_4bit": true,
      "bnb_4bit_quant_type": "nf4",
      "bnb_4bit_compute_dtype": "bfloat16",
      "bnb_4bit_use_double_quant": false
    }
  }
}
```

More runner examples: https://github.com/crynux-network/stable-diffusion-task/tree/main/examples
