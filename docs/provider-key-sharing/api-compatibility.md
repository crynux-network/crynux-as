# Public API Compatibility

## Public Boundary

Crynux AS remains the owner of the public OpenAI-compatible contract. Service Provider adapters are internal transports and MUST NOT expose Provider-specific authentication, URLs, IDs, raw events, or error bodies.

Clients continue to use:

```text
/api/<endpoint_token>/v1
```

Authentication remains:

* project endpoint token in the URL;
* project API key or owner JWT in `Authorization: Bearer`;
* active project ownership and isolation.

The official OpenAI SDK MUST be usable by setting its base URL to the project endpoint and using the configured official Provider model name.

## Supported Provider Endpoints

The first release supports:

* `GET /models`
* `GET /models/<model>`
* `POST /chat/completions`
* `POST /responses`
* `GET /responses/<response_id>`

Provider execution MUST NOT be available through:

* `POST /completions`;
* Raw Task endpoints;
* Images generations;
* Images edits;
* Provider-specific native endpoints.

A Provider model name on `POST /completions` MUST return HTTP 400 with the existing OpenAI invalid-request shape. Crynux Completions behavior MUST remain unchanged.

## Model Names

Provider model names MUST be exact official API model names from YAML configuration.

For example:

```json
{"model":"gpt-5.6-sol"}
```

AS MUST:

* trim surrounding whitespace for lookup;
* preserve the configured model name in public responses;
* send the configured model name to the selected Service Provider;
* avoid adding `openai/`, `anthropic/`, `google/`, or any other prefix;
* avoid aliases and client-visible internal IDs.

Provider lookup is exact and case-sensitive. Crynux repository names continue to use existing Hugging Face normalization rules.

## Model Catalog Merge

`GET /models` MUST merge:

* current Crynux catalog rows from the Relay loaded-model snapshot;
* configured Provider model rows whose Service Provider currently has at least one enabled, non-deleted Shared API Key.

Provider catalog items MUST contain:

```json
{
  "id": "gpt-5.6-sol",
  "object": "model",
  "created": 0,
  "owned_by": "openai"
}
```

`owned_by` MUST be `openai`, `anthropic`, or `google`.

Provider rows MUST omit Crynux-only fields such as `min_vram`, `on_disk_node_count`, `execution_dtype`, and `quantization_config`.

Catalog order MUST be model ID ascending, then execution-source order with Provider rows after Crynux rows for deterministic output. Configuration validation and loaded-model refresh MUST prevent a cross-source name collision.

Temporary key saturation and cooldown MUST NOT remove a Provider model. The model disappears only when no enabled, non-deleted key remains for its Service Provider.

`GET /models/<model>` MUST return the existing list envelope. A Provider model returns one row. Missing models return the existing OpenAI `model_not_found` error.

Every configured Provider model MUST be callable through Chat Completions and Responses. Provider adapters MUST translate each supported public request into the Provider's native API contract. Model configuration MUST NOT declare an API capability list.

## Request Fields

Provider calls MUST accept the public fields already specified for the corresponding endpoint in [OpenAI-Compatible LLM API](../llm-api.md), subject to adapter support described in [Provider Adapters](provider-adapters.md).

The following Crynux execution extensions MUST be rejected for Provider models:

* `vram_limit`;
* path-based VRAM selection;
* `dtype`;
* `quantization_config`;
* Crynux model components;
* Crynux hardware requirements.

`queue_timeout` MUST remain accepted. It controls only how long the job may wait for a Shared API Key slot. When omitted, AS MUST use `provider.default_queue_timeout_seconds`. The resolved timeout seconds MUST be stored in the existing `task_jobs.queue_timeout` column. AS MUST calculate expiration from `task_jobs.created_at + task_jobs.queue_timeout` and MUST NOT store a separate Provider queue deadline.

Provider-specific request fields that are not part of the AS public contract MUST be rejected as unknown fields. AS MUST NOT forward arbitrary extra JSON.

## Tools and Structured Output

AS MUST apply its existing OpenAI-compatible validation rules before calling a Provider adapter.

Every Provider adapter MUST translate all validated AS tool and structured-output semantics into its Service Provider's native request format:

* function definitions;
* `none`, `auto`, `required`, and named tool choice;
* tool-call history;
* tool-result history;
* text, JSON object, and JSON schema response formats.

Every Provider adapter MUST translate native tool calls, tool results, and structured output back to the selected OpenAI-compatible public response format. Provider-native tool or content types that AS does not declare MUST not appear in public responses.

## Chat Completions

Non-streaming Provider Chat Completions MUST return the existing OpenAI-compatible Chat Completions object.

Streaming Provider Chat Completions MUST be real-time:

* upstream events are read while generation runs;
* chunks are normalized to Chat Completions chunk objects;
* every chunk uses the AS public job ID rather than an upstream internal ID;
* model is the configured public model name;
* tool-call deltas preserve stable public indexes and IDs;
* the final usage-only chunk is emitted only when `stream_options.include_usage = true`;
* `data: [DONE]` terminates a successful stream.

AS MUST NOT emit `[DONE]` after an upstream failure.

Crynux model streaming remains completion-first simulated SSE.

## Responses

Provider Responses MUST support:

* non-background, non-streaming execution;
* `background = true` retained execution and polling;
* real-time streaming when `stream = true` and `background` is false or omitted;
* local `previous_response_id` expansion;
* function-call and function-call-output history;
* public Responses status and error objects.

A request with both `stream = true` and `background = true` MUST return HTTP 400.

`previous_response_id` MUST remain local to the same project. AS MUST expand retained history before Provider submission and MUST NOT expose the ID to the Service Provider.

Crynux model Responses behavior remains unchanged, including its current rejection of `stream = true`.

## Result Persistence

Provider adapters MUST assemble one final normalized result whenever final usage is confirmed. Safety stops, max-token truncation, empty assistant content, and tool-only output remain valid public results.

The temporary job row MUST store:

* normalized raw semantic result required for history;
* formatted public result required for non-streaming response and polling.

Permanent call records MUST NOT store prompts, completions, raw Provider responses, or result bodies.

## Usage

Public usage MUST use the endpoint's OpenAI-compatible shape.

Adapter-specific usage components required for settlement MUST remain in the active Provider goroutine until settlement commits. They MUST NOT be persisted as a separate attempt record or added as undocumented top-level public fields. Permanent call records MUST retain only the normalized public prompt/input, completion/output, and total token values.

The public prompt/input, completion/output, and total token values MUST be derived without double-counting cached or reasoning subcategories.

## Error Mapping

Before any public stream event:

* invalid input: HTTP 400;
* failed project authentication: HTTP 401;
* insufficient Credits: HTTP 402;
* model absent: HTTP 404;
* no usable key before queue timeout: OpenAI timeout error with HTTP 504;
* temporary internal or Provider failure: HTTP 500.

After public stream output begins, HTTP status cannot change. For Chat Completions, AS MUST emit one SSE `data` item whose JSON body contains the existing OpenAI error object, then close without `[DONE]`. For Responses, AS MUST emit one `error` SSE event containing the existing Responses error object, then close. AS MUST not emit further content or terminal-success events after either error.

Raw Service Provider error bodies, URLs, account identifiers, and request headers MUST NOT be returned.

Authentication failure of a Shared API Key is an internal capacity failure, not an API Consumer authentication failure.

## IDs

Public task and response IDs MUST be generated by AS.

Upstream request IDs MUST remain internal and may appear only in redacted operator logs and attempt records.

Cross-project lookup MUST continue to return 404 without revealing resource existence.
