# Model Compatibility Documentation

## Authority

This directory is authoritative for Crynux AS OpenAI-compatible request conversion, canonical task persistence, raw-task submission, raw assistant-text parsing, and public response construction.

Prompt rendering, tokenizer and processor behavior, constrained token generation, execution backends, and raw decoding MUST use `gpt-task/docs/model-compatibility/` in the standalone gpt-task repository.

Bridge, Relay, Node, and Worker documentation MUST define only their validation, persistence, scheduling, transport, worker-management, and execution boundaries for the AS raw-task path. They MUST NOT redefine the AS public API conversion.

## Update Requirements

Documents MUST state final, testable behavior. They MUST preserve canonical task arguments and raw generated text across component boundaries. Model-specific generated syntax MUST be parsed by syntax and MUST NOT become a model-ID allowlist.
