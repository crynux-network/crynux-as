# Credits Billing Model

This document is the complete specification for charging Credits on LLM calls in Crynux AS, and for the shared `billable_gwei` amount used to set the submitted task fee.

Task fee persistence, Bridge submission, and estimation failure HTTP behavior that are not Credits-specific remain in [llm-api.md](./llm-api.md) Task Fee Estimation. The fee formula itself MUST match this document.

## Model Summary

Crynux AS MUST NOT maintain a separate Credits price table for prompt tokens, completion tokens, or discrete VRAM billing tiers.

Credits and the submitted task fee MUST share one amount:

```text
billable_gwei =
    priority_gwei
    * estimated_node_seconds
    * vram_weight
```

* `task_fee_gwei = floor(billable_gwei)`
* `credits = max(1, floor(billable_gwei * credits_per_gwei))`

`priority_gwei` MUST be the project Cost Level stored on `projects.priority_gwei`.

The live queued-task `median_priority_gwei`, `highest_priority_gwei`, and `lowest_priority_gwei` MUST be used only for management UI comparison. They MUST NOT enter `billable_gwei`, Credits, or task fee.

For the same model, token counts, effective VRAM, and project `priority_gwei`, Credits and task fee MUST NOT change when the live queue median or bounds change.

## Configuration

```yaml
llm:
  default_max_tokens: 2048
  default_vram_limit: 24
  loaded_models_refresh_interval: 1800
  queued_priority_refresh_interval: 300
  execution_time_cache_ttl: 300
  base_vram: 8
  empty_queue_median_priority_gwei: "34"
  min_priority_gwei: "1"
  max_priority_gwei: "1000000000"
  credits_per_gwei: "1"
  job_submit_timeout: 600
  job_retention_days: 30
```

Configuration loading MUST fail when any of these values is missing or invalid:

* `base_vram` (MUST be greater than zero)
* `empty_queue_median_priority_gwei`
* `min_priority_gwei`
* `max_priority_gwei`
* `credits_per_gwei` (MUST parse as a positive decimal string)

Configuration loading MUST fail when `min_priority_gwei` is greater than `max_priority_gwei`.

The following items MUST NOT appear in configuration and MUST NOT be used for Credits or task fee:

* `reference_priority_gwei`
* `max_token_ratio`
* `prompt_credits_per_token`
* `completion_credits_per_token`
* `vram_ratios`

`min_priority_gwei` and `max_priority_gwei` are the hard inclusive bounds for project Cost Level values.

`credits_per_gwei` converts `billable_gwei` into Credits. It MUST NOT change `task_fee_gwei`.

`empty_queue_median_priority_gwei`, `min_priority_gwei`, `max_priority_gwei`, and every API or persisted field whose name ends in `_gwei` MUST be encoded as a decimal integer string (the same representation used for other Gwei amounts in Crynux AS). `credits_per_gwei` MUST be a positive decimal string. Configuration, `billing_config`, and the call-record snapshot MUST store the configured string value. Integer forms such as `"1"` and fractional forms such as `"0.000001"` are both valid.

`empty_queue_median_priority_gwei` is used only when presenting a queue-median hint and the queued-priority cache has never observed a non-empty queue, and when creating a project with no usable live or remembered median. It MUST NOT enter `billable_gwei` except when it becomes the project's stored `priority_gwei` at project creation through the Queue Median Hint rules in [llm-api.md](./llm-api.md).

`base_vram` MUST remain aligned with Relay `task_pricing.base_vram`. Every YAML configuration template MUST set it to `8`.

## Charge Inputs

| Input | Symbol | Source |
|-------|--------|--------|
| Prompt tokens | `P` | Bridge `usage.prompt_tokens` at settle; precheck estimate before forward |
| Completion tokens | `C` | Bridge `usage.completion_tokens` at settle; precheck estimate before forward |
| Project Cost Level | `Prio` | `projects.priority_gwei` at job create; snapshotted on the job and call record |
| Effective VRAM | `v` | Resolved effective VRAM in GB |
| Base VRAM | `B` | `llm.base_vram` |
| Constant seconds | `T0` | Relay LLM execution-time coefficient for `(model, min_vram=v)` |
| Seconds per input token | `Tin` | Same Relay response |
| Seconds per output token | `Tout` | Same Relay response |
| Credits per Gwei | `G` | `llm.credits_per_gwei` |

Version 1 covers text work only. Model-switch seconds and image work MUST NOT enter the formula.

## Derived Quantities

