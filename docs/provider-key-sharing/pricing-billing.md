# Provider Pricing and Billing

## Relationship to Existing Billing

[Credits Billing](../credits-billing.md) remains authoritative for:

* project Cost Level fields;
* static and auto Cost Level behavior;
* `credits_per_gwei`;
* Credits reservation;
* Credits account locking;
* the per-provider `max_task_price_cnx` cap;
* the shared `min_task_price_cnx.providers` admission floor;
* task charge ledger ownership.

This document defines Provider workload units, Provider queue priority, actual-usage settlement, and Key Owner CNX earnings.

## Economic Model

Provider calls MUST use the same economic conversion as Crynux tasks:

```text
consumer_credits = billable_gwei * credits_per_gwei
key_owner_earning_wei = billable_gwei * 1e9
```

AS MUST NOT apply a Provider-specific Credits-to-CNX rate or an additional Provider revenue-share percentage.

Platform economics remain determined by:

* `blockchains.<network>.tokens.<token>.credits_per_token`, which determines Credits purchased per payment token;
* `llm.credits_per_gwei`, which determines Credits charged per Gwei of task value.

`llm.max_task_price_cnx.<service_provider>` MUST cap that Service Provider's consumer charge and Key Owner earning through the same post-cap `billable_gwei`. The job billing snapshot MUST store the configured CNX string used for that cap.

`llm.min_task_price_cnx.providers` MUST define the shared Provider admission floor in CNX units. For the current request's estimated input tokens and maximum generated tokens, admission MUST:

1. estimate Provider units for every configured Provider model under those same request parameters;
2. select the cheapest positive unit estimate;
3. convert the shared CNX floor into one Cost Level lower bound as `ceil(min_task_fee_gwei / cheapest_units)`;
4. apply that shared Cost Level lower bound to the current request for every selected Provider model.

After the estimated post-cap `billable_gwei` is computed for the selected model, static Cost Level mode MUST reject with HTTP 400 when estimated Credits at the fixed priority are strictly greater than `auto_max_credits_per_task`, or when the fixed priority is strictly below the shared Cost Level lower bound. Auto Cost Level mode MUST clamp the queue-target priority into `[shared_min_priority_gwei, max_priority_within_auto_max_credits]` for the selected model; when that interval is empty it MUST reject with HTTP 400.

A configured providers minimum of `0` disables the shared Cost Level lower bound. Settlement MUST NOT raise Credits or Key Owner earnings to the floor after admission. The job billing snapshot MUST store the selected model weights, the effective priority, Credits rate, max-price CNX string, and estimated request units. It MUST NOT store a per-Service-Provider minimum CNX string.

## Configured Usage Weights

Each Provider model configuration MUST contain:

* one non-negative decimal-string weight for every usage component the adapter can return;
* explicit zero weights for components that are reported but not billed.

Common usage components are:

* input tokens;
* output tokens;
* cached input tokens;
* cache-write input tokens;
* reasoning or thinking tokens;
* tool-use prompt tokens;
* image input tokens;
* image output tokens.

OpenAI and Google `api_kind = chat` model configuration MUST include a `reasoning_tokens` weight because those adapters can return positive reasoning or thoughts counts. Anthropic configuration MUST NOT require a separate `reasoning_tokens` weight; Anthropic thinking tokens are already counted in output tokens.

OpenAI and Google `api_kind = image` model configuration MUST include `image_input_tokens` and `image_output_tokens` weights. Google `api_kind = image` model configuration MUST also include a `reasoning_tokens` weight because Gemini image models can return positive `thoughtsTokenCount`. OpenAI image models MUST NOT require `reasoning_tokens`. Anthropic MUST NOT configure `api_kind = image`.

Weights are normalized Provider work units per usage item. They MUST be parsed as exact rational numbers. Floating-point arithmetic MUST NOT be used for ledger amounts.

An adapter MUST reject a terminal usage object containing a positive component for which the pricing snapshot has no weight.

Effort values do not change usage weights. Different `reasoning_effort` or `reasoning.effort` values affect only the actual token counts returned by the Service Provider. Consumer Credits and Key Owner earnings MUST continue to use the job pricing snapshot weights multiplied by those actual counts.

## Provider Work

For a model with usage components `u_i` and configured weights `w_i`:

```text
provider_units = sum(u_i * w_i)
uncapped_billable_gwei = effective_priority_gwei * provider_units
```

The effective priority is the request's resolved Cost Level. Provider queue order MUST use effective priority directly, not total estimated fee.

This normalization gives larger requests a proportionally larger total fee while preserving Cost Level as the price per normalized work unit.

## Static and Auto Cost Level

