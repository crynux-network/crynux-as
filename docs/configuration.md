# Configuration

The complete YAML template is [config/config.example.yml](../config/config.example.yml). Default values MUST be declared in YAML templates only. Configuration loading MUST fail when a required item is missing or invalid. Domain-specific config contracts that already live in other specs remain authoritative there; this document indexes those fields and states the shared rules that do not belong elsewhere.

## HTTP

* `http.host` and `http.port` bind the HTTP server.
* `http.max_body_bytes` is the maximum accepted request body size.
* `http.public_origin` is the absolute public AS origin used when AS builds Image and Video public result download URLs. It MUST use scheme `http` or `https`, MUST include a host, and MUST NOT include a path, query, fragment, or userinfo. Trailing slashes MUST be stripped at load time.
* `http.jwt` and `http.turnstile` configure management-API authentication. Outside the test environment, JWT and Turnstile secrets MUST be present.

## Database and Logging

* `db.driver` and `db.connection` select MySQL. A MySQL connection string MUST include `collation=utf8mb4_unicode_ci` and MUST NOT include `charset=`.
* `db.log` and top-level `log` configure structured file logging.

## Blockchains

The configuration defines a map of blockchain networks under `blockchains.<network-name>`. Each network defines a map of supported ERC20 tokens under `tokens`.

Each network gets exactly one blockchain client, one scanning worker, and one `blockchain_cursors` row keyed by the network name. The worker polls on `scan_interval` seconds. The worker MUST scan only blocks at or below `latest - confirmation_blocks`; `confirmation_blocks` of `0` means scan up to the latest block. Credits for a deposit are computed as `amount * credits_per_token / 10^decimals` using integer arithmetic.

Deposit scan and conversion rules are specified in [deposit-processing.md](./deposit-processing.md).

## Bridge, Relay, and Hugging Face

* `bridge.base_url` and `bridge.api_key` / `bridge.api_key_file` configure the platform Bridge client.
* `relay.base_url` is the public Relay URL for loaded-models, queued-priority, and execution-time fetches.
* `huggingface.api_base_url`, `huggingface.timeout_seconds`, and `huggingface.cache_ttl_seconds` configure create-time Hub model validation. Hub rules are specified in [llm-api.md](./llm-api.md) and [raw-task-api.md](./raw-task-api.md).

## Task Schema

`task_schema.stable_diffusion_inference`, `task_schema.gpt_inference`, `task_schema.stable_diffusion_finetune_lora`, and `task_schema.video_inference` MUST be present schema URLs used to validate Raw TaskArgs before Credits reservation.

## LLM and Task Runtime

Shared LLM and Raw Task runtime fields, including refresh intervals, Cost Level bounds, `task_result_dir`, `job_retention_days`, and `max_crynux_task_args_bytes`, are specified in [llm-api.md](./llm-api.md), [raw-task-api.md](./raw-task-api.md), and [llm-job-processing.md](./llm-job-processing.md).

Credits billing fields such as `credits_per_gwei`, `max_task_price_cnx`, and `min_task_price_cnx` are specified in [credits-billing.md](./credits-billing.md).

## Credits and Referral

* `credits.signup_bonus` MUST be present and MUST be a non-negative integer; `0` disables the signup bonus.
* `credits.referral.deposit_reward_ratio`, `credits.referral.invitee_bonus`, and `credits.referral.purchase_bonus_tiers` MUST be present.
* `deposit_reward_ratio` MUST parse as a non-negative decimal less than or equal to `1`.
* `invitee_bonus` MUST be a non-negative integer; `0` disables the invitee signup bonus.
* `purchase_bonus_tiers` MUST be a list and MAY be empty; each tier MUST define positive unique `min_purchases` and a non-negative `extra_ratio` less than or equal to `1`, and `deposit_reward_ratio` plus all tier `extra_ratio` values MUST be less than or equal to `1`.

Referral deposit reward application is specified in [deposit-processing.md](./deposit-processing.md).

## Usage Stats

`usage_stats.recent_failure_window_seconds` and `usage_stats.recent_failure_rate_threshold` configure the project recent-failure-window counts and `elevated_recent_failure_rate` flag returned by project list and detail APIs.

## Provider Key Sharing

Provider concurrency, scheduler, encryption, services file, and Wallet network fields under `provider` are specified in [provider-key-sharing/](./provider-key-sharing/).
