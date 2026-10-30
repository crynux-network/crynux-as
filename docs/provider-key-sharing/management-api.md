# Earn Management API

## Authentication and Response Envelope

Earn management APIs use the existing `/v1` management group and JWT middleware. They are called by the authenticated Key Owner through the Earn WebUI.

The authenticated JWT wallet address identifies the Key Owner. An API MUST select resources by both user ID and resource ID. A foreign resource MUST return the same 404 response as a missing resource.

Routes used by the unmodified Relay Wallet are specified in [Wallet API](wallet-api.md).

Successful responses use:

```json
{
  "message": "success",
  "data": {}
}
```

The API MUST never include Shared API Key plaintext, ciphertext, fingerprint, or authorization headers.

## Error Semantics

The management response layer MUST provide consistent mappings:

* HTTP 400: validation, unsupported Service Provider, unsupported network, invalid signature, invalid state transition;
* HTTP 401: missing or invalid JWT;
* HTTP 402: insufficient available earnings for withdrawal;
* HTTP 404: missing or foreign resource;
* HTTP 409: duplicate Shared API Key fingerprint or conflicting lifecycle update;
* HTTP 429: withdrawal risk limit;
* HTTP 503: Service Provider unavailable during credential validation;
* HTTP 500: unexpected internal failure.

Handlers MUST use shared typed errors and MUST not render ad hoc envelopes.

## Earn Summary

```http
GET /v1/earn
```

Response data:

```json
{
  "address": "0x...",
  "asset": "CNX",
  "decimals": 18,
  "available": "2000000000000000000",
  "withdrawal_locked": "0",
  "enabled_key_count": 2,
  "active_call_count": 1
}
```

All amounts are CNX wei decimal strings.

## Service Provider Configuration

```http
GET /v1/earn/providers
```

Response data is an array of configured Service Providers:

```json
[
  {
    "id": "openai",
    "display_name": "OpenAI",
    "configured_model_count": 3
  }
]
```

This endpoint MUST NOT expose upstream base URLs, timeout values, model usage weights, or pricing configuration.

## Shared API Key List

```http
GET /v1/earn/keys
```

Response item:

```json
{
  "id": "key_public_id",
  "service_provider": "openai",
  "masked_suffix": "abcd",
  "owner_enabled": true,
  "used_input_tokens": 1200,
  "used_output_tokens": 300,
  "max_input_tokens": 1000000,
  "max_output_tokens": null
}
```

The list is account-scoped, excludes soft-deleted Keys, and is ordered by ID descending. Cooldown, concurrency, encryption, and internal health fields MUST NOT be returned. `used_input_tokens` and `used_output_tokens` are cumulative counters since the last usage reset. `max_input_tokens` and `max_output_tokens` are null when that side has no limit.

## Create Shared API Key

```http
POST /v1/earn/keys
```

Request:

```json
{
  "service_provider": "openai",
  "api_key": "secret-value"
}
```

Requirements:

* `service_provider` must be configured;
* `api_key` must be non-empty and within the configured maximum secret length;
* AS must validate the credential before persistence;
* duplicate fingerprint must return 409;
* the new key is owner-enabled and has no active cooldown.

The response returns the list-item shape. It MUST NOT echo `api_key`.

## Enable and Disable

```http
POST /v1/earn/keys/:key_id/enable
POST /v1/earn/keys/:key_id/disable
```

The request body is empty.

Enable and disable are idempotent. Both return the updated list-item shape.

Disable excludes the key from subsequent scheduler and in-worker Key-switch eligibility queries but does not cancel active calls or a claim that already selected the key. Enable does not clear an active cooldown.

## Delete Shared API Key

```http
DELETE /v1/earn/keys/:key_id
```

Delete is idempotent. It soft-deletes and disables the Key, clears its encrypted secret and fingerprint fields, excludes it from later scheduler and in-worker Key-switch eligibility queries, and retains its history and earnings.

Active calls that already loaded key material continue to completion. A deleted Key is omitted from the list and cannot be enabled.

## Set Token Limits

```http
POST /v1/earn/keys/:key_id/limits
```

Request:

```json
{
  "max_input_tokens": 1000000,
  "max_output_tokens": null
}
```

Requirements:

* both fields are required keys in the JSON object;
* each value MUST be either `null` or a positive integer;
* zero and negative values MUST return HTTP 400;
* the update replaces both limits in one request;
* changing a limit takes effect for subsequent eligibility queries immediately.

The response returns the updated list-item shape.

## Reset Usage Counters

```http
POST /v1/earn/keys/:key_id/usage/reset
```

The request body is empty.

Reset sets `used_input_tokens` and `used_output_tokens` to zero and MUST NOT change either max. Reset is idempotent. After reset, the key is eligible again when it is enabled, outside cooldown, and under its limits.

The response returns the updated list-item shape.

## Key Owner Call Records

```http
GET /v1/earn/calls?before_id=<id>&limit=<limit>
```

The endpoint uses descending keyset pagination. `limit` is bounded by configuration.

Response data:

