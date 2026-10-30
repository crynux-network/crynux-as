# Provider Pricing and Billing

## Relationship to Existing Billing

[Credits Billing](../credits-billing.md) remains authoritative for:

* project Cost Level fields;
* static and auto Cost Level behavior;
* `credits_per_gwei`;
* Credits reservation;
* Credits account locking;
* the per-provider `max_task_price_cnx` cap;
* the per-provider `min_task_price_cnx` admission floor;
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

`llm.min_task_price_cnx.<service_provider>` MUST define the Provider admission floor for a request whose maximum generated tokens equal `llm.default_max_tokens`. Admission MUST scale this configured base floor by the request's resolved maximum generated tokens:

```text
base_min_task_fee_gwei =
  floor(min_task_price_cnx.<service_provider> * 1e9)

admission_min_task_fee_gwei =
  floor(
    base_min_task_fee_gwei
    * maximum_generated_tokens
    / llm.default_max_tokens
  )
```

After the estimated post-cap `billable_gwei` is computed, when `admission_min_task_fee_gwei` is positive and the estimated fee is strictly below that scaled floor:

* static Cost Level mode MUST reject with HTTP 400 and MUST NOT create a job or lock Credits;
* auto Cost Level mode MUST raise the effective priority (and therefore the estimated fee) to the lowest value that meets the scaled floor, capped by the project's `auto_max_priority_gwei`, when that auto maximum can produce a fee at or above the scaled floor; otherwise it MUST reject with HTTP 400.

A configured minimum of `0` disables the gate for that Service Provider. Settlement MUST NOT raise Credits or Key Owner earnings to the scaled floor after admission. The job billing snapshot MUST store the configured base CNX string, the resolved maximum generated tokens, and the post-raise effective priority.

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
* tool-use prompt tokens.

Weights are normalized Provider work units per usage item. They MUST be parsed as exact rational numbers. Floating-point arithmetic MUST NOT be used for ledger amounts.

An adapter MUST reject a terminal usage object containing a positive component for which the pricing snapshot has no weight.

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

The result MUST be capped by `auto_max_priority_gwei` and clamped to the configured global bounds.

When the Provider queue is empty or its bounds are unusable, auto mode MUST use `llm.min_priority_gwei`.

The resolved effective priority MUST be persisted on the job and MUST NOT change after creation.

Provider queue statistics and Relay queue statistics are independent. A Provider call MUST NOT use Relay queue bounds.

The Provider scheduler MUST refresh one immutable in-memory Provider queue-priority snapshot at the start of every scheduler iteration. It MUST query the lowest and highest effective priority from non-expired `pending_submit` Provider jobs with two indexed `ORDER BY sortable_priority ... LIMIT 1` queries. The snapshot MUST contain the observation time, whether a matching job exists, lowest priority, and highest priority and MUST be replaced atomically.

Provider admission in auto mode MUST read one complete published snapshot. It MUST use that snapshot only when a matching job exists, both bounds are present, and the highest bound is not below the lowest bound. It MUST otherwise use `llm.min_priority_gwei`. Jobs admitted concurrently after the observation MUST affect only a later snapshot. Queue-timeout rows and jobs already claimed by a key MUST not enter the snapshot.

## Pre-Execution Estimate

Admission MUST reserve an upper estimate before queueing.

For text inputs, the estimate MUST count the UTF-8 byte length of all text, tool definitions, structured-output schemas, instructions, and locally expanded history as input-token estimate. This one-byte-per-token estimate intentionally over-reserves relative to ordinary tokenization.

For generated usage:

* use the request's positive maximum output token field;
* otherwise use `llm.default_max_tokens`;
* apply the greatest configured weight among output, reasoning, and other generated-token components.

For input categories not known before execution:

* apply the greatest configured weight among ordinary input, cached input, cache write, tool-use prompt, and other input-token components.

The estimated Provider units MUST be:

```text
estimated_input_bytes * maximum_input_weight
+ maximum_generated_tokens * maximum_generated_weight
```

The estimated billable amount MUST then use the same Cost Level and cap rules as final settlement. Admission MUST scale the Service Provider `min_task_price_cnx` floor by `maximum_generated_tokens / llm.default_max_tokens` and apply that scaled floor to the final estimated fee before Credits reservation.

The reservation MUST be:

```text
estimated_credits =
  max(1, floor(post_cap_estimated_billable_gwei * credits_per_gwei))
```

## Actual-Usage Settlement

The adapter MUST produce one normalized final usage object. Component semantics MUST state whether a field is a total or a subset so categories are not double-counted.

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

Actual usage is expected to fit the reserved estimate. If computed Credits exceed current balance after releasing the hold, the existing rule applies:

```text
consumer_charge = min(computed_credits, current_balance)
```

The Key Owner earning MUST still use the full computed billable amount. Any uncollected balance shortfall MUST produce an error log with job ID, computed Credits, and charged Credits.

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
* minimum task price;
* estimated usage;
* estimated Provider units;
* locked Credits.

Settlement MUST use only this snapshot and final normalized usage. It MUST NOT read current model pricing configuration.
