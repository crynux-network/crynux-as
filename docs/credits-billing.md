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

`priority_gwei` MUST be the effective Cost Level resolved for the request from the project Cost Level mode and snapshotted at job create. For `static` mode that value MUST be `projects.priority_gwei`. For `auto` mode that value MUST be computed from the live queue bounds and the project's `auto_queue_position`, then capped by `auto_max_priority_gwei`, as specified in [llm-api.md](./llm-api.md) Project Cost Level.

The live queued-task `median_priority_gwei` MUST be used only for management UI comparison and MUST NOT enter `billable_gwei`, Credits, or task fee as a multiplier.

For `static` mode, the live `highest_priority_gwei` and `lowest_priority_gwei` MUST be used only for management UI comparison. They MUST NOT enter `billable_gwei`, Credits, or task fee.

For `auto` mode, the live `highest_priority_gwei` and `lowest_priority_gwei` MAY determine the effective `priority_gwei` at job create. After the job snapshot is written, later queue changes MUST NOT change that request's Credits or task fee.

For the same model, token counts, effective VRAM, and the same snapshotted effective `priority_gwei`, Credits and task fee MUST NOT change when the live queue median or bounds change after job create.

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
| Project Cost Level | `Prio` | Effective `priority_gwei` resolved from the project Cost Level mode at job create; snapshotted on the job and call record |
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

`Prio` MUST be the effective request `priority_gwei` snapshotted at job create. The service MUST NOT substitute the live queued-task median priority into `Prio`. For static mode the service MUST NOT substitute live queue bounds into `Prio`. For auto mode the service MUST resolve `Prio` once before estimation using the Project Cost Level rules in [llm-api.md](./llm-api.md) and MUST NOT re-resolve during settle.

## Execution-Time Coefficients

The service MUST obtain `T0`, `Tin`, and `Tout` from Relay `GET /v2/models/llm/execution-time` with query `model=<request model>` and `min_vram=<effective_vram>`, using the existing execution-time cache.

## Request Path Before Bridge Forwarding

For each LLM request that requires Credits precheck and task fee, the service MUST run these steps in order:

1. Resolve effective VRAM and precheck token estimates (`P` estimate and `C` estimate).
2. Resolve the request effective `priority_gwei` from the project Cost Level mode as specified in [llm-api.md](./llm-api.md) Project Cost Level.
3. Fetch and validate execution-time coefficients for `(model, effective_vram)`.
4. Compute `vram_weight`, `estimated_node_seconds` for the precheck token estimates, and `billable_gwei` using the request's resolved effective `priority_gwei`.
5. Reject with HTTP 402 when the account balance is strictly less than `max(1, floor(billable_gwei * G))`.
6. Set `task_fee_gwei = floor(billable_gwei)` from the same `billable_gwei`.
7. Resolve `median_priority_gwei` with the Queue Median Hint rules in [llm-api.md](./llm-api.md). This resolution MUST always produce a value and MUST NOT fail the request.
8. Persist on the LLM job: the resolved effective `priority_gwei`, `constant_seconds`, `seconds_per_input_token`, `seconds_per_output_token`, `vram_weight`, `task_fee_gwei`, precheck `estimated_node_seconds`, and the resolved `median_priority_gwei` snapshot.

Credits precheck and task fee MUST use the same coefficient fetch and the same `billable_gwei`. The service MUST NOT run a Credits precheck that uses a different formula or a second coefficient fetch from the task fee path.

When step 3 fails validation or Relay fetch rules in [llm-api.md](./llm-api.md) Task Fee Estimation Failure, the service MUST NOT forward the request and MUST NOT charge Credits for that attempt.

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

The effective request `priority_gwei` is the user Cost Level used for charging.

* Credits scale with `Prio`.
* Task fee scales with `Prio`.

Raising the effective Cost Level MUST increase both Credits and the submitted task fee for the same model and token counts.

In `static` mode, the live median priority and queue bounds MUST NOT change Credits or task fee by themselves. When the queue becomes more congested, the same stored `priority_gwei` MUST keep the same task fee and the same Credits; scheduling may become slower until the user raises `priority_gwei`.

In `auto` mode, different requests from the same project MAY resolve different effective `priority_gwei` values when the live queue bounds change. Each request MUST still charge from its own create-time snapshot.

Project create and update rules for Cost Level fields are specified in [llm-api.md](./llm-api.md) Project Cost Level.

## Cost Level WebUI

The Crynux AS WebUI project Cost Level panel MUST support modes `static` and `auto` with a short explanation for each mode. The mode cards MUST place Auto on the left and Static on the right.

* Auto: the system adjusts Cost Level to the user's chosen queue position. Credits for the same task can change, but they never exceed the user's set maximum.
* Static: the project uses a fixed Cost Level. The same task always spends the same Credits, but tasks may wait too long in the queue.