Static mode MUST use the project's stored `priority_gwei`.

Auto mode MUST use the Provider queue's current lowest and highest effective priorities with the same integer interpolation and project maximum rules defined for the existing queue:

```text
effective =
  lowest
  + floor((highest - lowest) * auto_queue_position / 100)
```

The result MUST stay within `auto_max_credits_per_task` and MUST be clamped to the configured global Cost Level bounds.

When the Provider queue is empty or its bounds are unusable, auto mode MUST use `llm.min_priority_gwei`.

The resolved effective priority MUST be persisted on the job and MUST NOT change after creation.

Provider queue statistics and Relay queue statistics are independent. A Provider call MUST NOT use Relay queue bounds.

The Provider scheduler MUST refresh one immutable in-memory Provider queue-priority snapshot at the start of every scheduler iteration. It MUST query the lowest and highest effective priority from non-expired `pending_submit` Provider jobs with two indexed `ORDER BY sortable_priority ... LIMIT 1` queries. The snapshot MUST contain the observation time, whether a matching job exists, lowest priority, and highest priority and MUST be replaced atomically.

Provider admission in auto mode MUST read one complete published snapshot. It MUST use that snapshot only when a matching job exists, both bounds are present, and the highest bound is not below the lowest bound. It MUST otherwise use `llm.min_priority_gwei`. Jobs admitted concurrently after the observation MUST affect only a later snapshot. Queue-timeout rows and jobs already claimed by a key MUST not enter the snapshot.

## Pre-Execution Estimate

Admission MUST reserve an upper estimate before queueing.

For text inputs, the estimate MUST count the UTF-8 byte length of all text, tool definitions, structured-output schemas, instructions, and locally expanded history as the text input-token upper bound. The `image_url` value of Chat Completions `image_url` parts and Responses `input_image` parts MUST be excluded from that text walk. This one-byte-per-token text estimate intentionally over-reserves relative to ordinary tokenization.

For image inputs, admission MUST parse the same canonical request used by Provider adapters, validate JPEG/PNG/WebP base64 payloads against `provider.max_input_image_bytes`, read only image headers for width and height, and estimate visual input tokens with the target Service Provider's official model-family rules. Those rules live in `provider/` code, not YAML. OpenAI image rules MUST select the model family by exact configured model name, including date-suffixed snapshots after stripping `-YYYY-MM-DD`, and MUST apply that family's detail-level resizing, patch budget, and multiplier or tile formula. A request that includes images for a model whose image token rule is unknown, or that uses a `detail` value unsupported by that model, MUST return HTTP 400 before Credits reservation. A text-only request for that model MUST still be accepted.

The admission input-token estimate MUST be:

```text
estimated_input_tokens = estimated_text_input_tokens + estimated_image_input_tokens
```

Both parts use the model's configured `input_tokens` weight family. Settlement MUST continue to use the Service Provider's returned actual input tokens and MUST NOT charge image tokens a second time.

For generated usage:

* use the request's positive maximum output token field;
* otherwise use `llm.default_max_tokens`;
* apply the greatest configured weight among output, reasoning, and other generated-token components.

The same resolved maximum generated-token value MUST be written into the upstream Provider request body for OpenAI, Anthropic, and Google. When the public request omits a positive maximum output token field, adapters MUST inject `llm.default_max_tokens` rather than leaving the upstream limit unset or using a separate hardcoded default.

For input categories not known before execution:

* apply the greatest configured weight among ordinary input, cached input, cache write, tool-use prompt, and other input-token components.

The estimated Provider units MUST be:

```text
estimated_input_tokens * maximum_input_weight
+ maximum_generated_tokens * maximum_generated_weight
```

For `api_kind = chat`, maximum input weight MUST be the greatest configured weight among ordinary input, cached input, cache write, tool-use prompt, and image input. Maximum generated weight MUST be the greatest among output and reasoning.

For `api_kind = image`, estimated input tokens MUST be prompt text bytes plus estimated image input tokens for edit source images. Maximum generated tokens MUST be the estimated image output tokens for the requested size, quality, and `n` using Service Provider image-output rules in `provider/`. Maximum input weight MUST include `image_input_tokens`. Maximum generated weight MUST include `image_output_tokens`.

When computing the shared `min_task_price_cnx.providers` Cost Level lower bound, AS MUST estimate units only across configured models whose `api_kind` matches the current request (`chat` or `image`).

The job billing snapshot MUST store `estimated_input_tokens`, `estimated_text_input_tokens`, and `estimated_image_input_tokens`.

The estimated billable amount MUST then use the same Cost Level and cap rules as final settlement. Admission MUST apply the shared `min_task_price_cnx.providers` Cost Level lower bound derived from the cheapest configured Provider model under the current request's estimated input and maximum generated tokens before Credits reservation.

