# OpenAI-Compatible LLM API and Charging

This document specifies the private OpenAI-compatible LLM endpoints, Bridge forwarding, project token-ratio billing, and Credits charging rules.

## Endpoints

Each project exposes a private base URL `/api/<endpoint_token>/v1`.

Implemented endpoints:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/completions`

The request and response formats MUST be OpenAI-compatible.

`GET /api/<endpoint_token>/v1/models` is not implemented and MUST return HTTP 501.

## Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. The project API key in the `Authorization: Bearer <api_key>` header, which MUST match the located project's stored key hash.

The project MUST have status active. A request failing any of these checks MUST be rejected with HTTP 401.

## Forwarding to the Crynux Bridge

The service MUST forward LLM requests to the Crynux Bridge using the platform-level Bridge API key from the service configuration (`Authorization: Bearer <api_key>`). The Bridge API key MUST have the `chat` role.

| Client endpoint | Bridge endpoint |
|-----------------|-----------------|
| `POST .../v1/chat/completions` | `POST {bridge.base_url}/v1/llm/chat/completions` |
| `POST .../v1/completions` | `POST {bridge.base_url}/v1/llm/completions` |

For streaming requests (`stream: true`), the Bridge returns server-sent events after the task completes, and includes the `usage` payload in the final chunk only when the request contains `stream_options.include_usage: true`.

When forwarding a streaming request, the service MUST:

1. Inject `stream_options.include_usage: true` into the request sent to the Bridge.
2. Return the streamed response to the client in the shape the client requested.
3. Omit the injected usage-only final chunk when the client did not request `include_usage`.

## Project Token Ratio

Each project stores a `token_ratio` that scales billed tokens relative to Bridge-reported consumed tokens.

### Allowed values

The API exposes `token_ratio` as a floating-point number. The allowed set is exactly 19 values:

* `0.1` through `1.0` in steps of `0.1`
* `2` through `10` in steps of `1`

The default value is `1.0`.

Values below `1` MUST use exactly one decimal place. Values at or above `1` MUST be whole numbers from the allowed set.

### Storage

The database column `projects.token_ratio` MUST store the ratio as an unsigned integer equal to the display value multiplied by `10`:

| Display | Stored |
|---------|--------|
| `0.1` | `1` |
| `1.0` | `10` |
| `2` | `20` |
| `10` | `100` |

Project create and update APIs MUST accept the display float, validate it against the allowed set, and persist the stored integer. Project read APIs MUST return the display float.

## Unit Prices

LLM charging uses global unit prices from the service configuration:

```yaml
llm:
  prompt_credits_per_token: 1
  completion_credits_per_token: 1
  default_max_tokens: 2048
```

Configuration loading MUST fail when any of these values is zero.

* `prompt_credits_per_token` is the Credits charged per billed prompt token before integer division by the ratio scale.
* `completion_credits_per_token` is the Credits charged per billed completion token before integer division by the ratio scale.
* `default_max_tokens` is used in the pre-forward balance estimate when the request omits both `max_tokens` and `max_completion_tokens`.

## Charge Formula

Let:

* `P` = Bridge `usage.prompt_tokens`
* `C` = Bridge `usage.completion_tokens`
* `R` = stored `token_ratio` (display × 10)
* `Pp` = `llm.prompt_credits_per_token`
* `Cp` = `llm.completion_credits_per_token`

The charged Credits MUST be computed with integer arithmetic:

```text
credits = (P * R * Pp + C * R * Cp) / 10
```

## Balance Precheck

Before forwarding a request to the Bridge, the service MUST estimate an upper-bound charge and reject the request with HTTP 402 when the account balance is strictly less than the estimate.

Estimate inputs:

1. Prompt token estimate: UTF-8 rune count of the request prompt or chat message text content, divided by 4 and rounded up. A non-empty text whose estimate would be zero MUST use `1`.
2. Completion token estimate: `max_completion_tokens` if present and positive; otherwise `max_tokens` if present and positive; otherwise `llm.default_max_tokens`.

The estimate MUST use the same charge formula as the actual settle, substituting the estimated prompt and completion token counts.

## Settle After Bridge Response

After a successful Bridge response:

1. The service MUST read `usage.prompt_tokens`, `usage.completion_tokens`, and `usage.total_tokens`.
2. The service MUST compute Credits with the charge formula.
3. The service MUST create one `llm_call_records` row with success status, token counts, charged Credits, and call duration.
4. When the computed Credits are greater than zero and the account balance is sufficient, the service MUST create one `credit_events` row of type LLM charge referencing the call record ID and MUST decrease the account balance by the same amount in the same database transaction.

When the Bridge call succeeds but the account balance is insufficient for the computed Credits at settle time, the service MUST still create a success `llm_call_records` row with charged Credits set to `0`, MUST NOT create a Credits ledger event, and MUST emit an error log for operators.

## Failed Calls

Every failed LLM call MUST be recorded as one `llm_call_records` row with failure status. A failed call MUST NOT create a Credits ledger event.

Failure includes Bridge transport errors, Bridge HTTP status greater than or equal to 400, response parse failures, and streaming responses that end without a usable `usage` payload.

## Call Record Fields

Each `llm_call_records` row MUST contain:

* `project_id`
* `model`
* `prompt_tokens`, `completion_tokens`, `total_tokens`
* `status` (success or failed)
* `credits` (charged Credits; `0` when not charged)
* `duration_ms`