The Cost Level panel intro copy MUST state that Cost Level controls how many Credits each request spends and how long tasks wait in the queue. That intro MUST appear above the mode cards for every mode. User-facing Cost Level panel copy MUST NOT mention Gwei or task fee in the intro or in the mode card explanations.

The panel MUST persist mode and both mode setting sets independently. Switching mode MUST NOT clear or overwrite the other mode's stored values.

When the Cost Level panel is collapsed, the header preview MUST show a mode tag (`Auto` or `Static`) and the current setting values in a unified layout: Auto MUST show `auto_queue_position` as a percent and `auto_max_priority_gwei` as `position% · max`; Static MUST show only `priority_gwei`.

### Static controls

When mode is `static`, the panel MUST provide:

1. A logarithmic slider bound to `priority_gwei`, mapped on the axis from `min_priority_gwei` to `max_priority_gwei` returned by `GET /v1/llm/billing_config`.
2. A numeric input bound to the same `priority_gwei`. On blur or save, the value MUST be parsed as a positive decimal integer and clamped into `[min_priority_gwei, max_priority_gwei]`.
3. A separate queue range bar under the slider when both `highest_priority_gwei` and `lowest_priority_gwei` are present in `billing_config`. The bar MUST map `lowest_priority_gwei` and `highest_priority_gwei` onto the same logarithmic axis as the slider bounds.

Static control layout:

1. The numeric input MUST be on the left of the control group.
2. The slider MUST be to the right of the numeric input, with a `Cheaper` label left of the track and a `Faster` label right of the track.
3. The queue range bar MUST sit directly under the slider track.
4. The queue range bar track MUST share the same width and the same left/right edges as the visible slider track (`SliderTrack`). It MUST NOT extend under `Cheaper`, `Faster`, or the numeric input. It MUST NOT add horizontal padding or inset relative to the slider column. Thumb overhang MUST NOT change the range bar width.
5. The numeric `priority_gwei` control MUST use a larger type size than the side labels and MUST use the primary highlight text color. Its outer bordered box MUST use a tall fixed control height with flexbox-centered digits around a natural-height input.
6. The digits MUST be vertically centered inside that outer box. The implementation MUST NOT rely on a tall native `<input>` height plus line-height to center the text.
7. The numeric box MUST stay left-aligned. The slider-plus-range-bar block MUST occupy about 80% of the remaining row width and MUST be horizontally centered in that remaining space. The queue range bar MUST size to its visible track and Current queue min / Current queue max labels without extra empty height below the labels.
8. Spacing from the intro text to the mode cards, from the mode cards to the control row, and from the control row to the example tables MUST use the larger panel gaps used by the WebUI Cost Level panel.

Queue range bar rules:

1. Current queue min marker: position of `lowest_priority_gwei` on the log axis. The label under the marker MUST be `Current queue min`.
2. Current queue max marker: position of `highest_priority_gwei` on the log axis. The label under the marker MUST be `Current queue max`.
3. When at least one queue bound maps inside `[min_priority_gwei, max_priority_gwei]`, the segment between the clamped Current queue min and Current queue max positions MUST be blue, and the segments outside that interval MUST be red.
4. When both mapped queue bounds fall outside the allowed range on the same side, the entire bar MUST be red.
5. The panel MUST NOT draw Current queue min and Current queue max markers on the slider track itself.
6. The panel MUST NOT show a separate Queue position text block with effective priority, median, or ratio.
7. The panel MUST NOT present the markers as a queue wait time. The panel MUST NOT estimate queue wait seconds.

When `highest_priority_gwei` or `lowest_priority_gwei` is absent, the panel MUST omit the range bar.

### Auto controls

When mode is `auto`, the panel MUST provide two separate setting groups. The groups MUST NOT share one slider. Each group MUST use the same control chrome as Static mode: a left tall numeric input and a right dual-bar block.

#### Queue position group

1. A numeric input and linear slider bound to `auto_queue_position`.
2. The WebUI MUST clamp `auto_queue_position` into `[1, 99]` for display, editing, and save. Values loaded from the API outside that range MUST be clamped into `[1, 99]` before display.
3. The dual-bar block MUST place the linear slider above a fixed decorative range bar. The range bar MUST be symmetric: red from `0%` to `10%`, blue from `10%` to `90%`, and red from `90%` to `100%` of the track width. Marker labels under the blue ends MUST be `Queue min` and `Queue max`.
4. The position slider MUST NOT accept values in the red segments. Its `min` MUST be `1` and its `max` MUST be `99`. The slider track MUST be horizontally inset so value `1` sits on the `Queue min` marker and value `99` sits on the `Queue max` marker; the thumb MUST NOT travel over the red segments.

