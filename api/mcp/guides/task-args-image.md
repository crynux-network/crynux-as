# Image TaskArgs

Schema (main): https://github.com/crynux-network/stable-diffusion-task/blob/main/schema/image-task.json

`task_type` for create tools is `0`. Prefer copying `dtype`, `quantization_config`, `components`, and `lora` from one `list_models` image row together with `base_model.name`.

## Fields

| Field | Notes |
|-------|--------|
| `version` | Runner TaskArgs version string (required). |
| `base_model` | `{ "name", "revision"? }` (required). |
| `prompt` | Text prompt (required). |
| `task_config` | Required object. Every nested field is listed below. |
| `dtype` | Concrete dtype: `float16`, `bfloat16`, or `float32`. Prefer the catalog row. |
| `negative_prompt` | Optional. |
| `images` | Optional data-URL inputs for edit / img2img. |
| `strength` | Optional for image-conditioned runs (`0`–`1`). |
| `lora` | Optional. Nested fields below. Prefer the catalog row value (`null` or object) as a set with `dtype` / `components` / `quantization_config`. |
| `components` | Optional. Nested fields below. Prefer the catalog row. Changes the required model file set. |
| `quantization_config` | Optional object or `null`. Nested fields below. Prefer the catalog row. Crynux AS accepts only `quant_method` `bitsandbytes` here; GGUF uses `components`, not this object. |

### `task_config`

| Field | Handling |
|-------|----------|
| `image_width` | Positive integer. Required for Crynux AS create (fee uses it). Schema default is `1024` if the runner applies defaults; AS does **not** fill an omitted value — omit or `0` fails create. |
| `image_height` | Positive integer. Same AS rule as `image_width`. Schema default is `1024`. |
| `steps` | Positive integer, maximum `100`. Required for Crynux AS create. Schema default is `30`; AS does **not** fill an omitted value — omit or `0` fails create. |
| `num_images_per_prompt` | Positive integer, maximum `10`. Optional on create: when omitted, AS treats it as `1` for fee and result URL count. Schema default is `1`. |
| `seed` | Integer in `[0, 2147483647]`. Optional: when omitted, AS writes a random value in that range before persist. When present (including `0`), AS keeps it unchanged. |
| `guidance_scale` | Optional number `>= 0`, or `null`. When omitted or `null`, the runner uses the pipeline default. When set, the runner maps it to the pipeline's guidance argument (`guidance_scale` or `true_cfg_scale`). Not used in AS fee calculation. |

### `lora`

Omit the field or set `null` for no LoRA. Otherwise one object:

| Field | Handling |
|-------|----------|
| `model` | Required. HuggingFace LoRA repository id (normalized to lowercase by AS). |
| `weight_file_name` | Optional string. Default `""` (runner picks the default weight file in the repo). Set when the repo has multiple weight files. |
| `weight` | Optional integer in `(0, 100]`. Default `100`. Strength as percent (100 = full). |

LoRA is **not** part of the required model file set and does **not** cause `model_not_ready`. It is part of the run-config identity. Load order on the node: base (+ quantized components) first, then floating-point LoRA, without fusing into base weights.

### `components`

Omit the field or use `{}` for no overrides. Otherwise a map from **pipeline component name** (must exist in the base repo `model_index.json`) to a source object. Each entry replaces that component; the overridden weights are **not** loaded from `base_model`.

| Field | Handling |
|-------|----------|
| `format` | Required non-empty string. Current adapters: `gguf`, `diffusers`. |
| `repo` | Required. HuggingFace repository that holds the replacement component. |
| `revision` | Optional string, or `null`. Hub ref / commit; `null` or omit lets Relay freeze a commit. |
| `format_kwargs` | Optional object (default `{}`). Keys depend on `format` (below). |

**`format: "gguf"`** (current adapter: `transformer` only)

| `format_kwargs` key | Handling |
|---------------------|----------|
| `filename` | Required. Exact GGUF file name inside `repo`. |

GGUF is a **component replacement**, not `quantization_config`. Set `quantization_config` to `null` (or omit) when only using GGUF, unless you also BitsAndBytes-quantize a **different** unquantized component. The same component must not receive two quantization methods. Runner-recognized GGUF tensor types include `BF16`, `Q4_0`, `Q4_1`, `Q5_0`, `Q5_1`, `Q8_0`, and `Q2_K`–`Q6_K`; support is validated from tensor types in the file, not from the filename string alone.

**`format: "diffusers"`** (any component name declared in `model_index.json`)

| `format_kwargs` key | Handling |
|---------------------|----------|
| `subfolder` | Optional. Non-empty subdirectory inside `repo`. Do not set `filename`. |

`components` participates in the required model file set: a different `repo` / `format` / relevant `format_kwargs` can change on-disk readiness and can cause `model_not_ready`.

### `quantization_config`

Task-time dynamic quantization applied when loading Diffusers/Transformers components from the base pipeline (or unquantized replacements).

| Value | Handling |
|-------|----------|
| omit / `null` | No task-time quantization. Use this for plain float pipelines, GGUF-only via `components`, or pipelines that already embed quantization (for example Nunchaku Lite) — do not duplicate embedded backends here. |
| object | Crynux AS requires `quant_method` = `bitsandbytes` for Image tasks. Any other method (including putting `gguf` here) fails create with HTTP 400. |

Object fields (all required when the object is present):

| Field | Handling |
|-------|----------|
| `quant_method` | Must be `"bitsandbytes"` on Crynux AS Image create. |
| `components_to_quantize` | Non-empty array of unique component names from `model_index.json`. Current coverage includes `transformer` alone, and `transformer` + `text_encoder` together. One config applies the same `quant_kwargs` to every listed name. |
| `quant_kwargs` | Object. Only the keys below are allowed; unknown keys fail AS create. Must set **exactly one** of `load_in_4bit: true` or `load_in_8bit: true` (not both, not neither). |

