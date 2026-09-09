# Functional Requirements

## Scope

Crynux AS exposes the AI capabilities of the Crynux Network as managed services. It manages user accounts, on-chain ERC20 payments that purchase Credits, projects with private OpenAI-compatible LLM API endpoints, usage-based Credits charging, and per-project usage statistics. Inference itself is executed by the Crynux Network; Crynux AS submits LLM jobs to the Crynux Bridge raw task APIs.

## Wallet Login and Accounts

The wallet address is the user identity. One wallet address maps to exactly one user account.

Login MUST use a wallet signature:

1. The client signs a login message that contains the wallet address and a unix timestamp, using the Ethereum personal sign format.
2. `POST /v1/auth/login` receives the address, timestamp, and signature. The server MUST verify that the recovered signer address equals the submitted address and that the timestamp is within the accepted validity window.
3. On successful verification, the server MUST create the user account if it does not exist, and MUST return a JWT token bound to the wallet address.

All account and project management APIs MUST require a valid JWT token in the `Authorization: Bearer <token>` header. A request without a valid token MUST be rejected with HTTP 401.

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

1. Attribute the transfer to the user account whose wallet address equals the `from` address of the transfer. If no account exists for the `from` address, the service MUST emit a warning log and MUST NOT create a deposit row or Credits ledger event.
2. When the user account exists, record a deposit row identified by network, transaction hash, and log index. This identity MUST be unique; re-scanning the same log MUST NOT create a second deposit or credit the account twice.
3. Convert the token amount to Credits using `credits_per_token`, create a Credits ledger event of type deposit referencing the deposit row ID, and update the account balance.

Deposits and Credits balance changes MUST go through the Credits ledger: every balance change MUST be recorded as a `credit_events` row referencing its source record ID (`ref_id`), and the `credit_accounts` balance MUST equal the sum of its processed events. The event type + `ref_id` pair MUST be unique so one source record produces at most one ledger event.

## Projects and Private LLM API Endpoints

A user account can create multiple projects. Each project has:

* A name.
* A unique `endpoint_token`: a cryptographically random string generated at project creation. The private LLM API base URL of the project is `/api/<endpoint_token>/v1`.
* Exactly one API key: a cryptographically random secret generated at project creation and stored on the project as a key hash and a short public prefix. The plaintext key is shown to the user only once at creation and when reset.
* A `token_ratio` that scales billed tokens relative to consumed tokens. See [llm-api.md](./llm-api.md) for the allowed values, storage format, and charging rules.

Project management APIs:

* `POST /v1/projects`, `GET /v1/projects`, `GET /v1/projects/:project_id`, `PUT /v1/projects/:project_id`, `DELETE /v1/projects/:project_id`.
* `POST /v1/projects/:project_id/api_key/reset`.
* `GET /v1/projects/:project_id/stats`.

`POST /v1/projects` creates the project and its API key together and returns the plaintext API key once. `POST /v1/projects/:project_id/api_key/reset` regenerates the API key for the same project, invalidates the previous secret immediately, and returns the new plaintext once.

A project MUST only be visible to and manageable by its owning account.

## OpenAI-Compatible LLM API

The detailed endpoint, Bridge forwarding, token-ratio, pricing, balance precheck, and Credits settle rules are specified in [llm-api.md](./llm-api.md).

### Endpoints

