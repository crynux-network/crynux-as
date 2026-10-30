# Provider Key Sharing Data Model

## General Rules

New model structs MUST:

* define `ID`, `CreatedAt`, and `UpdatedAt` explicitly;
* avoid `gorm.Model` and implicit soft delete;
* use named `int8` status enums;
* store monetary values as decimal integer strings through the existing big-integer type;
* store versioned snapshots as canonical JSON in long-text columns;
* use unique source identities for ledger idempotency;
* use indexes aligned with queue and history queries.

Schema migrations MUST use migration-local frozen structs. Existing business history MUST not be backfilled without separate approval.

## Existing Table Changes

### `task_jobs`

Add:

* `execution_source`: `crynux`, `openai`, `anthropic`, or `google`;
* `sortable_priority`: numeric representation of effective priority for database ordering;
* Provider request data in the existing temporary request JSON field;
* a versioned Provider variant in `billing_data`.

Provider jobs MUST store the resolved queue wait in the existing `queue_timeout` column. AS MUST determine queue expiration from `created_at + queue_timeout`. Provider execution MUST NOT add a separate queue-deadline column.

The Provider billing variant MUST contain:

* exact model name;
* usage weights;
* effective priority;
* Credits per Gwei;
* maximum task price;
* estimated usage and Provider units;
* locked Credits.

`task_jobs` MUST not store key plaintext, ciphertext, fingerprint, or authorization headers.

Queue index:

```text
(execution_source, status, sortable_priority, id)
```

Because `priority_gwei` is stored as a large integer string in the current schema, `sortable_priority` MUST preserve arbitrary integer order. Lexicographic string order MUST NOT be used.

### `task_call_records`

Add:

* `execution_source`;
* `shared_api_key_id`, nullable for Crynux calls and Provider calls that fail before Key assignment.

Existing prompt, completion, and total token fields remain the normalized public totals.

Provider call records MUST index:

```text
(shared_api_key_id, id)
```

`shared_api_key_id` MUST identify the last Shared API Key used by the Provider job. It MUST NOT be a foreign key. It MUST remain unchanged after Key deletion and MUST never be exposed to API Consumers. The Key Owner call-history API MAY query call records only through Shared API Key IDs owned by the authenticated user, including soft-deleted Key rows.

Call records MUST not store:

* request or response bodies;
* detailed Provider error bodies;
* charged Credits;
* complete pricing coefficients.

Call records own the normalized public usage totals. `credit_events` own the consumer charge, and `earning_events` own the Key Owner earning.

Usage aggregation identity for Provider rows MUST include execution source and exact model name. The Provider identity is the execution source itself. Provider rows MUST not use a fabricated Relay model-cache hash.

## New Tables

### `shared_api_keys`

One row represents one user-managed Shared API Key and its encrypted secret.

Required fields:

* `id`;
* `public_id`, globally unique and non-sequential;
* `created_at`, `updated_at`;
* `user_id`, indexed;
* `service_provider`;
* `masked_suffix`;
* `owner_enabled`;
* `deleted_at`, nullable;
* `consecutive_unavailable_count`;
* `cooldown_until`, nullable;
* `used_input_tokens`, non-negative integer, default `0`;
* `used_output_tokens`, non-negative integer, default `0`;
* `max_input_tokens`, nullable positive integer; null means unlimited;
* `max_output_tokens`, nullable positive integer; null means unlimited;
* `fingerprint`, nullable after deletion;
* `key_ciphertext`, nullable after deletion;
* `key_nonce`, nullable after deletion;

Unique and lookup indexes:

```text
UNIQUE(public_id)
(user_id, id)
UNIQUE(service_provider, fingerprint)
(service_provider, owner_enabled, deleted_at, cooldown_until, id)
```

Delete MUST atomically set `deleted_at`, set `owner_enabled = false`, and clear `fingerprint`, key ciphertext, and key nonce. The retained row preserves ownership, Provider, masked suffix, and historical call-record lookup. Deleted rows MUST not be listed or selected for execution.

Active slot counts MUST be maintained only in process memory. `consecutive_unavailable_count` MUST count only authentication failure, permission denial, rate limiting, and quota exhaustion. Each qualifying failure MUST atomically update the count before the current job selects another Key. On the third consecutive unavailable result, AS MUST reset the count to zero and set `cooldown_until` using `provider.key_cooldown_seconds`. A successful Provider call MUST clear the count. A failed Key below the threshold remains eligible for later jobs but MUST NOT be selected again by the current job. Cooldown expiry MUST require no database transition.