```text
estimated_node_seconds =
    T0 + Tin * P + Tout * C

vram_weight =
    max(v, B) / B

billable_gwei =
    Prio * estimated_node_seconds * vram_weight

task_fee_gwei =
    floor(billable_gwei)

credits =
    max(1, floor(billable_gwei * G))
```

`billable_gwei` and `task_fee_gwei` MUST be computed with floating-point intermediates and MUST truncate toward zero to non-negative integer values. After truncating `floor(billable_gwei * G)` toward zero to a non-negative integer, Credits for a chargeable LLM request MUST be raised to `1` when that truncated value is `0`.

`Prio` MUST be the project's stored `priority_gwei` snapshotted at job create. The service MUST NOT substitute the live queued-task median priority into `Prio`.

## Execution-Time Coefficients

The service MUST obtain `T0`, `Tin`, and `Tout` from Relay `GET /v2/models/llm/execution-time` with query `model=<request model>` and `min_vram=<effective_vram>`, using the existing execution-time cache.

## Request Path Before Bridge Forwarding

For each LLM request that requires Credits precheck and task fee, the service MUST run these steps in order:

1. Resolve effective VRAM and precheck token estimates (`P` estimate and `C` estimate).
2. Fetch and validate execution-time coefficients for `(model, effective_vram)`.
3. Compute `vram_weight`, `estimated_node_seconds` for the precheck token estimates, and `billable_gwei` using the project's snapshotted `priority_gwei`.
4. Reject with HTTP 402 when the account balance is strictly less than `max(1, floor(billable_gwei * G))`.
5. Set `task_fee_gwei = floor(billable_gwei)` from the same `billable_gwei`.
6. Resolve `median_priority_gwei` with the Queue Median Hint rules in [llm-api.md](./llm-api.md). This resolution MUST always produce a value and MUST NOT fail the request.
7. Persist on the LLM job: `constant_seconds`, `seconds_per_input_token`, `seconds_per_output_token`, `vram_weight`, `task_fee_gwei`, precheck `estimated_node_seconds`, and the resolved `median_priority_gwei` snapshot.

Credits precheck and task fee MUST use the same coefficient fetch and the same `billable_gwei`. The service MUST NOT run a Credits precheck that uses a different formula or a second coefficient fetch from the task fee path.

When step 2 fails validation or Relay fetch rules in [llm-api.md](./llm-api.md) Task Fee Estimation Failure, the service MUST NOT forward the request and MUST NOT charge Credits for that attempt.

After coefficients and configuration values are validated as usable non-negative finite inputs, computing `billable_gwei`, `task_fee_gwei`, and the Credits precheck amount MUST succeed. The service MUST NOT treat ordinary arithmetic of those validated inputs as a business failure mode.

## Balance Precheck Token Estimates

Precheck token inputs:

1. Prompt token estimate: UTF-8 rune count of the request prompt or chat message text content, divided by 4 and rounded up. A non-empty text whose estimate would be zero MUST use `1`.
2. Completion token estimate: `max_completion_tokens` if present and positive; otherwise `max_tokens` if present and positive; otherwise `llm.default_max_tokens`.

## Settle After Bridge Response

After a successful Bridge response:

1. The service MUST read `usage.prompt_tokens`, `usage.completion_tokens`, and `usage.total_tokens`.
2. The service MUST compute Credits with the charge formula using persisted job coefficients, persisted `vram_weight`, the job `priority_gwei` snapshot, and the usage token counts.
3. The service MUST create one `llm_call_records` row with success status, token counts, the job `priority_gwei` used for the charge, the billed effective VRAM, call duration, the pre-forward Task Fee Estimation fields, and the Credits recalculation snapshot (`constant_seconds`, `seconds_per_input_token`, `seconds_per_output_token`, `credits_per_gwei`). The call record MUST NOT store the charged Credits amount. The call record `estimated_node_seconds` MUST be the pre-forward value persisted on the job. It MUST NOT be replaced by the settle-time recomputation used only for Credits.
4. When the computed Credits are greater than zero and the account balance is sufficient, the service MUST create one `credit_events` row of type LLM charge referencing the call record ID and MUST decrease the account balance by the same amount in the same database transaction. The event amount is the sole permanent store of the charged Credits. Because Credits for a successful chargeable settle MUST be at least `1`, a sufficient balance MUST produce a ledger event.
5. The service MUST mark the LLM job `completed` only after steps 1 through 4 succeed.

