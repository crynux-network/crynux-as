# Provider Key Sharing Operations

## Deployment Topology

Production MUST run:

* exactly one Crynux AS process;
* exactly one active AS Wallet process;
* one AS MySQL database;
* one separate AS Wallet database;
* one dedicated AS Wallet API signer;
* one dedicated CNX payout hot wallet.

AS MUST NOT use Redis, NATS, or another stream broker for this feature. Real Provider streams are process-local.

## Configuration

All Provider Key Sharing values MUST be explicit in every YAML template. Missing, empty, zero, duplicate, or out-of-range required values MUST fail startup. Go code MUST NOT supply default values.

The configuration shape is:

```yaml
provider:
  max_concurrency_per_key: 2
  scheduler_interval_milliseconds: 100
  scheduler_batch_size: 100
  default_queue_timeout_seconds: 600
  max_attempts_per_job: 2
  key_cooldown_seconds: 3600
  stream_subscriber_buffer: 128
  shutdown_timeout_seconds: 600
  max_secret_bytes: 4096
  key_encryption:
    encryption_key_file: "config/secrets/provider_key_encryption.txt"
  services:
    openai:
      display_name: "OpenAI"
      base_url: "https://api.openai.com"
      connect_timeout_seconds: 10
      response_header_timeout_seconds: 60
      stream_idle_timeout_seconds: 120
      request_timeout_seconds: 600
      credential_validation_timeout_seconds: 10
      models:
        - name: "gpt-5.6-sol"
          usage_weights:
            input_tokens: "1"
            output_tokens: "4"
            cached_input_tokens: "0.1"
            reasoning_tokens: "4"
    anthropic:
      display_name: "Anthropic"
      base_url: "https://api.anthropic.com"
      connect_timeout_seconds: 10
      response_header_timeout_seconds: 60
      stream_idle_timeout_seconds: 120
      request_timeout_seconds: 600
      credential_validation_timeout_seconds: 10
      models: []
    google:
      display_name: "Google"
      base_url: "https://generativelanguage.googleapis.com"
      connect_timeout_seconds: 10
      response_header_timeout_seconds: 60
      stream_idle_timeout_seconds: 120
      request_timeout_seconds: 600
      credential_validation_timeout_seconds: 10
      models: []
  wallet:
    signer_address: "0x0000000000000000000000000000000000000001"
    networks:
      base:
        display_name: "Base"
        benefit_address_contract: "0x0000000000000000000000000000000000000002"
        fixed_withdrawal_fee_cnx: "1"
        withdrawal_min_cnx: "10000"
        max_withdrawals_per_day: 3
        fee_receiver_address: "0x0000000000000000000000000000000000000003"
```

Service Provider base URLs are operator-controlled trusted configuration. User input MUST never alter them.

Each Wallet network MUST reference an existing AS blockchain configuration so Beneficial Address reads use the existing rate-limited blockchain client.

## Configuration Validation

Startup MUST validate:

* every positive integer and duration;
* global model-name uniqueness;
* no configured Provider name collides with the initial Crynux catalog;
* known Service Provider IDs;
* non-negative rational usage weights;
* at least one positive usage weight per model;
* the encryption-key path is non-empty;
* Wallet signer and contract addresses are valid;
* every Wallet network display name is non-empty;
* fixed withdrawal fees and withdrawal minimums are non-negative decimal CNX strings with at most 18 fractional digits;
* Wallet network exists in `blockchains`.

When a later Relay loaded-model refresh introduces a name reserved by Provider configuration, AS MUST reject that conflicting Crynux catalog row and retain Provider routing.

## Startup

Startup MUST follow the order in [Architecture](architecture.md).

Important failure behavior:

* no secret file is deleted until every secret is read and validated;
* a missing or invalid encryption key fails startup before HTTP serving;
* migration failure stops startup;
* recovery inconsistency stops Provider scheduling;
* initial Relay catalog failure follows existing behavior, but Provider catalog construction may still succeed;
* HTTP serving begins only after Provider recovery.

## Graceful Shutdown

Shutdown MUST:

1. stop accepting HTTP requests;
2. stop Provider claims;
3. detach live downstream subscribers;
4. allow active Provider calls to continue until `shutdown_timeout_seconds`;
5. commit final usage and settlement whenever obtained;
6. stop other workers through the existing context flow.

Forced termination cannot preserve an upstream stream. Startup recovery MUST fail unrecovered in-flight Provider jobs under the pre-send or uncertain rules rather than replaying the request.

## Key Cooldown

Key creation MUST run credential validation without submitting billable model work.

Authentication failure, permission denial, rate limiting, and quota exhaustion MUST atomically increment the selected Key's consecutive unavailable count before the current job selects another Key. The current job MUST exclude that Key for all later selections. Provider 5xx responses, network errors, and timeouts MUST NOT increment the count or cause a Key switch. A successful Provider call MUST clear the count.

On the third consecutive unavailable result, AS MUST reset the count to zero and set `cooldown_until` to the current time plus `provider.key_cooldown_seconds`. A Key with a future `cooldown_until` MUST not receive a new claim or in-worker Key-switch assignment. A failed Key below the threshold MAY be selected by a later job. Cooldown expiry MUST restore eligibility through the scheduler's time comparison and MUST NOT require a background worker.

## Logging

Logs MUST identify:

* operation;
* job ID;
* Service Provider and model;
* redacted key logical ID;
* error class;
* withdrawal ID and network when applicable.

Logs MUST NOT contain:

* Shared API Key material;
* Authorization headers;
* prompts or result bodies;
* complete upstream error bodies;
* user signatures;
* signed raw wallet transactions.

Accepted Provider calls MUST not create an unbounded log entry per stream event.

## Runtime Hardening

The production AS container MUST:

* run as a dedicated non-root user;
* disable core dumps;
* have no `CAP_SYS_PTRACE`;
* use minimal Linux capabilities;
* use a read-only root filesystem;
* mount only required writable data directories;
* omit shell and debugger packages;
* use a minimal seccomp profile;
* restrict access to the container runtime socket and host.

Secret files MUST be provisioned with owner-only permissions and removed by AS after successful initialization.

## Wallet Funding

The AS Wallet hot wallet MUST hold enough CNX and native gas asset for expected withdrawals.

Wallet funding transfers are operational actions and MUST not modify AS earning balances.

## Provider Terms

Before enabling a Service Provider, operators MUST record approval that its terms permit the planned key sharing and multi-user access. Disabling a Service Provider for compliance reasons requires disabling its model configuration and all associated Shared API Keys on the next controlled deployment.
