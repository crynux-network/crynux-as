# Functional Requirements

## Scope

Crynux AS exposes Crynux Network and configured third-party Service Provider models as managed services. It manages user accounts, on-chain ERC20 payments that purchase Credits, referral invite codes and referral Credits rewards, projects with private OpenAI-compatible LLM API endpoints, usage-based Credits charging, per-project usage statistics, encrypted Shared API Keys, Key Owner CNX earnings, and CNX withdrawals. Crynux models execute through Crynux Bridge. Configured OpenAI, Anthropic, and Google models execute through the Provider Key Sharing architecture specified in [provider-key-sharing/](./provider-key-sharing/).

## Wallet Login and Accounts

The wallet address is the user identity. One wallet address maps to exactly one user account.

Login MUST use a wallet signature:

1. The client signs a login message that contains the wallet address and a unix timestamp, using the Ethereum personal sign format.
2. `POST /v1/auth/login` receives the address, timestamp, signature, and optional `invite_code`. The server MUST verify that the recovered signer address equals the submitted address and that the timestamp is within the accepted validity window.
3. On successful verification, the server MUST create the user account if it does not exist, and MUST return a JWT token bound to the wallet address.

When the server creates a new user account, it MUST also create the user's `credit_accounts` row and one active `referral_codes` row in the same database transaction. When `credits.signup_bonus` is greater than zero, that transaction MUST set the new account balance to `credits.signup_bonus` and MUST write one processed `credit_events` row of type signup bonus with `ref_id` equal to the new user ID and amount equal to `credits.signup_bonus`. When `credits.signup_bonus` is `0`, the new account balance MUST be `0` and the server MUST NOT write a signup-bonus ledger event. An existing user account MUST NOT receive another signup bonus on later logins.

When the server creates a new user account and `invite_code` is non-empty, the server MUST look up an active `referral_codes` row by exact `code`. When a matching active code exists and the code owner is not the new user, the transaction MUST write one `referral_bindings` row (`invitee_user_id` unique) that permanently binds the new user to that referrer. When `credits.referral.invitee_bonus` is greater than zero, that transaction MUST also increase the new account balance by `invitee_bonus` and MUST write one processed `credit_events` row of type invitee bonus with `ref_id` equal to the new user ID. When the invite code is missing, inactive, unknown, or owned by the new user, the server MUST still create the account and MUST NOT bind a referrer or grant invitee bonus. An existing user account MUST ignore `invite_code` on later logins.

`credits.signup_bonus` MUST be present in configuration. The value MUST be a non-negative integer. The example configuration value is `1000`.

`credits.referral.deposit_reward_ratio`, `credits.referral.invitee_bonus`, and `credits.referral.purchase_bonus_tiers` MUST be present in configuration. `deposit_reward_ratio` MUST be a non-negative decimal string less than or equal to `1`; `0` disables the base deposit referral reward. `invitee_bonus` MUST be a non-negative integer; `0` disables the invitee signup bonus. `purchase_bonus_tiers` MUST be a list and MAY be empty. Each tier MUST define `min_purchases` as a positive integer and `extra_ratio` as a non-negative decimal string less than or equal to `1`. `min_purchases` values MUST be unique across tiers. The sum of `deposit_reward_ratio` and every tier `extra_ratio` MUST be less than or equal to `1`. Example values are `deposit_reward_ratio: "0.10"`, `invitee_bonus: 500`, and tiers `{min_purchases: 50, extra_ratio: "0.05"}` and `{min_purchases: 200, extra_ratio: "0.10"}`.

All account and project management APIs MUST require a valid JWT token in the `Authorization: Bearer <token>` header. A request without a valid token MUST be rejected with HTTP 401.

Referral management APIs:

* `GET /v1/account/referral` MUST return the account's active invite code, invitee count, invitee purchase count, and the configured referral policy fields including `purchase_bonus_tiers`.
* `POST /v1/account/referral/code/reset` MUST revoke the current active invite code and create a new active invite code for the same user. Existing `referral_bindings` MUST remain unchanged so later deposit rewards continue to use the bound `referrer_user_id`.
* `GET /v1/account/referral/rewards` MUST return the referrer's reward history with `offset`/`limit` pagination. Each deposit reward item MUST include `amount`, `deposit_credits`, `prior_purchase_count`, `tier_min_purchases`, `extra_ratio`, and `extra_amount`.