#### BitsAndBytes 4-bit (`load_in_4bit: true`)

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `load_in_4bit` | `true` (required for this mode). |
| `load_in_8bit` | Must be absent or `false`. |
| `bnb_4bit_quant_type` | Optional. Allowed: `nf4`, `fp4`. |
| `bnb_4bit_compute_dtype` | Optional. Allowed: `float16`, `bfloat16`, `float32`. When omitted, the runner uses the task's concrete `dtype`. |
| `bnb_4bit_use_double_quant` | Optional boolean. |
| `bnb_4bit_quant_storage` | Optional. Allowed: `float16`, `bfloat16`, `float32`, `float64`, `int8`, `uint8`. |
| `llm_int8_*` | Not used for 4-bit; if present, still subject to the shared key allowlist / false-only rules below. |

#### BitsAndBytes INT8 (`load_in_8bit: true`)

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `load_in_8bit` | `true` (required for this mode). |
| `load_in_4bit` | Must be absent or `false`. |
| `llm_int8_threshold` | Optional. JSON number whose literal has a fractional part or exponent (write `6.0`, not `6`). |
| `llm_int8_skip_modules` | Optional. `null` or an array of strings. |
| `llm_int8_enable_fp32_cpu_offload` | Optional; must be `false` if present (CPU offload is rejected). |
| `llm_int8_has_fp16_weight` | Optional; must be `false` if present. |

`quantization_config` does **not** change the required download file set by itself (weights still come from `base_model` / `components`). It does change run-config identity. Prefer copying the full `list_models` row rather than inventing a new quant mix.

## Examples by VRAM tier

Which model and quant to pick for each GPU class is listed in `crynux://guides/vram-models`. Create with `task_type=0` and the matching `min_vram`.

### 24 GB — `Qwen/Qwen-Image-2.1-Turbo` GGUF Q4 + text_encoder BitsAndBytes NF4

```json
{
  "version": "3.7.1",
  "base_model": {"name": "Qwen/Qwen-Image-2.1-Turbo", "revision": null},
  "dtype": "bfloat16",
  "prompt": "A product photo of a white ceramic cup on a wooden table",
  "task_config": {
    "image_width": 1024,
    "image_height": 1024,
    "steps": 8,
    "seed": 0,
    "num_images_per_prompt": 1
  },
  "components": {
    "transformer": {
      "format": "gguf",
      "repo": "chilozk1022/Qwen-Image-2.1-Turbo-Quantized",
      "revision": null,
      "format_kwargs": {"filename": "qwen-image-2.1-turbo-Q4_K_M.gguf"}
    }
  },
  "quantization_config": {
    "quant_method": "bitsandbytes",
    "components_to_quantize": ["text_encoder"],
    "quant_kwargs": {
      "load_in_4bit": true,
      "bnb_4bit_quant_type": "nf4",
      "bnb_4bit_compute_dtype": "bfloat16",
      "bnb_4bit_use_double_quant": false
    }
  }
}
```

### 32 GB — `Qwen/Qwen-Image-2.1` BitsAndBytes NF4

```json
{
  "version": "3.7.1",
  "base_model": {"name": "Qwen/Qwen-Image-2.1", "revision": null},
  "dtype": "bfloat16",
  "prompt": "A product photo of a white ceramic cup on a wooden table",
  "task_config": {
    "image_width": 1024,
    "image_height": 1024,
    "steps": 8,
    "seed": 0,
    "num_images_per_prompt": 1
  },
  "components": {},
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

### 80 GB — `black-forest-labs/FLUX.2-klein-4B` bf16

```json
{
  "version": "3.7.1",
  "base_model": {"name": "black-forest-labs/FLUX.2-klein-4B", "revision": null},
  "dtype": "bfloat16",
  "prompt": "A product photo of a white ceramic cup on a wooden table",
  "task_config": {
    "image_width": 1024,
    "image_height": 1024,
    "steps": 4,
    "seed": 0,
    "num_images_per_prompt": 1,
    "guidance_scale": 1
  },
  "quantization_config": null
}
```

### 96 GB — `Qwen/Qwen-Image-2.1` bf16

```json
{
  "version": "3.7.1",
  "base_model": {"name": "Qwen/Qwen-Image-2.1", "revision": null},
  "dtype": "bfloat16",
  "prompt": "A product photo of a white ceramic cup on a wooden table",
  "task_config": {
    "image_width": 1024,
    "image_height": 1024,
    "steps": 8,
    "seed": 0,
    "num_images_per_prompt": 1
  },
  "components": {},
  "quantization_config": null
}
```

### LoRA (optional)

`lora` is not part of the required model file set. Use a LoRA trained for the same base as `base_model.name`. Example on the 96 GB `Qwen/Qwen-Image-2.1` bf16 stack — replace `lora.model` with a HuggingFace LoRA repo for that base:

```json
{
  "version": "3.7.1",
  "base_model": {"name": "Qwen/Qwen-Image-2.1", "revision": null},
  "dtype": "bfloat16",
  "prompt": "A product photo of a white ceramic cup on a wooden table",
  "task_config": {
    "image_width": 1024,
    "image_height": 1024,
    "steps": 8,
    "seed": 0,
    "num_images_per_prompt": 1
  },
  "components": {},
  "quantization_config": null,
  "lora": {
    "model": "<huggingface-lora-repo-for-qwen-image-2.1>",
    "weight_file_name": "pytorch_lora_weights.safetensors",
    "weight": 100
  }
}
```

More runner examples: https://github.com/crynux-network/stable-diffusion-task/tree/main/examples
