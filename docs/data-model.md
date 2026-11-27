# Data Model

All persistent state is stored in one MySQL database. Schema changes are applied by versioned gormigrate migrations in `migrate/` at startup.

Provider Key Sharing table contracts that extend or specialize these rows are specified in [provider-key-sharing/data-model.md](./provider-key-sharing/data-model.md). Task job and call-record ownership during execution is specified in [llm-job-processing.md](./llm-job-processing.md).

| Table | Content |
|-------|---------|
| `users` | Wallet address as the unique user identity |
| `credit_accounts` | One row per user; the current Credits `balance` and soft-lock `locked`; available Credits are `balance - locked` |
| `credit_events` | Append-only Credits ledger; every balance change is one event referencing its source record by `ref_id`; unique by type + `ref_id`; LLM charge amounts are the sole permanent store of charged Credits |
| `deposits` | Detected ERC20 transfers; unique by network + tx hash + log index |
| `referral_codes` | Per-user invite codes; `code` unique; status active or revoked; at most one active code per user in normal operation |
| `referral_bindings` | Permanent invitee-to-referrer bind created at first login; unique by `invitee_user_id` |
| `referral_rewards` | Referrer reward source rows for deposit rewards; unique by `deposit_id`. Each deposit reward stores `prior_purchase_count`, highest applied `tier_min_purchases`, summed `extra_ratio`, and `extra_amount` Credits from tiers. Historical one-time milestone bonus rows may exist with unique `milestone_referrer_user_id`; new milestone bonus rows MUST NOT be created |
| `blockchain_cursors` | Per-network scan cursor; unique by network |
| `projects` | User projects; unique `endpoint_token` forms the private base URL; stores the project API key hash, public prefix, Cost Level mode (`static` or `auto`), static `priority_gwei`, auto `auto_queue_position` and `auto_max_credits_per_task`, `last_request_at`, current-Unix-day usage summary fields (`request_count_day`, `success_count_day`, `failure_count_day`, `credits_day`), and recent-failure-window counts (`recent_window_success_count`, `recent_window_failure_count`) |
| `task_jobs` | Temporary execution rows for OpenAI-compatible and Raw LLM, Image, or Video tasks; stores ownership, execution source, task/API type, source-specific canonical request data, nullable Bridge ID, status, result metadata, versioned create-time billing data, sortable priority, queue timeout, Credits hold fields, and for Image/Video jobs a unique nullable `result_download_hash` used by public result download routes; retained for `llm.job_retention_days` after terminal settle. Provider queue expiration is calculated from `created_at + queue_timeout`; no separate Provider queue-deadline column is stored. Image PNG and Video MP4 results live under `llm.task_result_dir` and MUST NOT be stored in this table |
| `task_call_records` | Permanent finished-call rows with task/job ownership, execution source, exact display model, loaded-model identity (`model_cache_hash`, `dtype`; empty for Provider tasks), Image and Video workload fields, nullable Provider `shared_api_key_id`, applicable usage, status, times, duration, Cost Level, and source-specific billing display fields; MUST NOT store request bodies, result bodies, complete pricing coefficients, or charged Credits amounts. Provider Key identity MUST remain internal and MUST NOT be exposed to API Consumers |
| `usage_stats_pending_call_records` | Temporary queue of finished call-record IDs waiting for base usage-stats aggregation; primary key is `call_record_id`; rows are created in the same transaction as the call record and deleted in the same transaction that applies the bounded aggregation batch |
| `shared_api_keys` | User-owned Shared API Keys with owner state, soft-delete state, cooldown state, fingerprint, AES-256-GCM ciphertext, and nonce. Active slot counts exist only in process memory |
| `earning_accounts` / `earning_events` | Key Owner available and withdrawal-locked CNX balances and the permanent Wallet-compatible earnings ledger |
| `earning_withdrawals` | User-authorized CNX withdrawal requests, event anchors, Wallet callback state, and transaction hashes |
| `account_usage_hourly_stats` | Per-account Unix-hour aggregates of requests, success, failure, tokens, Image `image_count`, Video `video_count`, and charged Credits |
| `project_usage_hourly_stats` | Per-project Unix-hour aggregates with the same metrics plus `user_id` |
| `project_model_usage_10m_stats` | Per-project Unix 10-minute aggregates, unique by `(project_id, model_cache_hash, dtype, model, period_start)`. Crynux rows carry the loaded-model identity; Provider rows store empty `model_cache_hash` and `dtype` with the exact configured model name, without a fabricated identity. Rows store task type, applicable configuration, token totals, `image_count`, and `video_count` |
| `project_duration_usage_10m_stats` | Per-project Unix 10-minute completion-duration small-bucket counts |
| `project_model_usage_snapshots` | Per-project Top-10 model snapshots for `1h`, `1d`, and `7d`, grouped by the same `(model_cache_hash, dtype, model)` identity as the 10-minute table and storing the same counters |
| `project_duration_histogram_snapshots` | Per-project displayable completion-duration histogram snapshots for `1h`, `1d`, and `7d` |
| `usage_stats_progress` | Base aggregation mutex row (`project_id = 0`) and per-project snapshot dirty/claim generations |