`GET /v1/account/purchases` MUST return the account's processed Credits inflows of types purchase (deposit), signup bonus, and invitee bonus, newest first, with `offset`/`limit` pagination. Referral rewards earned as a referrer MUST NOT appear in this list.

## Deposits and Credits

### Payment Model

Users purchase Credits by transferring supported ERC20 tokens from their login wallet directly to the platform receiving address of a configured blockchain network. There is no per-user deposit address; the deposit is attributed to the user account whose wallet address equals the `from` address of the ERC20 `Transfer` event.

### Supported Networks and Tokens

The set of supported blockchain networks and the supported ERC20 tokens on each network are defined in the service configuration. Each network configuration MUST define the chain ID, RPC endpoint, request rate limit, scan start block, log scan block range, scan interval in seconds, the receiving address, and for every supported token the contract address, decimals, and `credits_per_token`.

`credits_per_token` is the number of Credits awarded for one whole token (`10^decimals` raw units). The conversion MUST use integer arithmetic:

```text
credits = amount * credits_per_token / 10^decimals
```

### Deposit Detection

For each configured blockchain network, the service MUST run a scanning worker that:

1. Maintains a per-network scan cursor (`blockchain_cursors`) recording the last processed block number.
2. Polls for new blocks on the configured `scan_interval`.
3. Fetches ERC20 `Transfer(address,address,uint256)` logs of all configured token contracts on that network where the `to` address is the receiving address, in block ranges no larger than the configured `log_block_range`.
4. Rate-limits all RPC requests to the configured RPS.
5. Advances the cursor only after all logs in the scanned range have been handled (credited or deliberately ignored).

### Crediting Rules

For each detected transfer log, the service MUST:

1. Convert the token amount to Credits using `credits_per_token`. When the computed Credits is less than `1`, the service MUST emit a warning log and MUST NOT create a deposit row or Credits ledger event.
2. Attribute the transfer to the user account whose wallet address equals the `from` address of the transfer. If no account exists for the `from` address, the service MUST emit a warning log and MUST NOT create a deposit row or Credits ledger event.
3. When the user account exists and the computed Credits is at least `1`, record a deposit row identified by network, transaction hash, and log index. This identity MUST be unique; re-scanning the same log MUST NOT create a second deposit or credit the account twice.
4. Create a Credits ledger event of type deposit referencing the deposit row ID, and update the account balance.
5. When the depositing user has a `referral_bindings` row, let `prior_count` be the number of `deposits` rows whose `user_id` belongs to any invitee bound to that referrer, excluding the deposit being credited. Let `extra_ratio` be the sum of `extra_ratio` for every configured `purchase_bonus_tiers` entry whose `min_purchases` is less than or equal to `prior_count`. Let `effective_ratio` be `deposit_reward_ratio + extra_ratio`. Compute `reward = floor(deposit_credits * effective_ratio)` with integer truncation toward zero. When `reward` is greater than zero, the same transaction MUST insert one `referral_rewards` row of kind deposit reward keyed uniquely by `deposit_id`, storing `prior_purchase_count = prior_count`, `tier_min_purchases` as the highest matching tier `min_purchases` or null when none match, the summed `extra_ratio`, and `extra_amount = reward - floor(deposit_credits * deposit_reward_ratio)` (never negative), write one processed `credit_events` row of type referral reward with `ref_id` equal to that reward ID for the referrer, and increase the referrer's `credit_accounts.balance` by `reward`. When there is no binding, `effective_ratio` is `0`, or `reward` is `0`, the server MUST NOT create a referral reward.

Deposits and Credits balance changes MUST go through the Credits ledger: every balance change MUST be recorded as a `credit_events` row referencing its source record ID (`ref_id`), and the `credit_accounts` balance MUST equal the sum of its processed events. The event type + `ref_id` pair MUST be unique so one source record produces at most one ledger event. Signup-bonus and invitee-bonus events use the user ID as `ref_id`. Referral-reward events use the `referral_rewards` row ID as `ref_id`.

## Projects and Private LLM API Endpoints

A user account can create multiple projects. Each project has:

