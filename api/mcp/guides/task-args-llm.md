# LLM TaskArgs

Schema (main): https://github.com/crynux-network/gpt-task/blob/main/schema/gpt-inference-task.json

`task_type` for create tools is `1`. Prefer copying `dtype` and `quantization_config` from one `list_models` chat row together with `model`.

## Fields

| Field | Notes |
|-------|--------|
| `version` | Runner TaskArgs version string (required). |
| `model` | HuggingFace model id (required). Normalized to lowercase by AS. |
| `messages` | Chat messages array (required). Nested shape below. |
| `revision` | Optional Hub revision string, or `null`. |
| `dtype` | On Crynux AS Raw create: must be concrete `float16`, `bfloat16`, or `float32` (case-insensitive; stored lowercase). `auto` and other values fail create. Prefer the catalog row. |
| `quantization_config` | Optional object or `null`. Nested fields below. Prefer the catalog row. |
| `generation_config` | Optional object or `null`. Nested fields below. |
| `tools` | Optional array of function tools, or `null`. Nested shape below. |
| `tool_choice` | Optional. Nested values below. |
| `response_format` | Optional. Nested values below. |
| `template_args` | Optional object or `null`. Opaque chat-template kwargs (for example thinking flags). Copied as-is; the runner decides which keys the template uses. |
| `seed` | Optional integer. Nested handling below. |

Required model files for LLM are the base `model` repository only. `dtype` and `quantization_config` do **not** change that file set and do **not** by themselves cause `model_not_ready`. They do change run-config identity.

### `messages`

Each message is an object:

| Field | Handling |
|-------|----------|
| `role` | `system`, `user`, `assistant`, or `tool`. |
| `content` | String, `null`, or an array of content blocks. Text block: `{ "type": "text", "text": "..." }`. Image block: `{ "type": "image", "base64": "..." }` (raw base64, not a data URL). |
| `tool_call_id` | Optional string or `null` (tool-result messages). |
| `tool_calls` | Optional array or `null` (assistant tool-call history). |

### `generation_config`

Omit or `null` to use runner defaults. When present, schema fields are:

| Field | Handling |
|-------|----------|
| `max_new_tokens` | Optional integer. |
| `stop_strings` | Optional array of strings. |
| `do_sample` | Optional boolean. |
| `num_beams` | Optional integer. |
| `temperature` | Optional number. |
| `typical_p` | Optional number. |
| `top_k` | Optional integer. |
| `top_p` | Optional number. |
| `min_p` | Optional number. |
| `repetition_penalty` | Optional number. |
| `num_return_sequences` | Optional integer. |

### `tools` / `tool_choice` / `response_format`

**`tools`:** array of `{ "type": "function", "function": { "name", "description"?, "parameters"?, "strict"? } }`. `name` is required on each function.

**`tool_choice`:** `null`, `"none"`, `"auto"`, `"required"`, or `{ "type": "function", "function": { "name": "..." } }`.

**`response_format`:** `null`, `{ "type": "json_object" }`, or `{ "type": "json_schema", "json_schema": { "name", "schema", "description"?, "strict"? } }` (`name` and `schema` required for `json_schema`).

### `seed`

Integer. Optional: when omitted, AS writes a random value in `[0, 2147483647]` before persist. When present (including `0`), AS keeps it unchanged.

### `quantization_config`

| Value | Handling |
|-------|----------|
| omit / `null` | No task-time quantization. Use this for plain float models and for Hub checkpoints that already embed quantization (AWQ, GPTQ, compressed-tensors, and similar). Do not put those methods in `quant_method`; use the quantized Hub repo as `model` and leave this `null`. |
| object | Crynux AS allows `quant_method` `bitsandbytes` or `fp8` only. Any other method fails create with HTTP 400. |

Object fields (both required when the object is present):

| Field | Handling |
|-------|----------|
| `quant_method` | `"bitsandbytes"` or `"fp8"`. |
| `quant_kwargs` | Object. For `bitsandbytes`, only the keys below are allowed and the 4-bit / INT8 rules apply. For `fp8`, AS does not re-validate kwargs keys; use the runner FP8 shape below. |

LLM TaskArgs have **no** `components_to_quantize` field (unlike Image/Video).

#### BitsAndBytes 4-bit (`quant_method: "bitsandbytes"`, `load_in_4bit: true`)

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `load_in_4bit` | `true` (required for this mode). |
| `load_in_8bit` | Must be absent or `false`. Exactly one of `load_in_4bit` / `load_in_8bit` must be true. |
| `bnb_4bit_quant_type` | Optional. Allowed: `nf4`, `fp4`. |
| `bnb_4bit_compute_dtype` | Optional. Allowed: `float16`, `bfloat16`, `float32`. When omitted, the runner uses the task's concrete `dtype`. |
| `bnb_4bit_use_double_quant` | Optional boolean. |
| `bnb_4bit_quant_storage` | Optional. Allowed: `float16`, `bfloat16`, `float32`, `float64`, `int8`, `uint8`. |
| `llm_int8_threshold` | Optional; if present, JSON number literal must include a fractional part or exponent (`6.0`, not `6`). |
| `llm_int8_skip_modules` | Optional; `null` or an array of strings. |
| `llm_int8_enable_fp32_cpu_offload` | Optional; must be `false` if present. |
| `llm_int8_has_fp16_weight` | Optional; must be `false` if present. |
| any other key | Rejected by AS create. |