Settle MUST NOT re-fetch Relay coefficients for the charge. Settle MUST NOT recompute or replace the already submitted `task_fee_gwei`. The full transaction ownership rules are specified in [llm-job-processing.md](./llm-job-processing.md).

When the Bridge call succeeds but the account balance is insufficient for the computed Credits at settle time, the service MUST still create a success `llm_call_records` row without a Credits amount field, MUST NOT create a Credits ledger event, and MUST emit an error log for operators.

## Cost Level

The project `priority_gwei` is the user Cost Level.

* Credits scale with `Prio`.
* Task fee scales with `Prio`.

Raising Cost Level MUST increase both Credits and the submitted task fee for the same model and token counts.

The live median priority MUST NOT change Credits or task fee by itself. When the queue becomes more congested, the same Cost Level MUST keep the same task fee and the same Credits; scheduling may become slower until the user raises `priority_gwei`.

Project create and update rules for `priority_gwei` are specified in [llm-api.md](./llm-api.md) Project Cost Level.

## Cost Level WebUI

The Crynux AS WebUI project Cost Level panel MUST control and display `projects.priority_gwei` as a decimal integer Gwei value.

The panel MUST provide:

1. A logarithmic slider bound to `priority_gwei`, mapped on the axis from `min_priority_gwei` to `max_priority_gwei` returned by `GET /v1/llm/billing_config`.
2. A numeric input bound to the same `priority_gwei`. On blur or save, the value MUST be parsed as a positive decimal integer and clamped into `[min_priority_gwei, max_priority_gwei]`.
3. A separate queue range bar under the slider when both `highest_priority_gwei` and `lowest_priority_gwei` are present in `billing_config`. The bar MUST map `lowest_priority_gwei` and `highest_priority_gwei` onto the same logarithmic axis as the slider bounds.

The panel MUST NOT read model rows from `GET /v1/llm/pricing_examples` for queue comparison. `pricing_examples` is only for per-model Credits and execution-time example tables.

Queue range bar rules:

1. Min marker: position of `lowest_priority_gwei` on the log axis.
2. Max marker: position of `highest_priority_gwei` on the log axis.
3. When at least one queue bound maps inside `[min_priority_gwei, max_priority_gwei]`, the segment between the clamped Min and Max positions MUST be blue, and the segments outside that interval MUST be red.
4. When both mapped queue bounds fall outside the allowed range on the same side, the entire bar MUST be red.
5. The panel MUST NOT draw Min and Max markers on the slider track itself.
6. The panel MUST NOT show a separate Queue position text block with effective priority, median, or ratio.
7. The panel MUST NOT present the markers as a queue wait time. The panel MUST NOT estimate queue wait seconds.

When `highest_priority_gwei` or `lowest_priority_gwei` is absent, the panel MUST omit the range bar.

When a project is created, the service MUST set `projects.priority_gwei` to the Queue Median Hint resolved by `ResolveQueueMedianHint`, clamped into `[min_priority_gwei, max_priority_gwei]`. The create request MUST NOT require the client to supply Cost Level.

The WebUI Credits and execution-time example tables on the project page MUST recompute when the user changes `priority_gwei`.

## Relationship to Task Fee

| Quantity | Credits | Task fee |
|----------|---------|----------|
| `T0`, `Tin`, `Tout` | Yes | Yes |
| `estimated_node_seconds` | Yes | Yes |
| `vram_weight` | Yes | Yes |
| `priority_gwei` (`Prio`) | Yes | Yes |
| `credits_per_gwei` (`G`) | Yes | No |
| Live `median_priority_gwei` | No | No |
| Live `highest_priority_gwei` / `lowest_priority_gwei` | No | No |

Credits and task fee MUST remain separate ledger outcomes. Credits debit the user Credits balance. Task fee is paid on the Crynux Network through Bridge raw task submission.

## Management APIs

### Billing config

`GET /v1/llm/billing_config` requires a valid JWT. It MUST return:

```json
{
  "message": "success",
  "data": {
    "base_vram": 8,
    "credits_per_gwei": "1",
    "min_priority_gwei": "1",
    "max_priority_gwei": "1000000000",
    "median_priority_gwei": "34",
    "highest_priority_gwei": "1363",
    "lowest_priority_gwei": "18"
  }
}
```

