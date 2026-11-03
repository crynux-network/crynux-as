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

Provider execution is available through:

* `GET /models`
* `GET /models/chat`
* `GET /models/image`
* `GET /models/<model>`
* `POST /chat/completions` and `POST /responses` for models with `api_kind = chat`
* `GET /responses/<response_id>` for Responses jobs
* `POST /images/generations` and `POST /images/edits` for models with `api_kind = image`

Provider execution MUST NOT be available through:

* `POST /completions`;
* Raw Task endpoints;
* Provider-specific native endpoints.

A Provider model name on `POST /completions` MUST return HTTP 400 with the existing OpenAI invalid-request shape. Crynux Completions behavior MUST remain unchanged.

A chat Provider model on Images endpoints MUST return HTTP 400. An image Provider model on Chat Completions or Responses MUST return HTTP 400. Anthropic MUST NOT configure `api_kind = image`.

## Model Names

Provider model names MUST be official API model names from configuration. AS lowercases and trims configured names on load and lowercases and trims request model values before Provider lookup, so letter case in the config file or request body does not change routing.

For example:

```json
{"model":"gpt-5.6-sol"}
```

AS MUST:

* trim surrounding whitespace and lowercase the public `model` value for Provider lookup;
* use the canonical lowercase configured model name in public responses;
* send that canonical lowercase model name to the selected Service Provider;
* avoid adding `openai/`, `anthropic/`, `google/`, or any other prefix;
* avoid aliases and client-visible internal IDs.

Provider lookup uses the canonical lowercase name. Crynux repository names continue to use existing Hugging Face normalization rules.

## Model Catalog Merge

`GET /models` MUST merge all currently visible chat and image catalog rows.

`GET /models/chat` MUST merge:

* current Crynux LLM catalog rows from the Relay loaded-model snapshot;
* configured Provider model rows with `api_kind = chat` whose Service Provider currently has at least one enabled, non-deleted Shared API Key.

`GET /models/image` MUST merge:

* current Crynux Image catalog rows from the Relay loaded-model snapshot;
* configured Provider model rows with `api_kind = image` whose Service Provider currently has at least one enabled, non-deleted Shared API Key.

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

Provider rows MUST omit Crynux-only fields such as `min_vram`, `on_disk_node_count`, `execution_dtype`, `quantization_config`, `model_cache_hash`, `components`, and `lora`.

Catalog order MUST be model ID ascending, then execution-source order with Provider rows after Crynux rows for deterministic output. Configuration validation and loaded-model refresh MUST prevent a cross-source name collision.

Temporary key saturation and cooldown MUST NOT remove a Provider model. The model disappears from the public catalog only after a successful models-catalog refresh finds no enabled, non-deleted key for its Service Provider. Catalog list and retrieve handlers MUST read that refreshed snapshot and MUST NOT query Shared API Key visibility on each request.

`GET /models/<model>` MUST return the existing list envelope for every matching catalog row. A visible Provider chat or image model returns one row when its Service Provider has at least one enabled, non-deleted Shared API Key. Missing models return the existing OpenAI `model_not_found` error.

Each configured Provider model MUST declare exactly one `api_kind`:

* `chat`: listed on `GET /models` and `GET /models/chat`, and callable through Chat Completions and Responses only;
* `image`: listed on `GET /models` and `GET /models/image`, and callable through Images generations and Images edits only.

Provider adapters MUST translate each supported public request into the Provider's native API contract for that `api_kind`.

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

Provider-specific request fields that are not part of the AS public contract for the selected execution source MUST be rejected as unknown fields. AS MUST NOT forward arbitrary extra JSON.

## Images Generations and Edits

Crynux Images request validation in [OpenAI-Compatible LLM API](../llm-api.md) MUST remain unchanged for Crynux Stable Diffusion models.