Each project exposes the following endpoints under its private base URL:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/chat/completions`
* `POST /api/<endpoint_token>/v1/<vram_limit>/completions`
* `GET /api/<endpoint_token>/v1/models`
* `GET /api/<endpoint_token>/v1/models/<model>`

The request and response formats are OpenAI-compatible.

The models endpoints return the shared LLM model catalog built from the in-memory loaded-models cache refreshed from the Relay. Each model object includes the extra field `min_vram`. See [llm-api.md](./llm-api.md).

A request MAY specify a VRAM limit in GB through the `<vram_limit>` URL path segment or the `vram_limit` body field; the path value overrides the body value. The resolved effective VRAM selects `vram_weight` for Credits and task fee and is sent to Bridge as raw task `min_vram`. See [llm-api.md](./llm-api.md) and [credits-billing.md](./credits-billing.md).

### Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. The project API key in the `Authorization: Bearer <api_key>` header, which MUST match the located project's stored key hash.

A request failing either check MUST be rejected with HTTP 401.

### Forwarding to the Crynux Bridge

The service executes LLM jobs through the Crynux Bridge raw task APIs. AS parses OpenAI-compatible requests into canonical `GPTTaskArgs`, submits Bridge ClientTasks, queries ClientTask status, downloads raw `GPTTaskResponse` JSON, and formats public API responses locally. See [llm-api.md](./llm-api.md).

### Charging

Each LLM call is charged from the Credits balance of the owning account using the Credits billing model in [credits-billing.md](./credits-billing.md). Insufficient balance for the pre-forward estimate MUST be rejected with HTTP 402.

`GET /v1/llm/billing_config` and `GET /v1/llm/pricing_examples` are JWT-authenticated management APIs specified in [credits-billing.md](./credits-billing.md).

### Call Records

Every LLM call, successful or failed, MUST be recorded as one `llm_call_records` row containing the account `user_id` snapshot, project, optional unique `llm_job_id`, model, prompt/completion/total token counts, `token_usage_applicable`, success or failure status, charged Credits, `accepted_at`, `completed_at`, `duration_ms = completed_at - accepted_at`, and the billed effective VRAM.

`GET /v1/projects/:project_id/requests` is a JWT-authenticated management API. It MUST verify that the project belongs to the authenticated account and is not Deleted. It MUST return the project's finished `llm_call_records` rows newest first by `id`, including success and failure rows and rows with charged Credits equal to `"0"`. The returned row count MUST equal `llm.project_recent_requests_limit` from configuration when enough rows exist, and MUST be smaller only when fewer rows exist. The API MUST NOT accept `limit`, `offset`, or any other client-controlled page size. Each item MUST include `id`, `created_at`, `model`, `prompt_tokens`, `completion_tokens`, `total_tokens`, `token_ratio`, `credits`, `billed_vram`, `duration_ms`, and `status`. The `token_ratio` MUST be the display float of the project cost level stored on the call record. The response MUST NOT include `task_fee_gwei`, `median_priority_gwei`, `estimated_node_seconds`, or `vram_weight`. The response field MUST be `requests`.

## Usage Statistics

Finished task-creating calls MUST be aggregated for account and project usage views.

Requests MUST count only finished success and failure calls. Authenticated request validation failures, insufficient balance, estimation failures, and task execution failures MUST count as failures. HTTP 401 responses MUST NOT enter usage stats. `GET /responses/:id` and other non-creating GET requests MUST NOT enter usage stats.

Credits in usage stats MUST equal processed `credit_events` rows of type LLM charge referenced by call record ID. Token metrics MUST include only calls with `token_usage_applicable = 1`.

Deleting a project MUST set project status to Deleted. Deleted projects MUST NOT appear in list or detail APIs and MUST NOT accept new LLM requests. Existing jobs MUST continue using create-time snapshots. Historical account usage MUST retain Deleted project consumption.

Two background workers MUST run every minute:

1. The base worker advances a cursor over `llm_call_records` and updates account hourly, project hourly, project-model 10-minute, and project-duration 10-minute stats by `accepted_at`.
2. The snapshot worker claims dirty projects and rebuilds `1h`, `1d`, and `7d` model Top-10 and completion-duration display histograms from the 10-minute tables.

Management APIs:

* `GET /v1/account/stats?range=1d|7d|1m`
* `GET /v1/projects/:project_id/stats?range=1d|1m`
* `GET /v1/projects/:project_id/stats/completion-duration?range=1h|1d|7d`
* `GET /v1/projects/:project_id/stats/models?range=1h|1d|7d`

These APIs MUST read the stats tables and snapshots only. They MUST NOT scan `llm_call_records`, MUST use Unix timestamps, and MUST NOT accept a timezone parameter.
