# Provider Key Sharing Requirements

## Identity and Ownership

1. A Key Owner MUST be an existing Crynux AS user identified by the wallet address used for AS login.
2. The same user MAY own projects, consume Credits, upload Shared API Keys, and earn CNX.
3. A Shared API Key MUST belong to exactly one Key Owner and one Service Provider.
4. Shared API Keys MUST be account-level resources. They MUST NOT belong to a project.
5. Every active Shared API Key of a Service Provider MUST be eligible for every configured model of that Service Provider.
6. The system MUST NOT maintain a per-key model allowlist.
7. Provider calls MAY use a key owned by the API Consumer or by another user. Key selection MUST NOT prefer the API Consumer's own key.

## Service Providers and Models

1. The first release MUST support OpenAI, Anthropic, and Google adapters.
2. Provider models MUST be declared explicitly in YAML configuration.
3. A Provider model declaration MUST identify the Service Provider, official API model name, and usage weights. It MUST NOT contain an API capability list.
4. The configuration MUST be the only authority for which Provider models exist and for their pricing. Runtime catalog visibility additionally requires Shared API Key availability as specified below.
5. An upstream model-list response MUST NOT add, remove, rename, or reprice an AS model.
6. A configured Provider model MUST enter the public model catalog only while its Service Provider has at least one enabled, non-deleted Shared API Key.
7. Busy keys and temporary cooldown MUST NOT remove models from the catalog.
8. A Provider model name MUST be globally unique among Provider models and MUST NOT collide with a Crynux model name.
9. AS MUST expose and forward the official model name without adding a Service Provider prefix or alias.
10. Every configured Provider model MUST support the AS Chat Completions and Responses APIs through its Service Provider adapter. Provider models MUST reject Completions.

## Key Lifecycle

1. A user MUST be able to create, list, enable, disable, and delete a Shared API Key.
2. A newly created Shared API Key MUST be enabled.
3. Disable MUST be reversible and MUST preserve the encrypted key.
4. Disable MUST exclude the key from subsequent scheduler and in-worker Key-switch eligibility queries. A claim that already selected the key MAY continue.
5. Disable MUST NOT cancel, interrupt, or change the settlement of a job that already claimed the key.
6. Enable MUST restore scheduling eligibility when the key is outside cooldown.
7. Delete MUST soft-delete the Key, disable it, clear its encrypted secret fields, and exclude it from subsequent eligibility queries. A claim that already loaded the key material MAY continue.
8. Delete MUST preserve the Key row, historical call records, earnings, and withdrawals.
9. The API MUST never return an uploaded secret after accepting it.
10. Enabling a key MUST NOT clear an active cooldown.

## Concurrency and Queueing

Provider queueing is a core execution and pricing requirement. Each Shared API Key has bounded concurrency, so temporary slot saturation is expected during normal operation even when another slot will become available shortly. Accepted Provider jobs MUST be able to wait for the next eligible slot instead of failing immediately. This wait allows multiple Shared API Keys of the same Service Provider to share demand efficiently and avoids exposing short-lived key occupancy as client-facing request failures.

The queue is also the demand source for Provider pricing. Effective Cost Level MUST determine queue order, so an API Consumer can pay a higher task price for earlier assignment or a lower task price while accepting a longer wait. Provider queue priority bounds MUST supply the Provider-side inputs for Auto Cost Level. Removing the queue or assigning jobs without effective Cost Level order would remove the market mechanism that connects current Provider demand, assignment order, and task price.

1. Every Shared API Key MUST use the same positive global `provider.max_concurrency_per_key` configuration value.
2. Key Owners MUST NOT configure concurrency.
3. Provider jobs MUST wait in a durable database queue when every eligible key is at its concurrency limit.
4. Provider jobs MUST be ordered by effective Cost Level descending and job ID ascending.
5. The scheduler MUST reserve one process-local concurrency slot before the database claim transaction. That transaction MUST claim the job. A failed transaction MUST release the reserved slot.
6. A queued job MUST NOT allocate a goroutine before a key slot is claimed.
7. A job whose queue timeout expires before claim MUST fail without an upstream request, release its Credits hold, and create no Key Owner earning.
8. After a definitely unaccepted key-specific failure, the Provider job goroutine MUST record the failed Key's unavailable count and switch to another eligible Key. The same job MUST NOT select the failed Key again.
9. Per-key active slot counts MUST exist only in process memory. The database MUST NOT persist or reconcile active slot counters.
10. The scheduler MUST query enabled, non-deleted keys whose cooldown has expired, then exclude keys with no available in-memory slot and keys already tried by the current job. It MUST select one remaining key uniformly at random. It MUST NOT persist fairness counters or last-assigned state.
11. Authentication failure, permission denial, rate limiting, and quota exhaustion MUST atomically increment the selected Key's consecutive unavailable count before another Key is selected. Provider 5xx responses, network errors, and timeouts MUST NOT increment it or cause a Key switch.
12. A successful Provider call MUST clear the key's consecutive unavailable count.
13. On the third consecutive unavailable result, AS MUST reset the count to zero and set `cooldown_until` to the current time plus `provider.key_cooldown_seconds`.
14. Cooldown expiry MUST make the key eligible without a background transition worker.
15. A failed Key that has not reached cooldown MAY be selected by a later job.