* `base_vram`, `credits_per_gwei`, `min_priority_gwei`, and `max_priority_gwei` MUST come from configuration.
* `min_priority_gwei`, `max_priority_gwei`, `median_priority_gwei`, `highest_priority_gwei`, and `lowest_priority_gwei` MUST be decimal integer strings when present.
* `median_priority_gwei` MUST be the current queued-priority cache median used for UI hints: the latest non-empty median when one exists; otherwise `empty_queue_median_priority_gwei`.
* `highest_priority_gwei` and `lowest_priority_gwei` MUST be the current non-empty queue bounds from the queued-priority cache. When the live queue is empty or either bound is unavailable, both fields MUST be `null`.

It MUST NOT return `reference_priority_gwei`, `max_token_ratio`, `prompt_credits_per_token`, `completion_credits_per_token`, or `vram_ratios`.

### Pricing examples

`GET /v1/llm/pricing_examples` requires a valid JWT. It MUST return only per-model example rows and the fixed token counts used by the example tables. It MUST NOT be the source for cost-level queue position. Queue position MUST use `GET /v1/llm/billing_config`.

Response shape:

```json
{
  "message": "success",
  "data": {
    "pricing_prompt_tokens": 1000000,
    "pricing_completion_tokens": 1000000,
    "time_prompt_tokens": 512,
    "time_completion_tokens": 2048,
    "examples": [
      {
        "model": "example/model",
        "min_vram": 24,
        "constant_seconds": 30,
        "seconds_per_input_token": 0.0004,
        "seconds_per_output_token": 0.1
      }
    ]
  }
}
```

Example selection MUST use the in-memory loaded-models LLM catalog:

1. Collect models with a positive `min_vram`.
2. Group by exact `min_vram`.
3. Within each group, choose the model with the greatest node count; ties break by model id ascending.
4. Sort the chosen group representatives by `min_vram` ascending.
5. If more than four representatives exist, keep exactly the representatives at indexes `0`, `floor((n-1)/3)`, `floor(2*(n-1)/3)`, and `n-1` in that sorted list, removing duplicates while preserving order.
6. If fewer than one representative exists, `examples` MUST be an empty list.

For each selected model, coefficients MUST come from Relay execution-time with `model=<selected model>` and `min_vram=<selected model min_vram>`.

`pricing_prompt_tokens` and `pricing_completion_tokens` MUST both be `1000000`. They are the token counts the UI MUST use for the Credits-per-1M-token Input and Output unit examples.

The WebUI Credits table title MUST be `Credits per 1M tokens`. Column headers MUST be `Model`, `1M Input`, and `1M Output`. The WebUI MUST NOT present the sum of the Input and Output cells as the Credits for one combined request.

Credits example cells MUST use marginal token work only. They MUST NOT include `constant_seconds` (`T0`):

* Input: `P=pricing_prompt_tokens`, `C=0`, and `estimated_node_seconds = Tin * P`
* Output: `P=0`, `C=pricing_completion_tokens`, and `estimated_node_seconds = Tout * C`

Then:

```text
billable_gwei = Prio * estimated_node_seconds * vram_weight
credits = floor(billable_gwei * G)
```

with `Prio` equal to the project's current `priority_gwei`, `vram_weight = max(min_vram, base_vram) / base_vram` using `base_vram` from `billing_config` and the example row `min_vram`, and `G` from `billing_config`. Pricing-example Credits cells MUST use `floor(billable_gwei * G)` without raising a zero result to `1`. The per-request minimum of `1` Credit applies only to Credits precheck and settle for a real LLM request.

`time_prompt_tokens` MUST be `512`. `time_completion_tokens` MUST be `2048`. They are the token counts the UI MUST use when showing estimated execution seconds for a typical smaller call. The time example MUST include `constant_seconds`:

```text
estimated_node_seconds =
    constant_seconds
    + seconds_per_input_token * time_prompt_tokens
    + seconds_per_output_token * time_completion_tokens
```

The WebUI execution-time table title MUST be `Execution time examples`. Column headers MUST be `Model`, `Input`, `Output`, and `Seconds`. The `Input` and `Output` cells MUST show `time_prompt_tokens` and `time_completion_tokens`.

The pricing-examples API MUST NOT return queue wait estimates. Queue wait MUST NOT be estimated from `median_priority_gwei` for this UI. The WebUI MUST load `credits_per_gwei`, `base_vram`, `min_priority_gwei`, `max_priority_gwei`, and `median_priority_gwei` from `billing_config` when computing Credits examples and the cost-level queue position.

## Failed Calls

Every failed LLM call MUST be recorded as one `llm_call_records` row with failure status. A failed call MUST NOT create a Credits ledger event.