`used_input_tokens` and `used_output_tokens` MUST accumulate only from successful Provider settlement in the same transaction that creates the billable call record and Key Owner earning. Settlement MUST lock the Shared API Key row, add the settled call's input tokens to `used_input_tokens`, and add its output tokens to `used_output_tokens`. Failed settlement MUST NOT change either counter. Eligibility queries MUST exclude a key when a set `max_input_tokens` satisfies `used_input_tokens >= max_input_tokens` or a set `max_output_tokens` satisfies `used_output_tokens >= max_output_tokens`. Resetting usage MUST set both used counters to zero and MUST NOT change either max.

### `earning_accounts`

One row represents one CNX earning balance.

Required fields:

* `id`;
* `created_at`, `updated_at`;
* `user_id`, unique and not null;
* `available`;
* `withdrawal_locked`.

All balances MUST be non-negative CNX wei integers.

### `earning_events`

One row is the permanent earnings ledger entry and Wallet event source.

Required fields:

* `id`;
* `created_at`, `updated_at`;
* `earning_account_id`, nullable only for `WithdrawalFeeIncome`;
* `address`;
* `event_type`;
* `amount`;
* `source_type`;
* `source_id`;
* `status`;
* `processed_at`, nullable;
* `payload`;

Event types use Wallet-compatible values:

* `0 TaskIncome`;
* `2 WithdrawalFeeIncome`;
* `6 Withdraw`;
* `7 WithdrawRefund`.

A TaskIncome event MUST use the permanent `task_call_records` row as its source. Its `source_type` MUST identify a task call record and `source_id` MUST equal that call record ID.

User-owned events MUST derive the wallet address through `earning_accounts.user_id -> users.address`. A WithdrawalFeeIncome event MUST use the configured fee-receiver address directly and MUST NOT create an `earning_accounts` row.

Status values:

* `processed`: account mutation committed and Wallet event is publishable;
* `invalid`: event MUST never be published.

Unique idempotency constraint:

```text
UNIQUE(event_type, source_type, source_id)
```

Wallet log query index:

```text
(status, id)
```

### `earning_withdrawals`

One row represents one user-authorized payout request.

Required fields:

* `id`;
* `created_at`, `updated_at`;
* `user_id`;
* `benefit_address`;
* `amount`;
* `withdrawal_fee`;
* `network`;
* `status`;
* `earning_event_id`;
* `authorization_timestamp`;
* `authorization_signature`;
* `authorization_fingerprint`;
* `tx_hash`, nullable.

Status values:

* `pending`;
* `success`;
* `failed`.

Unique constraints:

```text
UNIQUE(authorization_fingerprint)
UNIQUE(earning_event_id)
```

Wallet list index:

```text
(status, id)
```

Daily-limit indexes MUST support bounded counts by `(user_id, network, created_at)` and `(benefit_address, network, created_at)`. Wallet-facing requester addresses MUST be loaded through `user_id -> users.address`.

## State Ownership

* `task_jobs` owns temporary execution and Credits hold state.
* `task_call_records` owns permanent consumer-facing call outcome.
* `credit_events` owns API Consumer Credits changes.
* `earning_events` owns Key Owner CNX balance changes and Wallet event publication.
* `earning_withdrawals` owns payout authorization and callback state.
* Relay Wallet owns blockchain transaction signing, broadcast, confirmation, and retry-chain state.

## Transaction Boundaries

The following MUST be atomic:

* Credits reserve and job create;
* successful consumer settlement, Shared API Key usage counter increment, available earning creation, and TaskIncome publication;
* failure record and Credits hold release;
* withdrawal create, available-to-locked transfer, and Withdraw event;
* fulfill, locked release, and fee-income event;
* reject, locked-to-available transfer, and refund event.

External HTTP and blockchain operations MUST not run inside database transactions.

Process-local key slots MUST be reserved before the corresponding job claim. A failed claim transaction MUST release the reserved slot.

## Retention and Deletion

Existing terminal job cleanup remains bounded and deletes only temporary job state.

The following are permanent:

* `task_call_records`;
* `credit_events`;
* `earning_events`;
* `earning_withdrawals`.

Deleting a Shared API Key MUST retain its soft-deleted row and historical references while clearing its encrypted secret fields in the delete transaction.

## Query Requirements

Task call records, earning events, and withdrawals are high-volume tables.

List APIs MUST use keyset pagination by monotonically ordered ID. Large `OFFSET` MUST NOT be used.

Candidate IDs with every membership filter, deterministic order, and `LIMIT` MUST be selected before joins. Queries spanning high-volume tables MUST be checked with MySQL 8.1 `EXPLAIN`.