* A name.
* A unique `endpoint_token`: a cryptographically random string generated at project creation. The private LLM API base URL of the project is `/api/<endpoint_token>/v1`.
* Exactly one API key: a cryptographically random secret generated at project creation and stored on the project as a key hash and a short public prefix. The plaintext key is shown to the user only once at creation and when reset.
* A Cost Level configuration with mode `static` or `auto`. Static mode stores a fixed `priority_gwei`. Auto mode stores an independent `auto_queue_position` (0–100) and `auto_max_priority_gwei` cap. The effective `priority_gwei` used for Credits and the submitted task fee is resolved at each LLM request and snapshotted on the job. See [llm-api.md](./llm-api.md) and [credits-billing.md](./credits-billing.md) for the bounds, defaults, resolution rules, and charging rules.
* Usage summary fields maintained by the usage stats workers: `last_request_at`, `request_count_day`, `success_count_day`, `failure_count_day`, `credits_day`, `recent_window_success_count`, and `recent_window_failure_count`. The day fields cover the current Unix natural day. The recent-window fields cover the configured recent-failure window aligned to Unix 10-minute buckets. `GET /v1/projects` and `GET /v1/projects/:project_id` MUST return those fields and MUST also return `elevated_recent_failure_rate`, which is true when `(recent_window_success_count + recent_window_failure_count) > 0` and `recent_window_failure_count / (recent_window_success_count + recent_window_failure_count)` is strictly greater than `usage_stats.recent_failure_rate_threshold`.

Project management APIs:

* `POST /v1/projects`, `GET /v1/projects`, `GET /v1/projects/:project_id`, `PUT /v1/projects/:project_id`, `DELETE /v1/projects/:project_id`.
* `POST /v1/projects/:project_id/api_key/reset`.
* `GET /v1/projects/:project_id/stats`.

`POST /v1/projects` creates the project and its API key together and returns the plaintext API key once. `POST /v1/projects/:project_id/api_key/reset` regenerates the API key for the same project, invalidates the previous secret immediately, and returns the new plaintext once.

A project MUST only be visible to and manageable by its owning account.

## OpenAI-Compatible LLM API

The detailed endpoint, Bridge forwarding, Cost Level modes and effective `priority_gwei` resolution, pricing, balance precheck, and Credits settle rules are specified in [llm-api.md](./llm-api.md) and [credits-billing.md](./credits-billing.md).

### Endpoints

