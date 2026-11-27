# Crynux AS Cost Level

Cost Level is a **project** setting. On each create, Crynux AS resolves it to that request's effective `priority_gwei`, which sets the submitted task fee and Credits charge. Queued tasks are matched in **priority descending** order (higher priority first; same priority uses task id). A higher Cost Level raises fee/Credits and places the task ahead of lower-priority queued tasks. It does not by itself guarantee a short wall-clock wait if no eligible node is free.

Use MCP tools:

- `get_cost_level` — read the project's current settings
- `set_cost_level` — change mode and/or values
- `get_cost_level_hints` — read Static bounds, queue median, live queue high/low, and the default Max Credits
- `estimate_task_credits` — dry-run create-time Credits and fee for one task under the current Cost Level

MCP cannot set `task_fee` on create. AS always computes the fee from Cost Level. `queue_timeout` only caps how long a task may wait in the queue; it does not raise Cost Level.

## Stored fields

| Field | Meaning |
|-------|---------|
| `cost_level_mode` | `static` or `auto` |
| `priority_gwei` | Static-mode priority as a decimal integer **Gwei** string. Must be a positive multiple of `1000` (one display unit = 1000 Gwei). |
| `auto_queue_position` | Auto-mode target in `[0, 100]`. `0` tracks the live queue low; `100` tracks the live queue high; `50` is mid-range. |
| `auto_max_credits_per_task` | Max Credits per task for **both** modes (positive decimal integer string). |

WebUI often shows Static Cost Level as an integer display unit equal to `priority_gwei / 1000`. MCP and HTTP APIs use the Gwei string (for example `"34000"` for display `34`).

## Modes

### Static

Effective priority is the stored `priority_gwei` on every create.

- Same workload with the same Max Credits produces a stable fee.
- If the estimated Credits at that priority exceed Max Credits, create is rejected without changing Cost Level.
- If the fee is below admission, create is rejected.

### Auto

AS resolves an uncapped target from the live Crynux queue:

```text
effective = lowest + floor((highest - lowest) * auto_queue_position / 100)
```

When the live queue is empty or bounds are unavailable, the target is the configured minimum priority. AS then clamps into configured hard bounds and adjusts during fee estimation so the request stays within Max Credits and meets the minimum fee when possible. If no executable priority exists inside Max Credits, create is rejected.

Switching mode does not clear the other mode's stored values. Setting `cost_level_mode` requires a valid Max Credits on the project (include `auto_max_credits_per_task` in the same `set_cost_level` call when it is missing).

## Using hints for reasonableness

Call `get_cost_level_hints` before or after changing settings. Fields:

| Field | Use |
|-------|-----|
| `min_priority_gwei` / `max_priority_gwei` | Inclusive Static bounds for `priority_gwei` |
| `median_priority_gwei` | Queue median hint (UI comparison; not a billing multiplier) |
| `highest_priority_gwei` / `lowest_priority_gwei` | Live non-empty queue bounds, or both `null` when the queue is empty |
| `default_auto_max_credits_per_task` | Default Max Credits for new projects |
| `credits_per_gwei` / `base_vram` | Inputs used when translating priority and VRAM into Credits |

Reasonableness checks:

1. **Static vs live queue:** If `priority_gwei` is far below `lowest_priority_gwei`, the task sits behind most of the queue. If it is at or above `highest_priority_gwei`, it competes at the front (higher Credits). Near `median_priority_gwei` is a mid-pack Static choice when the queue is non-empty.
2. **Auto position:** `0` / `50` / `100` map to low / mid / high of the current live bounds. Empty queue (`highest`/`lowest` null) means Auto falls back to the configured minimum until tasks appear.
3. **Max Credits:** Too small a Max Credits rejects creates that need a higher effective priority (busy queue or large workload). Raise `auto_max_credits_per_task` or lower the Static priority / Auto position.

## Estimate Credits

Call `estimate_task_credits` with the same `task_args`, `task_type`, and optional `min_vram` you would pass to `create_task` when you need the create-time Credits and fee before locking Credits.

- Uses the project's **current** Cost Level only. There is no what-if Cost Level override on this tool.
- Runs the same create preparation and admission checks as create (schema, model checks, Relay execution-time, Max Credits, minimum fee). Admission failures return an error; they do not create a job.
- Does **not** create a task job and does **not** lock Credits.
- Response `estimated_credits` is the create-time amount create would lock. `sufficient` is whether `available_credits` (`balance - locked`) covers that amount. When `sufficient` is false, create would fail with insufficient Credits; the estimate call itself still succeeds.
- `model_ready` is informational only. Estimate does not block on model readiness.
- **Image and Video:** create-time estimate matches settlement Credits for that task.
- **LLM:** create-time estimate uses prompt/completion precheck tokens. Settlement MAY differ when actual token usage differs from those estimates.

Use estimate after changing Cost Level, or before a large create, to confirm Max Credits admission and whether available Credits are enough.

## Typical create failures tied to Cost Level

- Fee or Credits fail admission, or estimate exceeds Max Credits → create fails; adjust with `set_cost_level` after reading hints. `estimate_task_credits` returns the same admission errors without locking Credits.
- Insufficient available Credits → payment failure on create; Cost Level tools do not add Credits. Estimate returns `sufficient: false` instead of a payment error.
- Cost Level behind many higher-priority queued tasks → longer queue wait until `queue_timeout`.