#### Max Cost Level group

1. A numeric input and logarithmic slider bound to `auto_max_priority_gwei`, using the same hard bounds and parsing/clamping rules as the static Gwei controls.
2. The dual-bar block MUST match the Static control layout: `Cheaper` / logarithmic slider / `Faster`, with the live Current queue min / Current queue max range bar under the slider when live bounds are present.
3. When the project has no stored `auto_max_priority_gwei` and the user first selects Auto, the WebUI MUST initialize the cap to the current `highest_priority_gwei` from `billing_config` when present, otherwise `min_priority_gwei`, then save that value with the mode change.

#### Auto control layout

1. The Queue position group MUST appear above the Max Cost Level group.
2. Each group MUST use a bordered section with a group title (`Queue position`, `Max Cost Level`) and a short group description. The Queue position description MUST state that when each task is sent, the system reads the current queue min and max Cost Level and sets this request's Cost Level from the configured position. The Queue position description MUST NOT say that values outside the blue range are not allowed.
3. Vertical spacing between the two groups MUST be larger than the spacing inside one dual-bar control.
4. Inside each group, the numeric input MUST use the Static tall input treatment (`h-20`, primary digits, flex-centered). The dual-bar block MUST occupy about 80% of the remaining row width and MUST be horizontally centered in that remaining space.
5. The Auto controls MUST NOT allow a Gwei value outside the configured hard bounds.
6. Spacing from the mode cards to the Auto control block, and from the Auto control block to the example tables, MUST use the same larger panel gaps as Static (`mb-20`).

### Example tables

The panel MUST NOT read model rows from `GET /v1/llm/pricing_examples` for queue comparison. `pricing_examples` is only for per-model Credits and execution-time example tables.

When mode is `static`, the WebUI Credits and execution-time example tables on the project page MUST recompute when the user changes `priority_gwei`. Credits example cells MUST use the current static `priority_gwei`.

When mode is `auto`, Credits example cells MUST use `auto_max_priority_gwei`. The Credits table title MUST be `Maximum Credits per 1M tokens`. The execution-time table MUST remain independent of Cost Level.

The project list MUST distinguish modes:

* Static: show a `Static` mode tag, the stored `priority_gwei`, and the current queue-range status tag derived from that value (`In range`, `Too low`, or `Too high`). The `priority_gwei` number color MUST use a discrete palette: `too_low` and `Slowest`/`Slower` in red, `Balanced` in blue, `Faster`/`Fastest` and `too_high` in yellow. The WebUI MUST NOT use a continuous tone gradient for that color.
* Auto: show an `Auto` mode tag, the stored `auto_queue_position` as a percent, and the stored `auto_max_priority_gwei` as Max. The list MUST NOT present a previous request's snapshotted effective priority as the current Auto setting.
## Relationship to Task Fee

| Quantity | Credits | Task fee |
|----------|---------|----------|
| `T0`, `Tin`, `Tout` | Yes | Yes |
| `estimated_node_seconds` | Yes | Yes |
| `vram_weight` | Yes | Yes |
| `priority_gwei` (`Prio`) | Yes | Yes |
| `credits_per_gwei` (`G`) | Yes | No |
| Live `median_priority_gwei` | No | No |
| Live `highest_priority_gwei` / `lowest_priority_gwei` for static mode | No | No |
| Live `highest_priority_gwei` / `lowest_priority_gwei` for auto effective-priority resolution at job create | Yes, only through resolved `Prio` | Yes, only through resolved `Prio` |

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

The WebUI Credits table title MUST be `Credits per 1M tokens` in static mode and `Maximum Credits per 1M tokens` in auto mode. Column headers MUST be `Model`, `1M Input`, and `1M Output`. The WebUI MUST NOT present the sum of the Input and Output cells as the Credits for one combined request.

Credits example cells MUST use marginal token work only. They MUST NOT include `constant_seconds` (`T0`):

* Input: `P=pricing_prompt_tokens`, `C=0`, and `estimated_node_seconds = Tin * P`
* Output: `P=0`, `C=pricing_completion_tokens`, and `estimated_node_seconds = Tout * C`

Then:

```text
billable_gwei = Prio * estimated_node_seconds * vram_weight
credits = floor(billable_gwei * G)
```

with `Prio` equal to the project's current static `priority_gwei` when mode is `static`, or the project's `auto_max_priority_gwei` when mode is `auto`, `vram_weight = max(min_vram, base_vram) / base_vram` using `base_vram` from `billing_config` and the example row `min_vram`, and `G` from `billing_config`. Pricing-example Credits cells MUST use `floor(billable_gwei * G)` without raising a zero result to `1`. The per-request minimum of `1` Credit applies only to Credits precheck and settle for a real LLM request.

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