When the request `model` resolves to a Provider image model, AS MUST parse an OpenAI-shaped Images body into a shared canonical Images request, then run the selected Service Provider Images converter. The allowed field set MUST depend on that Service Provider. AS MUST NOT apply Crynux Hugging Face model-id rules, Image TaskArgs construction, or Bridge fee estimation on that path.

### Shared Provider Images constraints

* `response_format` MUST be `b64_json` or omitted. `url` MUST return HTTP 400.
* `stream` and `partial_images` MUST return HTTP 400.
* OpenAI Files `file_id` input references MUST return HTTP 400. Clients MUST send data URLs or multipart file bytes.
* Crynux execution fields (`vram_limit`, path VRAM, `dtype`, `quantization_config`, `components`) MUST return HTTP 400.
* `queue_timeout` MUST remain an AS-local field and MUST NOT be forwarded upstream.

### OpenAI Images converter

The OpenAI Images converter MUST validate and pass through official Images generations and edits fields that the selected GPT Image model supports, including `prompt`, `model`, `n`, `size`, `quality`, `background`, `moderation`, `output_format`, `output_compression`, edit input images, `mask`, `input_fidelity`, and `markup` when present and valid. Combination rules MUST match OpenAI (for example `background=transparent` requires `output_format` `png` or `webp`). Unknown fields MUST return HTTP 400.

### Google Images converter

The Google Images converter MUST map:

* `prompt` and edit input images to `generateContent` `contents.parts`;
* `size` to `imageConfig.aspectRatio` and `imageConfig.imageSize`;
* `n` MUST be `1`. Any other value MUST return HTTP 400.

It MUST set `responseModalities` to `IMAGE` only. It MUST NOT send `outputMimeType`, `outputCompressionQuality`, or `imageOutputOptions` to the Gemini Developer API.

Google field handling:

* omitted `output_format`: return upstream bytes; detect format by magic; echo the detected format; unknown magic MUST fail the job;
* `output_format` `png`, `jpeg`, or `webp`: re-encode locally to that format after the upstream response and echo it;
* `output_compression` with `jpeg` or `webp`: apply locally during re-encode;
* `output_compression` with omitted or `png` format: HTTP 400;
* `quality`, any `background`, `moderation`, `mask`, `input_fidelity`, `markup`, and unknown fields: HTTP 400.

Public responses MUST remain `b64_json` without a public `usage` object. Settlement MUST use adapter-normalized token usage.

## Image Input

Provider Chat Completions and Responses MUST accept OpenAI-compatible base64 image inputs:

* Chat Completions uses `content[].type = "image_url"` with nested `image_url.url`;
* Responses uses `content[].type = "input_image"` with direct `image_url`;
* `image_url` MUST be a `data:<mime>;base64,<payload>` URL;
* absolute `http` and `https` URLs MUST be rejected with HTTP 400 before Credits reservation;
* allowed MIME types are `image/jpeg`, `image/png`, and `image/webp` only;
* the decoded base64 payloads of all images in one request MUST NOT exceed `provider.max_input_image_bytes`;
* images MUST appear only in user messages;
* OpenAI `detail` values `auto`, `low`, `high`, and `original` are accepted at parse time; whether a value is supported for the selected model MUST be decided by that model's image token rule before Credits reservation; OpenAI adapters MUST pass a present value through; Anthropic and Google adapters MUST drop it.

AS MUST expand `previous_response_id` history before Provider submission and MUST preserve ordered image parts with their original MIME type, base64 payload, and `detail` when present. Image validation and token estimation MUST run on the expanded request body.

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

Local expansion is required because Shared API Key selection MAY assign a different key to a later request, so a previous response stored under one upstream key is not available to the next request, and because Anthropic and Google have no equivalent previous-response store. AS MUST NOT rely on OpenAI Responses stateful `previous_response_id` storage.

The OpenAI adapter MUST translate public Responses requests to OpenAI Chat Completions. Anthropic and Google adapters MUST continue translating public Responses to their native chat APIs. All three adapters MUST return AS-assembled public Responses objects whose `id` is the job `PublicID`.

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