```json
{
  "items": [
    {
      "id": 123,
      "service_provider": "openai",
      "model": "gpt-5.6-sol",
      "shared_api_key_id": "key_public_id",
      "key_masked_suffix": "abcd",
      "status": "success",
      "prompt_tokens": 100,
      "completion_tokens": 20,
      "total_tokens": 120,
      "priority_gwei": "100",
      "earning_wei": "1200000000000",
      "accepted_at": "2026-10-27T08:00:00Z",
      "completed_at": "2026-10-27T08:00:01Z",
      "duration_ms": 1000
    }
  ],
  "next_before_id": 123
}
```

The endpoint MUST select `task_call_records` whose non-null `shared_api_key_id` belongs to the authenticated Key Owner. It MUST use the `(shared_api_key_id, id)` index and descending keyset pagination. It MUST not expose API Consumer identity, project, prompt, result, or upstream error body.

`key_masked_suffix` MUST come from the retained Shared API Key row, including soft-deleted rows.

The endpoint MUST left-join the call record's TaskIncome earning event. Calls without confirmed billable usage MUST return `earning_wei = "0"`.

## Earning Records

```http
GET /v1/earn/events?before_id=<id>&limit=<limit>
```

Response item:

```json
{
  "id": 456,
  "type": "task_income",
  "amount": "1200000000000",
  "status": "available",
  "service_provider": "openai",
  "model": "gpt-5.6-sol",
  "created_at": "2026-10-27T08:00:01Z",
  "processed_at": "2026-10-27T08:00:01Z"
}
```

This API uses keyset pagination and account ownership filtering before joins. It MUST return only Key Owner TaskIncome events. Withdrawal history belongs to the withdrawal endpoint, and WithdrawalFeeIncome is published directly to the configured fee-receiver address.

The public event status MUST be `available` for an internal processed TaskIncome event. Internal invalid events MUST not be returned. `service_provider` and `model` MUST come from the source task call record and are required for every returned item.

## Withdrawal Networks

```http
GET /v1/earn/withdrawal_networks
```

Response data is the configured withdrawal networks:

```json
[
  {
    "id": "base",
    "display_name": "Base",
    "chain_id": 8453,
    "asset": "CNX",
    "decimals": 18,
    "fixed_withdrawal_fee_cnx": "1",
    "withdrawal_min_cnx": "10000"
  }
]
```

`fixed_withdrawal_fee_cnx` and `withdrawal_min_cnx` are CNX decimal strings matching Wallet network configuration. The fee for a network is that fixed value only. The WebUI MUST use these fields to preview the fee, enforce the minimum, and compute the maximum withdrawable amount.

The endpoint MUST not expose contract addresses, fee-receiver addresses, risk limits, or RPC URLs.

## Prepare Withdrawal

```http
POST /v1/earn/withdrawals/prepare
```

Request:

```json
{
  "amount": "1000000000000000000",
  "network": "base"
}
```

AS resolves the Beneficial Address and fee and returns:

```json
{
  "amount": "1000000000000000000",
  "withdrawal_fee": "1000000000000000",
  "total": "1001000000000000000",
  "network": "base",
  "chain_id": 8453,
  "address": "0x...",
  "benefit_address": "0x...",
  "timestamp": 1730000000,
  "message": "Crynux Relay\nAction: Withdraw ...",
  "expires_at": 1730000120
}
```

Prepare does not lock balance or create a withdrawal.

## Create Withdrawal

```http
POST /v1/earn/withdrawals
```

Request:

```json
{
  "amount": "1000000000000000000",
  "network": "base",
  "benefit_address": "0x...",
  "timestamp": 1730000000,
  "signature": "0x..."
}
```

AS MUST recompute the fee, resolve the current on-chain Beneficial Address, reconstruct the exact canonical message, verify the signature and freshness, and atomically lock earnings and create the withdrawal.

The client-supplied Beneficial Address must equal the on-chain result. The client does not supply the fee.

Response data:

```json
{
  "id": 1,
  "amount": "1000000000000000000",
  "withdrawal_fee": "1000000000000000",
  "network": "base",
  "chain_id": 8453,
  "address": "0x...",
  "benefit_address": "0x...",
  "status": "pending",
  "tx_hash": null,
  "created_at": "2026-10-27T08:00:00Z"
}
```

## Withdrawal History

```http
GET /v1/earn/withdrawals?before_id=<id>&limit=<limit>
```

Response statuses:

* `pending`;
* `success`;
* `failed`.

Response data:

```json
{
  "items": [
    {
      "id": 1,
      "amount": "1000000000000000000",
      "withdrawal_fee": "1000000000000000",
      "network": "base",
      "chain_id": 8453,
      "address": "0x...",
      "benefit_address": "0x...",
      "status": "pending",
      "tx_hash": null,
      "created_at": "2026-10-27T08:00:00Z",
      "updated_at": "2026-10-27T08:00:00Z"
    }
  ],
  "next_before_id": 1
}
```

The transaction hash MUST be null until fulfill. The response MUST not expose Wallet-local transaction retries, authorization signatures, or signed raw transactions.

All withdrawal response `address` fields MUST come from `users.address` through the withdrawal's `user_id`; `earning_withdrawals` MUST NOT duplicate the login wallet address.

## OpenAPI

Every management route MUST declare:

* summary;
* authenticated request shape;
* success data shape;
* 400, 401, 402, 404, 409, 429, 500, and 503 responses where applicable.

Secret request fields MUST be marked write-only in OpenAPI and MUST not appear in example responses.
