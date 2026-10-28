# Wallet Settlement

## Purpose

AS MUST expose the existing Relay Wallet protocol so an unmodified `crynux-relay-wallet` deployment can reconstruct available Key Owner earnings and execute CNX withdrawals. The HTTP routes, signatures, and request/response bodies are specified in [Wallet API](wallet-api.md).

The AS Wallet deployment MUST use:

* a dedicated Wallet database;
* a dedicated Wallet API-signing private key;
* a dedicated CNX hot wallet;
* dedicated checkpoints and risk limits;
* a dedicated payout funding pool;
* `relay.api.host` configured to the AS base URL.

The AS Wallet MUST NOT share a signer, database, or hot wallet with the production Relay Wallet.

Only one active AS Wallet process may run. The current protocol has no remote claim token and does not support active-active withdrawal execution.

## Units and Accounts

All earning and withdrawal amounts MUST use CNX wei as decimal integer strings. CNX has 18 decimals.

Each Key Owner MUST have one earning account with:

* available balance;
* withdrawal-locked balance.

The earning account MUST reference the Key Owner by `user_id`. Wallet-facing addresses MUST come from `users.address`; the earning account MUST NOT duplicate the login address.

The earning account and `earning_events` ledger MUST be independent from consumer `credit_accounts` and `credit_events`.

## Earning Event Mapping

The Wallet API MUST map AS earning events to existing Relay account event types:

* `0 TaskIncome`: a committed Key Owner earning; Wallet adds `amount`;
* `2 WithdrawalFeeIncome`: a fulfilled withdrawal fee credited to the configured fee address; Wallet adds `amount`;
* `6 Withdraw`: withdrawal anchor; Wallet log synchronization ignores the amount;
* `7 WithdrawRefund`: rejected-withdrawal refund; Wallet log synchronization ignores the amount.

AS MUST NOT send unknown event types.

AS account semantics are:

* TaskIncome creation: increase available;
* Withdraw creation: decrease available by `amount + fee` and increase withdrawal-locked by the same amount;
* Fulfill: decrease withdrawal-locked by `amount + fee`;
* Reject: decrease withdrawal-locked and restore available by `amount + fee`.

Wallet ignores Withdraw and WithdrawRefund because it deducts `amount + fee` locally only after the chain transaction confirms.

## Withdrawal Creation

The management API MUST first prepare a withdrawal using current network configuration and on-chain Beneficial Address.

The exact user authorization message is:

```text
Crynux Relay
Action: Withdraw <amount> from <address> to <benefit_address> on <network>
Address: <address>
Timestamp: <timestamp>
```

The message MUST preserve capitalization, spaces, line breaks, field order, input address strings, network string, decimal integer amount, and decimal Unix-second timestamp.

The user MUST sign with Ethereum personal-sign semantics:

```text
Keccak256(
  "\x19Ethereum Signed Message:\n"
  + decimal_byte_length(message)
  + message
)
```

AS MUST accept recovery byte `0/1` or `27/28`.

The authenticated JWT address MUST equal the recovered signer.

AS MUST use `users.address` as the canonical requester address when preparing, verifying, and publishing the withdrawal. `earning_withdrawals` MUST store only `user_id`; Wallet-facing responses MUST load the address through that user.

The authorization freshness window at creation MUST accept:

* timestamps no more than 120 seconds old;
* timestamps no more than 60 seconds in the future.

Wallet will validate the signature later without reapplying freshness.

The authorization fingerprint MUST be:

```text
lowercase_hex(SHA256(canonical_message))
```

The fingerprint MUST be unique.

## Beneficial Address

For the requested withdrawal network, AS MUST:

1. load the configured Beneficial Address contract;
2. call `getBenefitAddress(key_owner_address)`;
3. use the Key Owner address when the contract returns the zero address;
4. require a valid EVM result;
5. include the resolved Beneficial Address in the canonical message and withdrawal record.

AS MUST not accept an arbitrary destination from the client.

Wallet independently repeats the same chain lookup and rejects a mismatch.

## Withdrawal Fee

Each withdrawal network MUST configure:

* fixed CNX fee as a decimal string;
* ordered proportional fee tiers;
* maximum withdrawals per day;
* fee-receiver address.

Fee calculation MUST match Relay:

```text
fee = fixed_fee_wei
    + floor(amount * selected_tier_ratio)
```

`fixed_withdrawal_fee_cnx` and each `min_amount_cnx` MUST be non-negative decimal strings with at most 18 fractional digits. Configuration loading MUST convert each value exactly to CNX wei by multiplying by `10^18` and MUST fail on excess precision, invalid syntax, or integer overflow. Floating-point conversion MUST NOT be used.

The selected tier is the highest tier whose converted minimum amount is not greater than the withdrawal amount. The ratio applies to the complete amount. When the amount is below the first tier minimum or the tier list is empty, the proportional ratio MUST be zero.

The fee MUST be fixed at withdrawal creation and returned to Wallet. Wallet MUST NOT recalculate it.

## Withdrawal Creation Transaction

Creation MUST run in one transaction:

1. lock the Key Owner earning account;
2. verify daily limits for requester and Beneficial Address on the selected network;
3. require `available >= amount + fee`;
4. insert a pending withdrawal with original user timestamp and signature;
5. insert a processed Withdraw event for `amount + fee`;
6. store the event ID as `relay_account_event_id`;
7. decrease available and increase withdrawal-locked by `amount + fee`.

The withdrawal MUST not become visible to Wallet until its Withdraw event is processed.

Wallet list, fulfill, and reject HTTP contracts are specified in [Wallet API](wallet-api.md).

## Daily Limits

AS and Wallet MUST use identical per-network maximum withdrawal counts.

Limits apply independently to:

* requester `user_id` and network;
* Beneficial Address and network.

Failed and finished-rejected withdrawals do not count. Day boundaries use UTC because this is a backend risk-control period, not a user display timezone.
