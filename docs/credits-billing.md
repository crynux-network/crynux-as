# Credits Billing Model

This document is the complete specification for charging Credits on LLM calls in Crynux AS, and for the shared `billable_gwei` amount used to set the submitted task fee.

Task fee persistence, Bridge submission, and estimation failure HTTP behavior that are not Credits-specific remain in [llm-api.md](./llm-api.md) Task Fee Estimation. The fee formula itself MUST match this document.

## Model Summary

Crynux AS MUST NOT maintain a separate Credits price table for prompt tokens, completion tokens, or discrete VRAM billing tiers.

Credits and the submitted task fee MUST share one amount:

```text
billable_gwei =
    reference_priority_gwei
    * token_ratio
    * estimated_node_seconds
    * vram_weight
```

* `task_fee_gwei = floor(billable_gwei)`
* `credits = floor(billable_gwei * credits_per_gwei)`

Both MUST use the configured fixed `llm.reference_priority_gwei`. Neither MUST use the live queued-task `median_priority_gwei`.

The project cost level (`token_ratio`) MUST scale both Credits and task fee. For the same model, token counts, effective VRAM, and cost level, Credits and task fee MUST NOT change when the live queue median changes.

The live queued-task `median_priority_gwei` MUST be used only for management UI comparison against `reference_priority_gwei * token_ratio`. It MUST NOT change Credits or task fee.

When the live median moves far above or below the range reachable by allowed cost levels against the fixed reference, operators MUST adjust `reference_priority_gwei`. The service MUST NOT automatically retarget task fee to the live median.

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
  reference_priority_gwei: "34"
  credits_per_gwei: 1
  max_token_ratio: 30
  job_submit_timeout: 600
  job_retention_days: 30
```

Configuration loading MUST fail when any of these values is missing or not greater than zero:

* `base_vram`
* `reference_priority_gwei`
* `credits_per_gwei`
* `empty_queue_median_priority_gwei`
* `max_token_ratio` (MUST be an integer greater than or equal to `2`)

The following items MUST NOT appear in configuration and MUST NOT be used for Credits or task fee:

* `prompt_credits_per_token`
* `completion_credits_per_token`
* `vram_ratios`

`reference_priority_gwei` is the fixed priority anchor for both Credits and task fee.

`credits_per_gwei` converts `billable_gwei` into Credits. It MUST NOT change `task_fee_gwei`.

`reference_priority_gwei`, `empty_queue_median_priority_gwei`, and every API or persisted field whose name ends in `_gwei` MUST be encoded as a decimal integer string (the same representation used for other Gwei amounts in Crynux AS). `credits_per_gwei` MUST remain an unsigned integer and MUST NOT use the Gwei string encoding.

`empty_queue_median_priority_gwei` is used only when presenting a queue-median hint and the queued-priority cache has never observed a non-empty queue. It MUST NOT enter `billable_gwei`.

`max_token_ratio` is the maximum allowed project cost level display value. The allowed `token_ratio` set is `0.1` through `1.0` in steps of `0.1`, then `2` through `max_token_ratio` in steps of `1`.

`base_vram` MUST remain aligned with Relay `task_pricing.base_vram`. Every YAML configuration template MUST set it to `8`.

## Charge Inputs

| Input | Symbol | Source |
|-------|--------|--------|
| Prompt tokens | `P` | Bridge `usage.prompt_tokens` at settle; precheck estimate before forward |
| Completion tokens | `C` | Bridge `usage.completion_tokens` at settle; precheck estimate before forward |
| Token ratio display | `R` | `projects.token_ratio` stored integer divided by `10` |
| Effective VRAM | `v` | Resolved effective VRAM in GB |
| Base VRAM | `B` | `llm.base_vram` |
| Constant seconds | `T0` | Relay LLM execution-time coefficient for `(model, min_vram=v)` |
| Seconds per input token | `Tin` | Same Relay response |
| Seconds per output token | `Tout` | Same Relay response |
| Reference priority | `Pref` | `llm.reference_priority_gwei` |
| Credits per Gwei | `G` | `llm.credits_per_gwei` |

Version 1 covers text work only. Model-switch seconds and image work MUST NOT enter the formula.

## Derived Quantities

```text
estimated_node_seconds =
    T0 + Tin * P + Tout * C

