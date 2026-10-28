# Wallet API

This document is the HTTP contract AS exposes to an unmodified `crynux-relay-wallet` deployment.

These routes do not use JWT. They MUST NOT be called by the Earn WebUI or by a Key Owner session. Account, fee, Beneficial Address, and withdrawal-creation rules remain in [Wallet Settlement](wallet-settlement.md).

The route paths and wire formats are fixed for compatibility with the unmodified Wallet.

## Routes

* `GET /v1/relay_account/event_logs`
* `GET /v1/withdraw/list`
* `POST /v1/withdraw/:id/fulfill`
* `POST /v1/withdraw/:id/reject`

## Authentication

Every Wallet API request MUST use the existing Relay Wallet signature algorithm.

For a business value `v` and Unix-second timestamp `t`:

1. JSON-marshal `v`;
2. decode and marshal it again as a generic JSON value;
3. concatenate the canonical JSON bytes and decimal timestamp with no delimiter;
4. hash with Keccak-256;
5. sign the hash with secp256k1;
6. encode `R || S || V` as a `0x`-prefixed 65-byte hex string, with `V` equal to `0` or `1`.

This signature is not Ethereum `personal_sign`.

AS MUST require:

```text
abs(server_unix_seconds - timestamp) <= 60
```

The recovered signer MUST equal the configured AS Wallet signer address using the same address representation expected by the existing Wallet protocol.

## Event Log API

### Request

```http
GET /v1/relay_account/event_logs
```

Query fields:

* `start_id`
* `limit`
* `timestamp`
* `signature`

The signed business object is:

```json
{"limit":100,"start_id":123}
```

### Response

```json
{
  "message": "success",
  "data": [
    {
      "id": 124,
      "created_at": 1730000000,
      "address": "0x...",
      "amount": "1000000000000000000",
      "type": 0,
      "payload": "{}"
    }
  ]
}
```

Rules:

* return rows with `id > start_id`;
* order by `id ASC`;
* apply the requested bounded limit;
* encode `created_at` as Unix seconds;
* encode `amount` as a decimal integer string;
* encode `payload` as a JSON string; Provider Key Sharing events use `"{}"`;
* return only processed events;
* stop before the first unprocessed event in ID order;
* never expose a later event while an earlier existing event remains hidden.

Event IDs MUST be unique and monotonically increasing. Database allocation gaps are allowed, but published ordering and visibility MUST be stable.

Event type numbers and account effects are specified in [Wallet Settlement](wallet-settlement.md).

## Withdrawal List API

### Request

```http
GET /v1/withdraw/list
```

It uses the same `start_id`, `limit`, timestamp, and signature rules as the event log API.

### Response item

```json
{
  "id": 1,
  "created_at": 1730000000,
  "address": "0x...",
  "benefit_address": "0x...",
  "amount": "1000000000000000000",
  "withdrawal_fee": "1000000000000000",
  "network": "base",
  "status": 0,
  "relay_account_event_id": 124,
  "timestamp": 1730000000,
  "signature": "0x..."
}
```

Rules:

* return only pending withdrawals;
* return `id > start_id`, ordered by ID ascending, within limit;
* load the requester address through `earning_withdrawals.user_id -> users.address`;
* preserve the original user timestamp and signature;
* expose a withdrawal only after its `relay_account_event_id` is visible in event logs;
* keep every published record immutable.

Wallet will accept a withdrawal only when:

```text
relay_account_event_id <= LatestTaskFeeLogID
```

## Fulfill API

```http
POST /v1/withdraw/:id/fulfill
```

Request:

```json
{
  "id": 1,
  "tx_hash": "0x...",
  "timestamp": 1730000000,
  "signature": "0x..."
}
```

The signed business object contains `id` and `tx_hash`. Path ID and body ID MUST match.

Fulfill MUST:

* allow only Pending to Success;
* store the first transaction hash;
* decrease withdrawal-locked by `amount + fee`;
* create WithdrawalFeeIncome exactly once with the configured fee-receiver address when fee is positive, without creating or updating an earning account for that address;
* return success for repeated fulfill of an already successful record;
* reject fulfill of a failed record.

A repeated fulfill MUST NOT replace the stored transaction hash.

## Reject API

```http
POST /v1/withdraw/:id/reject
```

Request:

```json
{
  "id": 1,
  "timestamp": 1730000000,
  "signature": "0x..."
}
```

Reject MUST:

* allow only Pending to Failed;
* atomically create one WithdrawRefund event for `amount + fee`;
* decrease withdrawal-locked and restore available;
* return success for repeated reject of an already failed record;
* reject a successful record.

## Callback Safety

AS MUST rely on Wallet's persisted broadcast and confirmation state. AS MUST NOT automatically refund a pending withdrawal based on elapsed time.

Once Wallet has persisted broadcast evidence, authorization defects or callback delay cannot justify rejection.

Fulfill and reject are individually idempotent but mutually exclusive.

## OpenAPI

Every Wallet route MUST declare:

* summary;
* Wallet-signed request shape;
* success data shape;
* authentication and validation error responses used by the existing Relay Wallet protocol.

Secret and signer fields MUST not appear in example responses.
