# Deposit Processing

## Scope

This document specifies how Crynux AS detects ERC20 deposits on configured blockchain networks and credits user Accounts through the Credits ledger. The term `network` means blockchain network.

## Worker Lifecycle

For each network entry under `blockchains` in the service configuration, the service MUST start exactly one blockchain processor goroutine.

Each processor MUST:

1. Read the network `scan_interval` (seconds) from configuration.
2. Tick on that interval for the lifetime of the process.
3. On each tick, process the next unprocessed block range for that network.
4. Stop when the process context is cancelled.

A tick that fails because of an RPC or database error MUST log the error, MUST NOT advance the scan cursor, and MUST retry on the next tick.

## Configuration

Each network configuration MUST define:

| Field | Meaning |
|-------|---------|
| `chain_id` | EVM chain ID |
| `rpc_endpoint` | JSON-RPC endpoint |
| `rps` | Maximum RPC requests per second for this network |
| `start_block_num` | Initial cursor value when no `blockchain_cursors` row exists |
| `log_block_range` | Maximum number of blocks in one `eth_getLogs` query |
| `scan_interval` | Seconds between deposit scan ticks |
| `confirmation_blocks` | Number of blocks behind the latest head that MUST exist before a block is scanned; `0` means scan up to the latest block |
| `receiving_address` | Platform address that receives ERC20 deposits |
| `tokens` | Map of supported ERC20 tokens on this network |

Each token configuration MUST define:

| Field | Meaning |
|-------|---------|
| `address` | ERC20 contract address |
| `decimals` | Token decimals |
| `credits_per_token` | Credits awarded for one whole token (`10^decimals` raw units) |

Configuration loading MUST fail when `scan_interval` is zero or when `credits_per_token` is zero for any token.

## Scan Cursor

The processor MUST persist progress in `blockchain_cursors`, keyed by network name.

On each successful tick:

1. Load the cursor with `GetBlockchainCursor`, creating the row with `last_block_num = start_block_num` when absent.
2. Read the latest block number through the network blockchain client (RPS-limited).
3. When `latest < confirmation_blocks`, skip the tick.
4. Set `confirmed_tip = latest - confirmation_blocks`.
5. When `last_block_num >= confirmed_tip`, skip the tick.
6. Otherwise scan the inclusive range `[last_block_num + 1, min(confirmed_tip, last_block_num + log_block_range)]`.
7. Advance `last_block_num` to the end of that range only after every log in the range has been handled.

Handled means either credited into the ledger or deliberately ignored according to the rules below. A failure while handling any log MUST leave the cursor unchanged.

## Log Filter

For each scanned range the processor MUST call `FilterERC20TransferLogs` with:

* All configured token contract addresses on that network.
* Topic filter for `Transfer(address,address,uint256)`.
* Indexed `to` address equal to the network `receiving_address`.
* Block range within `log_block_range`.

Every RPC request MUST pass through the network RPS limiter.

## Log Parsing

For each returned log the processor MUST:

1. Map the log contract address to a configured token name. An unknown contract MUST be ignored with a warning log.
2. Require exactly three topics and topic0 equal to the ERC20 `Transfer` topic. Otherwise ignore with a warning log.
3. Parse the token amount from log data. A non-positive amount MUST be ignored with a warning log.
4. Parse the `from` address from topic1. A zero address MUST be ignored with a warning log.
5. Normalize `from` with `common.HexToAddress(from).Hex()` so it matches the address stored at wallet login.

## Token to Credits Conversion

Credits MUST be computed with integer arithmetic:

```text
credits = amount * credits_per_token / 10^decimals
```

where `amount` is the raw ERC20 transfer amount.

When the computed Credits is less than `1` (integer division truncates any positive result below one whole Credit to `0`), the processor MUST emit a warning log that includes network, transaction hash, log index, from address, token name, and raw amount, and MUST create neither a `deposits` row nor a `credit_events` row. The scan cursor MUST still advance past the ignored log after the range completes successfully.

## Unknown Sender

When no `users` row exists for the normalized `from` address, the processor MUST:

1. Emit a warning log that includes network, transaction hash, log index, from address, token name, and raw amount.
2. Create neither a `deposits` row nor a `credit_events` row.
3. Continue processing the remaining logs in the range.

The scan cursor MUST still advance past ignored logs after the range completes successfully.

## Deposit Crediting

When a user exists for the normalized `from` address and the computed Credits is at least `1`, the processor MUST apply the deposit in one database transaction:

1. Insert a `deposits` row with network, token name, transaction hash, log index, from address, raw amount, computed Credits, `user_id`, and status `Processed`.
2. Insert a `credit_events` row with type deposit, `ref_id` equal to the deposit ID, `user_id`, amount equal to the deposit Credits, and status `Processed`.
3. Load the user's `credit_accounts` row with `SELECT ... FOR UPDATE`, then increase `credit_accounts.balance` by the deposit Credits.

The deposit identity `(network, tx_hash, log_index)` MUST be unique. Re-inserting an already recorded deposit MUST be treated as success and MUST NOT create a second ledger event or increase the balance again.

The credit event identity `(type, ref_id)` MUST be unique so one deposit produces at most one ledger event.

Deposit insert, credit event insert, and balance update MUST commit atomically. A failure of any step MUST roll back the entire transaction and MUST prevent cursor advancement for that range.

## Account APIs

* `GET /v1/account` MUST return the Credits balance of the authenticated wallet.
* `GET /v1/account/purchases` MUST return the purchase rows of the authenticated wallet, newest first, with offset/limit pagination. The response field MUST be `purchases`.
* `GET /v1/account/charges` MUST return the charged LLM call records of the authenticated wallet, newest first, with offset/limit pagination. A charged record is an `llm_call_records` row owned by the wallet through its projects where `credits` is not `"0"`. Each item MUST include `id`, `created_at`, `project_id`, `model`, `prompt_tokens`, `completion_tokens`, `total_tokens`, `token_ratio`, `credits`, `billed_vram`, `duration_ms`, and `status`. The `token_ratio` MUST be the display float of the project cost level stored on the call record at charge time.

## Purchase Configuration API

`GET /v1/purchase/networks` is a management API that requires a valid JWT token. It MUST return the configured purchase networks and tokens for client Credits purchase flows. Each network entry MUST include `name`, `chain_id`, `receiving_address`, and `tokens`. Each token entry MUST include `name`, `address`, `decimals`, and `credits_per_token`. The response MUST NOT include `rpc_endpoint`, RPS, scan interval, log block range, confirmation blocks, or scan cursor settings. Networks and tokens MUST be sorted by name ascending.