The reservation MUST be:

```text
estimated_credits =
  max(1, floor(post_cap_estimated_billable_gwei * credits_per_gwei))
```

## Actual-Usage Settlement

The adapter MUST produce one normalized final usage object. Component semantics MUST state whether a field is a total or a subset so categories are not double-counted.

OpenAI `reasoning_tokens` are a subset of `completion_tokens` and MUST be subtracted from ordinary output before both weights are applied. Google `thoughtsTokenCount` is billed in addition to `candidatesTokenCount` and MUST NOT be subtracted from ordinary output. Anthropic thinking tokens remain inside `output_tokens` and MUST NOT appear as a separate billed reasoning component.

Settlement MUST:

1. decode the job's immutable pricing snapshot;
2. calculate actual Provider units with exact rational arithmetic;
3. multiply by the job's effective priority;
4. apply `llm.max_task_price_cnx.<service_provider>`;
5. calculate consumer Credits;
6. calculate Key Owner earning wei;
7. enter the common settlement transaction.

Consumer Credits MUST be:

```text
computed_credits =
  max(1, floor(post_cap_billable_gwei * credits_per_gwei))
```

Key Owner earnings MUST be:

```text
earning_wei =
  floor(post_cap_billable_gwei * 1e9)
```

When `earning_wei` is zero, the call MUST succeed and charge the minimum one Credit, but MUST NOT create a zero-amount earning event.

## Rounding

All configuration decimals MUST be parsed into rational numerator and denominator values.

The implementation MUST:

* multiply integers and rationals without floating-point conversion;
* apply the CNX max cap before Credits or earning conversion;
* scale the Provider CNX min floor to integer Gwei by rounding down once after multiplying by the maximum-generated-token ratio;
* apply the scaled CNX min floor only at admission against the final estimated fee, never by raising settle amounts;
* round consumer Credits down once at the final Credits conversion;
* round earning wei down once at the final wei conversion;
* persist decimal integer strings for Credits, Gwei, and wei amounts.

Intermediate truncation MUST NOT occur between usage components.

## Charge Shortfall

Actual usage is expected to fit the reserved estimate. After the current job's Credits hold is released in the settle transaction, the consumer debit MUST use only available Credits:

```text
available = max(balance - remaining_locked, 0)
consumer_charge = min(computed_credits, available)
```

`remaining_locked` MUST be the Credits still held by other jobs of the same user. The settle path MUST NOT reduce `balance` below that remaining locked amount.

The Key Owner earning MUST still use the full computed billable amount. When `consumer_charge` is less than `computed_credits`, the platform MUST absorb the shortfall, and the settle path MUST emit an error log with job ID, computed Credits, charged Credits, balance, remaining locked, and shortfall.

## Settlement Usage

Intermediate in-worker Key switches after definitely unaccepted key-specific failures MUST NOT write usage records. The active Provider goroutine MUST pass the final normalized usage and final Shared API Key ID directly to common settlement.

Any final confirmed usage is billable to the API Consumer and creates the Key Owner earning. Confirmed usage MUST be accompanied by a valid public result and MUST settle as a completed job with a success call record. Uncertain or unverifiable usage MUST NOT create a charge or earning.

Credits charge, token usage recording, and Key Owner earning MUST be determined only by whether the Service Provider completed the request. When the Service Provider completes the request, settlement MUST complete. API Consumer disconnect, a detached or absent subscriber, and any other public-delivery outcome MUST NOT cancel, defer, or change that settlement.

A billable Provider call record MUST create at most one Key Owner earning source.

## Failure Cases

### Definitely unaccepted

Release the Credits hold, record no usage, charge no Credits, and create no earning.

### Missing final usage

Release the Credits hold and create a failed call record. Create no automatic earning. Observed usage fragments and upstream request identity MUST NOT be persisted.

## Earning Availability

A created earning MUST immediately increase `earning_accounts.available`. Its TaskIncome event MUST be created as `processed` in the same settlement transaction so Relay Wallet can publish it without another worker or delay.

## Pricing Snapshot

Every Provider job MUST persist a versioned billing snapshot containing:

* Service Provider;
* exact model name;
* every usage weight;
* effective priority;
* `credits_per_gwei`;
* maximum task price;
* estimated usage;
* estimated Provider units;
* locked Credits.

The snapshot MUST NOT store a per-Service-Provider minimum task price. Older snapshots that still contain that field MUST ignore it during settlement.

Settlement MUST use only this snapshot and final normalized usage. It MUST NOT read current model pricing configuration.
