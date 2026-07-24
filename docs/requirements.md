# Functional Requirements

## Scope

Crynux AS exposes the AI capabilities of the Crynux Network as managed services. It manages user accounts, on-chain ERC20 payments that purchase Credits, projects with private OpenAI-compatible LLM API endpoints, usage-based Credits charging, and per-project usage statistics. Inference itself is executed by the Crynux Network; Crynux AS forwards LLM requests to the Crynux Bridge.

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
* `GET /api/<endpoint_token>/v1/models`

The request and response formats are OpenAI-compatible.

### Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. The project API key in the `Authorization: Bearer <api_key>` header, which MUST match the located project's stored key hash.

A request failing either check MUST be rejected with HTTP 401.

### Forwarding to the Crynux Bridge

The service forwards LLM requests to the Crynux Bridge LLM API. Streaming requests MUST inject `stream_options.include_usage: true` toward the Bridge for metering and MUST NOT expose the injected usage chunk to clients that did not request it. See [llm-api.md](./llm-api.md).

### Charging

Each LLM call is charged from the Credits balance of the owning account using Bridge `usage` token counts, the project `token_ratio`, and the configured global unit prices. Insufficient balance for the pre-forward estimate MUST be rejected with HTTP 402. See [llm-api.md](./llm-api.md).

### Call Records

Every LLM call, successful or failed, MUST be recorded as one `llm_call_records` row containing the project, model, prompt/completion/total token counts, success or failure status, charged Credits, and call duration.

## Usage Statistics

A background stats task MUST periodically aggregate `llm_call_records` into `project_usage_stats` rows keyed by project and time period, containing call count, success count, failure count, token counts, and charged Credits.

`GET /v1/projects/:project_id/stats` returns the aggregated stats of a project within the requested time range.
