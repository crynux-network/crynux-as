# Model Compatibility Architecture

## Scope

Crynux AS owns the public OpenAI-compatible contract for its Chat Completions, Completions, Responses, Images generations, and Images edits endpoints. For Chat Completions, Completions, and Responses it MUST convert accepted public fields to canonical `GPTTaskArgs`, persist that canonical JSON, submit it through the Bridge raw-task API, and convert raw `GPTTaskResponse` text to the public response. For Images generations and Images edits it MUST convert accepted OpenAI Images fields, plus optional top-level `quantization_config` and `components`, to Image TaskArgs, persist that TaskArgs JSON, submit it through the Bridge raw-task API, store PNG results under `llm.task_result_dir`, and encode those PNG files into the public Images HTTP response without persisting base64 image payloads in MySQL. Images edits MUST additionally convert input images to non-empty TaskArgs `images` data URLs. Images generations and Images edits MUST NOT accept client `dtype` or `lora`; AS MUST resolve `dtype` and any omitted `quantization_config` and `components` through the shared Image loaded-models resolver from one matching row, MUST write `dtype` `bfloat16` and keep the request fields unchanged when no row matches, and MUST NOT combine fields from different rows. Raw Image and Raw Video create MUST use the same resolver with the cache of their task type: a client `dtype` MUST be `float16`, `bfloat16`, or `float32` and passes through unchanged, any other client `dtype` including `auto` MUST be rejected, and an omitted `dtype` MUST resolve from loaded-models or default to `bfloat16`.

Bridge, Relay, Node, and Worker MUST transport canonical task arguments and raw task results without interpreting structured-output or tool-call semantics. gpt-task owns prompt rendering and constrained token generation.

## Request Conversion

Chat Completions `response_format` and Responses `text.format` MUST produce the same canonical `response_format`. Function tools from both APIs MUST produce the same canonical nested function-tool shape. Named Responses choices MUST produce the canonical named-function choice shape.

Canonical `tool_choice` MUST be `none`, `auto`, `required`, or one named function. Named choices MUST reference exactly one declared tool. Unknown canonical fields and unsupported public structured-output extensions MUST be rejected.

Assistant tool-call history and tool results MUST retain source order. `previous_response_id` MUST expand the retained previous job into canonical history before the new task is persisted.

Chat Completions and Responses MUST accept public `chat_template_kwargs` as a map. AS MUST copy that map into canonical `template_args` without renaming keys and without selecting keys by model ID.

Chat Completions MUST accept top-level `reasoning_effort`. Responses MUST accept nested `reasoning.effort`. When the corresponding effort value is non-empty and canonical `template_args` does not already contain `enable_thinking`, AS MUST inject `enable_thinking=false` for `"none"` and `enable_thinking=true` for any other non-empty effort value. An explicit `enable_thinking` entry in `chat_template_kwargs` MUST take precedence over that injection. AS MUST NOT translate effort into other template keys such as `thinking`. Templates that do not consume `enable_thinking` MUST receive that injected key unchanged; gpt-task owns whether the selected template accepts, filters, or ignores it. When both public fields are absent or empty, AS MUST omit `template_args`.

## Raw Task Transport

AS MUST persist the complete canonical request in `llm_jobs.task_args_json`. When canonical LLM TaskArgs omit `seed`, or Image TaskArgs omit `task_config.seed`, AS MUST set that field to a uniformly chosen integer from 0 through 2147483647 before persistence. A seed supplied by the client, including 0, MUST be kept. Its worker MUST submit that exact JSON through Bridge raw-task creation. Bridge and Relay schema validation MUST use the matching gpt-task schema version. Worker MUST deserialize the canonical object and invoke gpt-task without API-specific conversion.

When AS parses, rewrites, persists, catalogs, or forwards `quantization_config`, and when AS parses or forwards Image TaskArgs `components`, it MUST preserve JSON number literals. AS MUST NOT decode those objects into generic maps and re-encode them in a way that changes number literals. When a field inside those objects must be a Python `float` on the worker (for example `quant_kwargs.llm_int8_threshold`), a value written as `6.0` MUST remain `6.0` in the persisted TaskArgs and in catalog API responses. AS MUST NOT rewrite an integer literal `6` into `6.0`.

After JSON Schema validation and execution-parameter completion, and before Credits reservation, AS MUST validate the final TaskArgs `quantization_config` for worker compatibility. When `quantization_config` is absent or null, that check MUST pass. For Image and Video tasks, `quant_method` MUST be `bitsandbytes`. For LLM tasks, `quant_method` MUST be `bitsandbytes` or `fp8`. When `quant_method` is `bitsandbytes`, AS MUST reject unknown `quant_kwargs` keys, MUST require exactly one of `load_in_4bit` or `load_in_8bit` to be true, MUST require `llm_int8_threshold` (when present) to be a JSON number whose literal includes a fractional part or exponent (for example `6.0`, not `6`), MUST reject `llm_int8_enable_fp32_cpu_offload` or `llm_int8_has_fp16_weight` set to true, and MUST restrict `bnb_4bit_quant_type`, `bnb_4bit_compute_dtype`, and `bnb_4bit_quant_storage` to the values accepted by Crynux BitsAndBytes profiles. A failure MUST return HTTP 400 and MUST NOT reserve Credits or create a job.

The required task version MUST be `3.7.1`. Relay MUST dispatch the task only to a node whose runner version has major version `3` and minor and patch version not lower than `3.7.1`. Canonical LLM TaskArgs MUST include `version` set to `3.7.1`. Quantization MUST use `quantization_config` and MUST NOT use `quantize_bits`.

## Raw Output Conversion

gpt-task MUST return generated assistant text without creating public API tool-call objects. AS MUST select output parsers from generated syntax, not from model ID.

The parser registry MUST cover `llama`, `kimi`, `deepseek_r1`, `deepseek_v3_1`, `deepseek_v3_2`, `deepseek_v4`, `qwen_3`, `qwen_3_coder`, `qwen_3_5`, `glm_4_7`, `hermes`, `hy_v4`, and `kimi_k3`.

When canonical `tool_choice` is `none`, AS MUST retain the complete assistant text and MUST NOT parse a tool call. When a parser succeeds, AS MUST remove only its matched syntax, preserve other content in source order, validate that every function name was declared, and emit ordered public function calls. A named choice MAY return a bare JSON arguments object; AS MUST attach the selected canonical function name.

Canonical `response_format` output MUST remain assistant content or Responses `output_text`. AS MUST NOT parse it as a function call or modify its JSON representation.

Chat Completions, Responses, simulated SSE, and previous-response history reconstruction MUST use the same canonical-aware parser behavior.