## API Consumer Charging

1. Provider calls MUST use the existing project authentication and project ownership rules.
2. API Consumers MUST pay Credits from the user account that owns the calling project.
3. Provider job creation MUST estimate an upper-bound charge, reserve available Credits, and create the job in one transaction.
4. Any Provider call with confirmed billable upstream usage MUST produce a valid final public result, charge the API Consumer, and create the Key Owner earning from the job's immutable pricing snapshot. Safety stops, max-token truncation, empty assistant content, and tool-only output remain valid public results.
5. Credits charge, token usage recording, and Key Owner earning MUST be determined only by whether the Service Provider completed the request. When the Service Provider completes the request, settlement MUST complete. API Consumer disconnect, a detached or absent subscriber, and any other public-delivery outcome MUST NOT cancel, defer, or change that settlement.
6. A failed call that has no confirmed billable upstream usage MUST release the Credits hold and MUST NOT charge the API Consumer or create a Key Owner earning.
7. A call with missing or unverifiable final usage MUST fail without a consumer charge or Key Owner earning and MUST be recorded for operator investigation.
8. Charging retries MUST NOT create duplicate call records, Credits events, balance changes, earnings, or hold releases.

Confirmed usage is settled only as a completed job with a success call record. AS MUST NOT charge a failed job.

## Key Owner Earnings

1. Key Owner earnings MUST be denominated in CNX wei.
2. The Key Owner whose Shared API Key is recorded on the Provider call record MUST own the earning.
3. The earning MUST equal the complete CNX-equivalent task fee calculated from actual usage and effective Cost Level.
4. AS MUST NOT apply an additional Provider-specific exchange rate or revenue-share percentage.
5. Platform economics MUST continue to derive from `credits_per_token` and `credits_per_gwei`.
6. A committed earning MUST immediately increase the Key Owner's available balance and become publishable as TaskIncome.
7. Disabling or deleting a Key MUST NOT cancel earnings already created from that Key.
8. One billable Provider call record MUST create at most one earning source.

## Withdrawals

1. Key Owners MUST be able to withdraw available CNX through the Earn management API.
2. Withdrawal-locked earnings MUST NOT be withdrawable again.
3. Withdrawal authorization MUST use the exact canonical message expected by Relay Wallet.
4. The destination MUST be the Beneficial Address resolved from the configured on-chain contract for the Key Owner's login wallet.
5. AS and Wallet MUST independently verify the Beneficial Address.
6. Withdrawal creation MUST atomically reserve `amount + withdrawal_fee`, create the withdrawal record, and create its anchor event.
7. A fulfilled withdrawal MUST be idempotent and MUST preserve the first recorded transaction hash.
8. A rejected withdrawal MUST atomically restore `amount + withdrawal_fee` exactly once.
9. AS MUST NOT automatically reject or refund a withdrawal merely because a callback is delayed.
10. One active AS Wallet deployment MUST process all AS withdrawals.

## Security and Privacy

1. Shared API Keys MUST be encrypted with authenticated encryption before database storage.
2. The database MUST never contain a plaintext Shared API Key or encryption key.
3. Logs, traces, metrics, errors, call records, job records, and API responses MUST NOT contain Shared API Keys or authorization headers.
4. Plaintext key material MUST exist only during upload validation, encryption, decryption, and the upstream request setup.
5. The encryption design MUST protect against database-only disclosure.
6. The documentation and implementation MUST NOT claim that compiled code or process isolation makes keys impossible to extract after arbitrary code execution as the AS process user or host administrator.
7. Production deployment MUST run AS as a dedicated non-root user with core dumps and ptrace capabilities disabled.

## Operational and Compliance Requirements

1. AS MUST run as one process. This feature MUST NOT require cross-instance coordination.
2. All required configuration MUST be explicit in every YAML template. Go code MUST NOT supply hidden defaults.
3. Operators MUST verify that each enabled Service Provider permits the intended API-key sharing, resale, and multi-user use before enabling that Service Provider in production.
