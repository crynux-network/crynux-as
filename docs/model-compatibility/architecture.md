# Model Compatibility Architecture

## Scope

Crynux AS owns the public OpenAI-compatible contract for its Chat Completions, Completions, and Responses endpoints. It MUST convert accepted public fields to canonical `GPTTaskArgs`, persist that canonical JSON, submit it through the Bridge raw-task API, and convert raw `GPTTaskResponse` text to the public response.

Bridge, Relay, Node, and Worker MUST transport canonical task arguments and raw task results without interpreting structured-output or tool-call semantics. gpt-task owns prompt rendering and constrained token generation.

## Request Conversion

Chat Completions `response_format` and Responses `text.format` MUST produce the same canonical `response_format`. Function tools from both APIs MUST produce the same canonical nested function-tool shape. Named Responses choices MUST produce the canonical named-function choice shape.

Canonical `tool_choice` MUST be `none`, `auto`, `required`, or one named function. Named choices MUST reference exactly one declared tool. Unknown canonical fields and unsupported public structured-output extensions MUST be rejected.

Assistant tool-call history and tool results MUST retain source order. `previous_response_id` MUST expand the retained previous job into canonical history before the new task is persisted.

Chat Completions and Responses MUST accept public `chat_template_kwargs` as a map. AS MUST copy that map into canonical `template_args` without renaming keys and without selecting keys by model ID.

Chat Completions MUST accept top-level `reasoning_effort`. Responses MUST accept nested `reasoning.effort`. When the corresponding effort value is non-empty and canonical `template_args` does not already contain `enable_thinking`, AS MUST inject `enable_thinking=false` for `"none"` and `enable_thinking=true` for any other non-empty effort value. An explicit `enable_thinking` entry in `chat_template_kwargs` MUST take precedence over that injection. AS MUST NOT translate effort into other template keys such as `thinking`. Templates that do not consume `enable_thinking` MUST receive that injected key unchanged; gpt-task owns whether the selected template accepts, filters, or ignores it. When both public fields are absent or empty, AS MUST omit `template_args`.

## Raw Task Transport

AS MUST persist the complete canonical request in `llm_jobs.task_args_json`. Its worker MUST submit that exact JSON through Bridge raw-task creation. Bridge and Relay schema validation MUST use the matching gpt-task schema version. Worker MUST deserialize the canonical object and invoke gpt-task without API-specific conversion.

The required task version MUST be `3.6.0`. Relay MUST dispatch the task only to a node whose runner version has major version `3` and minor and patch version not lower than `3.6.0`.

## Raw Output Conversion

gpt-task MUST return generated assistant text without creating public API tool-call objects. AS MUST select output parsers from generated syntax, not from model ID.

The parser registry MUST cover `llama`, `kimi`, `deepseek_r1`, `deepseek_v3_1`, `deepseek_v3_2`, `deepseek_v4`, `qwen_3`, `qwen_3_coder`, `qwen_3_5`, `glm_4_7`, `hermes`, `hy_v4`, and `kimi_k3`.

When canonical `tool_choice` is `none`, AS MUST retain the complete assistant text and MUST NOT parse a tool call. When a parser succeeds, AS MUST remove only its matched syntax, preserve other content in source order, validate that every function name was declared, and emit ordered public function calls. A named choice MAY return a bare JSON arguments object; AS MUST attach the selected canonical function name.

Canonical `response_format` output MUST remain assistant content or Responses `output_text`. AS MUST NOT parse it as a function call or modify its JSON representation.

Chat Completions, Responses, simulated SSE, and previous-response history reconstruction MUST use the same canonical-aware parser behavior.