vram_weight =
    max(v, B) / B

billable_gwei =
    Pref * R * estimated_node_seconds * vram_weight

task_fee_gwei =
    floor(billable_gwei)

credits =
    floor(billable_gwei * G)
```

`billable_gwei`, `task_fee_gwei`, and `credits` MUST be computed with floating-point intermediates and MUST truncate toward zero to non-negative integer values.

`Pref` MUST be the configured `reference_priority_gwei`. The service MUST NOT substitute the live queued-task median priority into `Pref`.

## Execution-Time Coefficients

The service MUST obtain `T0`, `Tin`, and `Tout` from Relay `GET /v2/models/llm/execution-time` with query `model=<request model>` and `min_vram=<effective_vram>`, using the existing execution-time cache.

## Request Path Before Bridge Forwarding

For each LLM request that requires Credits precheck and task fee, the service MUST run these steps in order:

1. Resolve effective VRAM and precheck token estimates (`P` estimate and `C` estimate).
2. Fetch and validate execution-time coefficients for `(model, effective_vram)`.
3. Compute `vram_weight`, `estimated_node_seconds` for the precheck token estimates, and `billable_gwei`.
4. Reject with HTTP 402 when the account balance is strictly less than `floor(billable_gwei * G)`.
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
2. The service MUST compute Credits with the charge formula using persisted job coefficients, persisted `vram_weight`, the project `token_ratio` used for the call, and the usage token counts.
3. The service MUST create one `llm_call_records` row with success status, token counts, the project `token_ratio` used for the charge, the billed effective VRAM, call duration, the pre-forward Task Fee Estimation fields, and the Credits recalculation snapshot (`constant_seconds`, `seconds_per_input_token`, `seconds_per_output_token`, `reference_priority_gwei`, `credits_per_gwei`). The call record MUST NOT store the charged Credits amount. The call record `estimated_node_seconds` MUST be the pre-forward value persisted on the job. It MUST NOT be replaced by the settle-time recomputation used only for Credits.
4. When the computed Credits are greater than zero and the account balance is sufficient, the service MUST create one `credit_events` row of type LLM charge referencing the call record ID and MUST decrease the account balance by the same amount in the same database transaction. The event amount is the sole permanent store of the charged Credits.
5. The service MUST mark the LLM job `completed` only after steps 1 through 4 succeed.

Settle MUST NOT re-fetch Relay coefficients for the charge. Settle MUST NOT recompute or replace the already submitted `task_fee_gwei`. The full transaction ownership rules are specified in [llm-job-processing.md](./llm-job-processing.md).

When the Bridge call succeeds but the account balance is insufficient for the computed Credits at settle time, the service MUST still create a success `llm_call_records` row without a Credits amount field, MUST NOT create a Credits ledger event, and MUST emit an error log for operators.

## Cost Level

The project `token_ratio` is the user cost level.

* Credits scale with `R`.
* Task fee scales with `R` against the fixed `reference_priority_gwei`.

Raising cost level MUST increase both Credits and the submitted task fee for the same model and token counts.

The live median priority MUST NOT change Credits or task fee by itself. When the queue becomes more congested relative to the fixed reference, the same cost level MUST keep the same task fee and the same Credits; scheduling may become slower until the user raises cost level or an operator raises `reference_priority_gwei`.

## Cost Level Queue Position Display

The Crynux AS WebUI project Cost Level panel MUST show where the live task queue sits on the cost level slider, using `reference_priority_gwei`, `highest_priority_gwei`, and `lowest_priority_gwei` from `GET /v1/llm/billing_config`.

The panel MUST NOT read model rows from `GET /v1/llm/pricing_examples` for this comparison. `pricing_examples` is only for per-model Credits and execution-time example tables.

Define:

```text
queue_cost_level = queue_priority_gwei / reference_priority_gwei
```

`queue_cost_level` MUST NOT depend on model id, execution-time coefficients, or VRAM. Under the shared `billable_gwei` formula, Relay task priority equals `reference_priority_gwei * token_ratio_display` after dividing out `estimated_node_seconds * vram_weight`, so a queue priority maps to the cost level that would match it.

The Cost Level panel MUST show a separate range bar under the cost level slider when both `highest_priority_gwei` and `lowest_priority_gwei` are present:

1. Queue min: the bar position for `lowest_priority_gwei / reference_priority_gwei`
2. Queue max: the bar position for `highest_priority_gwei / reference_priority_gwei`

Each marker MUST snap to the nearest allowed cost level on the slider scale. When the mapped cost level is below the slider minimum or above the slider maximum, the marker MUST clamp to that end of the bar. The blue segment MUST stay strictly between the Min and Max markers.

When at least one mapped cost level falls inside the allowed cost level range, the bar segment between the clamped Min and Max positions MUST be blue, and the segments outside that interval MUST be red. When both mapped cost levels fall outside the allowed cost level range on the same side, the entire bar MUST be red.

The panel MUST NOT draw Min and Max markers on the cost level slider track itself. The panel MUST NOT show a separate Queue position text block with effective priority, median, or ratio. The panel MUST NOT present the markers as a queue wait time. The panel MUST NOT estimate queue wait seconds.

When `highest_priority_gwei` or `lowest_priority_gwei` is absent, the panel MUST omit the range bar.

## Relationship to Task Fee

| Quantity | Credits | Task fee |
|----------|---------|----------|
| `T0`, `Tin`, `Tout` | Yes | Yes |
| `estimated_node_seconds` | Yes | Yes |
| `vram_weight` | Yes | Yes |
| `token_ratio` (`R`) | Yes | Yes |
| `reference_priority_gwei` (`Pref`) | Yes | Yes |
| `credits_per_gwei` (`G`) | Yes | No |
| Live `median_priority_gwei` | No | No |

Credits and task fee MUST remain separate ledger outcomes. Credits debit the user Credits balance. Task fee is paid on the Crynux Network through Bridge raw task submission.

## Management APIs

### Billing config

`GET /v1/llm/billing_config` requires a valid JWT. It MUST return:

```json
{
  "message": "success",
  "data": {
    "base_vram": 8,
    "reference_priority_gwei": "34",
    "credits_per_gwei": 1,
    "max_token_ratio": 30,
    "median_priority_gwei": "34",
    "highest_priority_gwei": "1363",
    "lowest_priority_gwei": "18"
  }
}
```

* `reference_priority_gwei`, `credits_per_gwei`, and `max_token_ratio` MUST come from configuration.
* `max_token_ratio` MUST be the configured maximum allowed cost level display value.
* `reference_priority_gwei`, `median_priority_gwei`, `highest_priority_gwei`, and `lowest_priority_gwei` MUST be decimal integer strings when present.
* `median_priority_gwei` MUST be the current queued-priority cache median used for UI hints: the latest non-empty median when one exists; otherwise `empty_queue_median_priority_gwei`.
* `highest_priority_gwei` and `lowest_priority_gwei` MUST be the current non-empty queue bounds from the queued-priority cache. When the live queue is empty or either bound is unavailable, both fields MUST be `null`.

It MUST NOT return `prompt_credits_per_token`, `completion_credits_per_token`, or `vram_ratios`.

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
billable_gwei = Pref * R * estimated_node_seconds * vram_weight
credits = floor(billable_gwei * G)
```

with `vram_weight = max(min_vram, base_vram) / base_vram` using `base_vram` from `billing_config` and the example row `min_vram`.

`time_prompt_tokens` MUST be `512`. `time_completion_tokens` MUST be `2048`. They are the token counts the UI MUST use when showing estimated execution seconds for a typical smaller call. The time example MUST include `constant_seconds`:

```text
estimated_node_seconds =
    constant_seconds
    + seconds_per_input_token * time_prompt_tokens
    + seconds_per_output_token * time_completion_tokens
```

The WebUI execution-time table title MUST be `Execution time examples`. Column headers MUST be `Model`, `Input`, `Output`, and `Seconds`. The `Input` and `Output` cells MUST show `time_prompt_tokens` and `time_completion_tokens`.
The pricing-examples API MUST NOT return queue wait estimates. Queue wait MUST NOT be estimated from `median_priority_gwei` for this UI. The WebUI MUST load `reference_priority_gwei`, `credits_per_gwei`, `base_vram`, and `median_priority_gwei` from `billing_config` when computing Credits examples and the cost-level queue position.

## Failed Calls

Every failed LLM call MUST be recorded as one `llm_call_records` row with failure status. A failed call MUST NOT create a Credits ledger event.