#### BitsAndBytes INT8 (`quant_method: "bitsandbytes"`, `load_in_8bit: true`)

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `load_in_8bit` | `true` (required for this mode). |
| `load_in_4bit` | Must be absent or `false`. |
| `llm_int8_threshold` | Optional. Write `6.0`, not `6`. |
| `llm_int8_skip_modules` | Optional. `null` or an array of strings. |
| `llm_int8_enable_fp32_cpu_offload` | Optional; must be `false` if present. |
| `llm_int8_has_fp16_weight` | Optional; must be `false` if present. |
| `bnb_4bit_*` | Optional if present; still restricted to the allowlist / value sets above. |
| any other key | Rejected by AS create. |

#### Fine-grained FP8 (`quant_method: "fp8"`)

Runner-supported shape:

| `quant_kwargs` key | Handling |
|--------------------|----------|
| `activation_scheme` | e.g. `"dynamic"`. |
| `weight_block_size` | e.g. `[128, 128]`. |
| `scale_fmt` | e.g. `"float"` or `"ue8m0"`. |
| `dequantize` | Boolean. `true` loads as floating-point execution, not as FP8 quantized execution. |

Do not combine a non-null task-time `quantization_config` with a Hub checkpoint that already embeds its own quantization metadata; the runner rejects that overlap.

## Examples by VRAM tier

Which model and quant to pick for each GPU class is listed in `crynux://guides/vram-models`. Create with `task_type=1` and the matching `min_vram`.

### 24 GB — `Qwen/Qwen3.5-9B` bf16

```json
{
  "version": "3.7.0",
  "model": "Qwen/Qwen3.5-9B",
  "dtype": "bfloat16",
  "messages": [{"role": "user", "content": "Say hello in one sentence."}],
  "generation_config": {"max_new_tokens": 256},
  "quantization_config": null,
  "seed": 42
}
```

### 32 GB — `Qwen/Qwen3.8-27B` BitsAndBytes INT8

```json
{
  "version": "3.7.0",
  "model": "Qwen/Qwen3.8-27B",
  "dtype": "bfloat16",
  "messages": [{"role": "user", "content": "Say hello in one sentence."}],
  "generation_config": {"max_new_tokens": 256},
  "quantization_config": {
    "quant_method": "bitsandbytes",
    "quant_kwargs": {
      "load_in_8bit": true,
      "llm_int8_threshold": 6.0
    }
  },
  "seed": 42
}
```

### 80 GB / 96 GB — `Qwen/Qwen3.8-27B` bf16

```json
{
  "version": "3.7.0",
  "model": "Qwen/Qwen3.8-27B",
  "dtype": "bfloat16",
  "messages": [{"role": "user", "content": "Say hello in one sentence."}],
  "generation_config": {"max_new_tokens": 256},
  "quantization_config": null,
  "seed": 42
}
```

Use `min_vram=80` or `min_vram=96`.

### BitsAndBytes 4-bit NF4 (optional)

```json
{
  "version": "3.7.0",
  "model": "Qwen/Qwen3.8-27B",
  "dtype": "bfloat16",
  "messages": [{"role": "user", "content": "Say hello in one sentence."}],
  "generation_config": {"max_new_tokens": 256},
  "quantization_config": {
    "quant_method": "bitsandbytes",
    "quant_kwargs": {
      "load_in_4bit": true,
      "bnb_4bit_quant_type": "nf4",
      "bnb_4bit_compute_dtype": "bfloat16",
      "bnb_4bit_use_double_quant": false
    }
  },
  "seed": 42
}
```

### Fine-grained FP8 (optional)

```json
{
  "version": "3.7.0",
  "model": "Qwen/Qwen3.8-27B",
  "dtype": "bfloat16",
  "messages": [{"role": "user", "content": "Say hello in one sentence."}],
  "generation_config": {"max_new_tokens": 64},
  "quantization_config": {
    "quant_method": "fp8",
    "quant_kwargs": {
      "activation_scheme": "dynamic",
      "weight_block_size": [128, 128],
      "scale_fmt": "float",
      "dequantize": false
    }
  },
  "seed": 42
}
```

### Pre-quantized Hub model (optional)

Use the quantized Hub repo as `model` and set `quantization_config` to `null`. Linux / Docker only for AWQ / GPTQ.

```json
{
  "version": "3.7.0",
  "model": "mattbucci/Qwen3.8-27B-AWQ",
  "dtype": "bfloat16",
  "messages": [{"role": "user", "content": "Say hello in one sentence."}],
  "generation_config": {"max_new_tokens": 64},
  "quantization_config": null,
  "seed": 42
}
```

More runner examples: https://github.com/crynux-network/gpt-task/tree/main/examples
