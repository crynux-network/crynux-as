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

The set of supported blockchain networks and the supported ERC20 tokens on each network are defined in the service configuration. Each network configuration MUST define the chain ID, RPC endpoint, request rate limit, scan start block, log scan block range, the receiving address, and the contract address and decimals of every supported token.

### Deposit Detection

For each configured blockchain network, the service MUST run a scanning worker that:

1. Maintains a per-network scan cursor (`blockchain_cursors`) recording the last processed block number.
2. Fetches ERC20 `Transfer(address,address,uint256)` logs of all configured token contracts on that network where the `to` address is the receiving address, in block ranges no larger than the configured `log_block_range`.
3. Rate-limits all RPC requests to the configured RPS.
4. Advances the cursor only after all logs in the scanned range have been durably recorded.

### Crediting Rules

For each detected transfer log, the service MUST:

1. Record a deposit row identified by network, transaction hash, and log index. This identity MUST be unique; re-scanning the same log MUST NOT create a second deposit or credit the account twice.
2. Attribute the deposit to the user account whose wallet address equals the `from` address of the transfer. If no account exists for the `from` address, the deposit MUST be recorded and MUST NOT be credited to any account.
3. Convert the token amount to Credits using the configured conversion for that token, create a Credits ledger event of type deposit, and update the account balance.

Deposits and Credits balance changes MUST go through the Credits ledger: every balance change MUST be recorded as a `credit_events` row, and the `credit_accounts` balance MUST equal the sum of its processed events.

## Projects and Private LLM API Endpoints

A user account can create multiple projects. Each project has:

* A name.
* A unique `endpoint_token`: a cryptographically random string generated at project creation. The private LLM API base URL of the project is `/api/<endpoint_token>/v1`.
* Project API keys: each key is a cryptographically random secret shown to the user only once at creation. The server MUST store only the key hash and a short public prefix.

Project management APIs:

* `POST /v1/projects`, `GET /v1/projects`, `GET /v1/projects/:project_id`, `PUT /v1/projects/:project_id`, `DELETE /v1/projects/:project_id`.
* `POST /v1/projects/:project_id/api_keys`, `GET /v1/projects/:project_id/api_keys`, `DELETE /v1/projects/:project_id/api_keys/:api_key_id`.
* `GET /v1/projects/:project_id/stats`.

A project MUST only be visible to and manageable by its owning account.

## OpenAI-Compatible LLM API

### Endpoints

Each project exposes the following endpoints under its private base URL:

* `POST /api/<endpoint_token>/v1/chat/completions`
* `POST /api/<endpoint_token>/v1/completions`
* `GET /api/<endpoint_token>/v1/models`

The request and response formats are OpenAI-compatible.

### Authentication

A request to a private LLM endpoint MUST be authenticated by both:

1. The `endpoint_token` in the URL, which locates the project.
2. A project API key in the `Authorization: Bearer <api_key>` header, which MUST belong to the located project and MUST be active.

A request failing either check MUST be rejected with HTTP 401.

### Forwarding to the Crynux Bridge

The service forwards LLM requests to the Crynux Bridge LLM API:

* Bridge endpoints: `/v1/llm/chat/completions` and `/v1/llm/completions`.
* Bridge authentication: the platform-level Bridge API key from the service configuration, sent as `Authorization: Bearer <api_key>`. The Bridge API key MUST have the `chat` role.

For streaming requests, the Bridge returns the result as server-sent events emitted after the task completes, and includes the `usage` payload in the final chunk only when the request contains `stream_options.include_usage: true`. When forwarding a streaming request, the service MUST inject `stream_options.include_usage: true` into the request sent to the Bridge to guarantee usage metering, and MUST return the streamed response to the client in the shape the client requested: the injected usage chunk MUST NOT be exposed to a client that did not request `include_usage`.

### Charging

Each LLM call is charged from the Credits balance of the owning account:

1. The charge amount is calculated from the `usage` field of the Bridge response (`prompt_tokens`, `completion_tokens`, `total_tokens`) and the configured per-model unit prices.
2. Each successful call MUST create a Credits ledger event of type LLM charge and decrease the account balance.
3. If the account balance is insufficient for the request, the service MUST reject the request with HTTP 402 before forwarding it to the Bridge.

### Call Records

Every LLM call, successful or failed, MUST be recorded as one `llm_call_records` row containing the project, API key, model, prompt/completion/total token counts, success or failure status, charged Credits, and call duration.

## Usage Statistics

A background stats task MUST periodically aggregate `llm_call_records` into `project_usage_stats` rows keyed by project and time period, containing call count, success count, failure count, token counts, and charged Credits.

`GET /v1/projects/:project_id/stats` returns the aggregated stats of a project within the requested time range.