Each project exposes the following endpoints under its private base URL:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/responses`
* `GET /api/<endpoint_token>/v1/responses/<response_id>`
* `POST /api/<endpoint_token>/v1/images/generations`
* `POST /api/<endpoint_token>/v1/<vram_limit>/images/generations`
* `POST /api/<endpoint_token>/v1/images/edits`
* `POST /api/<endpoint_token>/v1/<vram_limit>/images/edits`
* `POST /api/<endpoint_token>/v1/<vram_limit>/chat/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/responses`
* `GET /api/<endpoint_token>/v1/models`
* `GET /api/<endpoint_token>/v1/models/<model>`

The request and response formats are OpenAI-compatible for the supported fields of each endpoint.

The models endpoints return one merged catalog. Crynux rows come from the Relay loaded-model snapshot and MUST include only models whose Relay `on_disk_node_count` is at least `llm.min_catalog_on_disk_node_count`. Provider rows come from explicit YAML model configuration and appear only while the Service Provider has at least one enabled, non-deleted, system-valid Shared API Key. Provider model names MUST remain their official API names without a prefix or alias. See [llm-api.md](./llm-api.md) and [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md).

A Crynux request MAY specify a VRAM limit in GB through the `<vram_limit>` URL path segment or the `vram_limit` body field; the path value overrides the body value. The resolved effective VRAM selects `vram_weight` for Credits and task fee and is sent to Bridge as raw task `min_vram`. Provider requests MUST reject VRAM fields and path-based VRAM selection. See [llm-api.md](./llm-api.md), [credits-billing.md](./credits-billing.md), and [provider-key-sharing/api-compatibility.md](./provider-key-sharing/api-compatibility.md).

### Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. The `Authorization: Bearer <token>` header, where `<token>` MUST be either the project API key matching the located project's stored key hash, or a valid JWT issued by Crynux AS for the wallet address that owns the located project.

A request failing either check MUST be rejected with HTTP 401. The project API key and an owner JWT MUST grant the same permission to call the project's private endpoints.

### Raw Task API

The same project authentication MUST protect the Raw Task routes specified in [raw-task-api.md](./raw-task-api.md). Raw Task creation MUST support LLM and Image task types through the shared Task job, call-record, Credits, and usage-statistics lifecycle. Raw Task IDs MUST be local AS job IDs scoped to the authenticated project. Raw requests MUST NOT supply `task_fee`; AS MUST calculate and submit it from the project Cost Level.

Raw batch create and status requests MUST contain between 1 and 100 items and MUST preserve request order. Item validation, insufficient Credits, missing IDs, and cross-project IDs MUST be represented as item errors without failing unrelated valid items.

### Task Execution

The service executes Crynux LLM and Image jobs through the Crynux Bridge raw task APIs. AS parses OpenAI-compatible Crynux LLM requests into canonical `GPTTaskArgs`, submits Bridge ClientTasks, queries ClientTask status, downloads raw `GPTTaskResponse` JSON, and formats public responses locally. For Image jobs, including OpenAI Images generations, OpenAI Images edits, and Raw Image tasks, AS downloads PNG results from Bridge into `llm.task_result_dir` before settle and serves or encodes those files from disk. See [llm-api.md](./llm-api.md) and [raw-task-api.md](./raw-task-api.md).

Configured Provider text models execute through independent Service Provider adapters and a priority scheduler. Provider calls MUST use a Shared API Key of the matched Service Provider, enforce the global per-key concurrency limit, stream output in real time, and settle Key Owner earnings from final normalized usage. Provider execution MUST NOT block the Bridge bounded-batch poller. See [provider-key-sharing/job-execution.md](./provider-key-sharing/job-execution.md).

### Charging

Each LLM call is charged from the Credits balance of the owning account using the Credits billing model in [credits-billing.md](./credits-billing.md). Insufficient available Credits (`balance - locked`) for the pre-forward estimate MUST be rejected with HTTP 402. Accepted jobs MUST soft-lock the estimate until success settle or failure recording releases it.

`GET /v1/llm/billing_config`, `GET /v1/llm/pricing_examples`, and `GET /v1/llm/image_pricing_examples` are JWT-authenticated management APIs specified in [credits-billing.md](./credits-billing.md).

### Call Records

Every task-creating call, successful or failed, MUST be recorded as one `task_call_records` row as specified in [llm-job-processing.md](./llm-job-processing.md). The call record MUST contain the account `user_id` snapshot, project, optional unique `task_job_id`, model, execution source, prompt/completion/total token counts, `token_usage_applicable`, success or failure status, `accepted_at`, `completed_at`, `duration_ms = completed_at - accepted_at`, and the billed effective VRAM when applicable. The call record MUST NOT store the charged Credits amount. Actual Credits changes MUST exist only on processed `credit_events` rows.

`GET /v1/projects/:project_id/requests` is a JWT-authenticated management API. It MUST verify that the project belongs to the authenticated account and is not Deleted. It MUST return one merged list of unfinished project jobs and finished project call records, newest first, with total length at most `llm.project_recent_requests_limit`. The API MUST NOT accept `limit`, `offset`, or any other client-controlled page size. Each item MUST include `id`, `source`, `execution_source`, `created_at`, `model`, `prompt_tokens`, `completion_tokens`, `total_tokens`, `priority_gwei`, `credits`, `billed_vram`, `duration_ms`, `status`, `phase`, and `error_message`. `source` MUST be `job` or `call_record`. `execution_source` MUST be `crynux`, `openai`, `anthropic`, or `google`. `status` MUST be one of `queued`, `in_progress`, `success`, or `failed` mapped as specified in [llm-job-processing.md](./llm-job-processing.md). Crynux rows MUST use the Bridge/AS `phase`; Provider queued rows MUST use `waiting_for_key`; Provider claimed or executing rows MUST use `provider_running`; Provider finished rows MUST return null `phase`. Provider rows MUST return null `billed_vram`. Unfinished rows MUST return null `error_message`; finished failed rows MAY expose a redacted `error_message` from a still-retained `task_jobs` row via `task_job_id`, and MUST NOT require `error_message` on `task_call_records`. In-progress items MUST return null for `prompt_tokens`, `completion_tokens`, `total_tokens`, `duration_ms`, and `credits`. Finished items MUST take `credits` from the processed task-charge event amount, or `0` when no event exists. The `priority_gwei` MUST be the Cost Level decimal integer string stored on the job or call record. The response MUST NOT include `task_fee_gwei`, `median_priority_gwei`, `estimated_node_seconds`, or `vram_weight`. The response field MUST be `requests`.

## Usage Statistics

Finished task-creating calls from OpenAI-compatible and Raw Task endpoints MUST be aggregated for account and project usage views.

Requests MUST count only finished success and failure calls. Authenticated request validation failures, insufficient balance, estimation failures, and task execution failures MUST count as failures. HTTP 401 responses MUST NOT enter usage stats. `GET /responses/:id` and other non-creating GET requests MUST NOT enter usage stats.

Credits in usage stats MUST equal processed `credit_events` rows of type LLM charge referenced by call record ID. Token metrics MUST include only calls with `token_usage_applicable = 1`. Account and project hourly aggregates MUST accumulate Image `image_count` for Image task call records and MUST keep `image_count` at zero for LLM call records.

Project model 10-minute aggregates and Top-10 snapshots MUST key Crynux rows by Relay loaded-model identity `(model_cache_hash, execution_dtype)` together with `execution_source = crynux`, `project_id`, and the period start. Provider rows MUST key by execution source, exact configured model name, project ID, and period start and MUST not use a separate Service Provider identity or a fabricated Relay identity. A Crynux call record MUST store its normalized lowercase repository model for display. A Provider call record MUST preserve its exact case-sensitive configured model name. Aggregates MUST also store `task_type`, `quantization_config`, `components`, and Image `image_count`. Usage stats aggregation MUST NOT apply additional case folding or case-insensitive merging of model names.

Deleting a project MUST set project status to Deleted. Deleted projects MUST NOT appear in list or detail APIs and MUST NOT accept new LLM requests. Existing jobs MUST continue using create-time snapshots. Historical account usage MUST retain Deleted project consumption.

Two background workers MUST run every minute:

1. The base worker advances a cursor over `task_call_records` and updates account hourly, project hourly, project-model 10-minute, and project-duration 10-minute stats by `accepted_at`. For each aggregated call record, the base worker MUST set `projects.last_request_at` to the greater of its current value and that record's `accepted_at` Unix second.
2. The snapshot worker claims dirty projects and rebuilds `1h`, `1d`, and `7d` model Top-10 and completion-duration display histograms from the 10-minute tables. For each claimed project, the snapshot worker MUST also refresh the project's current-Unix-day summary fields on `projects` by summing matching rows from `project_usage_hourly_stats` where `period_start` is in `[UnixDayStart(now), HourStartUnix(now)]`. The refreshed fields MUST be `request_count_day`, `success_count_day`, `failure_count_day`, and `credits_day`. For each claimed project, the snapshot worker MUST also refresh `recent_window_success_count` and `recent_window_failure_count` by summing matching rows from `project_model_usage_10m_stats` over the configured recent-failure window. The window length MUST be `usage_stats.recent_failure_window_seconds`. The window MUST align to Unix 10-minute buckets as `[TenMinuteStartUnix(now) - (buckets - 1) * 600, TenMinuteStartUnix(now) + 600)`, where `buckets = max(1, ceil(window_seconds / 600))`. When `last_request_at` is null or earlier than that window start, both recent-window counts MUST be set to zero without scanning the 10-minute table. Each snapshot tick MUST also refresh a bounded batch of projects that still have non-zero recent-window counts or whose `last_request_at` is at or after the current window start, so sliding-window aging clears stale counts when no new dirty claim occurs. The snapshot worker MUST also clear those day summary fields to zero for a bounded batch of projects whose day counters are non-zero and whose `last_request_at` is null or earlier than `UnixDayStart(now)`.

Configuration:

* `usage_stats.recent_failure_window_seconds` MUST be a positive integer. The default value in the example config is `3600`.
* `usage_stats.recent_failure_rate_threshold` MUST be a positive decimal rate in `(0, 1]`. The default value in the example config is `0.2`.

Management APIs:

* `GET /v1/account/stats?range=1d|7d|1m` MUST return request and Credits series plus totals for `request_count`, `prompt_tokens`, `completion_tokens`, `image_count`, charged Credits, and `success_rate`.
* `GET /v1/projects/:project_id/stats?range=1d|1m` MUST return `request_series`, `credits_series`, and `token_series`. Each `token_series` point MUST include `prompt_tokens`, `completion_tokens`, and `image_count` for that period.
* `GET /v1/projects/:project_id/stats/completion-duration?range=1h|1d|7d`
* `GET /v1/projects/:project_id/stats/models?range=1h|1d|7d`

These APIs MUST read the stats tables and snapshots only. They MUST NOT scan `task_call_records`, MUST use Unix timestamps, and MUST NOT accept a timezone parameter.
