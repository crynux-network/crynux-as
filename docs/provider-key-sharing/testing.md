# Provider Key Sharing Testing

## Test Isolation

Automated tests MUST NOT call public OpenAI, Anthropic, Google, blockchain RPC, or Crynux production services.

Provider integration tests MUST use local HTTP fake servers. Blockchain and Wallet contract tests MUST use local fakes or the repository's approved local test environment.

Secrets used by tests MUST be generated test values and MUST not resemble production credentials.

## Configuration Tests

Tests MUST cover:

* complete valid Provider configuration;
* every missing required field;
* zero and negative limits;
* duplicate Provider model names;
* Provider and Crynux model-name collision;
* unknown Service Provider;
* invalid usage weight;
* missing encryption-key path;
* wrong secret-file encoding or length;
* invalid Wallet addresses;
* unknown Wallet network;
* no implicit Go defaults.

## Encryption Tests

Tests MUST cover:

* AES-GCM key encryption and decryption;
* random nonce uniqueness;
* authenticated-data mismatch;
* ciphertext tampering;
* stable SHA-256 fingerprint;
* duplicate fingerprint rejection;
* delete clears encrypted secret and fingerprint fields;
* in-flight call retains key material already loaded before deletion;
* secret-file all-read-before-delete behavior;
* deletion failure stopping startup;
* API, logs, errors, and persisted rows not containing plaintext.

Log-redaction tests MUST scan captured log output for the complete test secret and Authorization header.

## Key Lifecycle Tests

Tests MUST cover:

* new key is enabled;
* enable and disable idempotency;
* disable excludes a key from later eligibility queries;
* disable does not cancel an in-flight call;
* enable does not clear an active cooldown;
* delete is idempotent and excludes the Key from later eligibility queries;
* deleted Key cannot be enabled;
* delete retains the soft-deleted row, history, and earnings;
* every key uses the global concurrency limit;
* Key Owner cannot modify concurrency or pricing.

## Model Routing Tests

Tests MUST cover:

* exact official Provider model match;
* no Provider prefix or alias;
* case-sensitive Provider lookup;
* unchanged Hugging Face normalization on a non-Provider model;
* every configured Provider model routes through Chat Completions and Responses;
* a Provider model on Completions returns HTTP 400;
* Provider model appears with one enabled, non-deleted key;
* Provider model remains during saturation and temporary cooldown;
* Provider model disappears when all keys are disabled or deleted;
* `/models` deterministic merged ordering;
* Crynux-only fields omitted from Provider catalog rows.

## Admission and Credits Tests

Tests MUST cover:

* model route fixed before reservation;
* Provider pricing snapshot persistence;
* exact rational weight parsing;
* conservative input and output estimate;
* account row lock;
* reserve and job create atomicity;
* insufficient available Credits;
* no hold without a job;
* queue timeout releasing the hold;
* omitted queue timeout using the configured default;
* Provider queue expiration calculated from `created_at + queue_timeout` without a separate deadline column;
* final usage lower than estimate;
* final usage equal to estimate;
* final usage greater than estimate and uncollected balance shortfall;
* global task price cap;
* global task price minimum: static mode rejects when the final estimated fee is below a non-zero `min_task_price_cnx`;
* global task price minimum: auto mode raises priority up to `auto_max_priority_gwei` when that maximum can meet the floor, otherwise rejects;
* `min_task_price_cnx` of `0` disables the admission floor;
* one-Credit minimum;
* no floating-point ledger arithmetic.

## Scheduler and Claim Tests

Tests MUST cover:

* priority descending and ID ascending order;
* Provider queue-priority snapshot excludes expired and claimed jobs;
* auto Cost Level reads one complete published Provider snapshot;
* bounded candidate selection;
* database eligibility excludes disabled, deleted, and cooling-down keys;
* in-memory eligibility excludes full and already-tried keys;
* one remaining eligible key is selected uniformly at random;
* process-local slot reservation followed by atomic job claim;
* two concurrent claims cannot exceed one key's configured slots;
* failed claim releases its reserved in-memory slot;
* only a successful claim starts a goroutine;
* streaming subscriber registration precedes scheduler visibility;
* queued jobs do not allocate goroutines;
* no eligible key leaves the job queued;
* Bridge poller never loads Provider jobs;
* Provider scheduler never loads Bridge jobs;
* one worker failure does not cancel the other.

## Adapter Contract Tests

Each adapter MUST run against a local fake server and cover:

* credential valid;
* credential invalid;
* validation service unavailable;
* non-streaming success;
* streaming success;
* safety stop, max-token truncation, empty assistant content, and tool-only output as success;
* tools and tool results;
* tool definitions, every supported tool choice, tool-call history, and tool-result history for OpenAI, Anthropic, and Google;
* text, JSON object, and JSON schema structured output for OpenAI, Anthropic, and Google;
* malformed response;
* missing usage;
* connection failure before send;
* timeout after possible acceptance;
* 401 or equivalent authentication failure;
* permission denial;
* 429 with reset hint;
* quota exhausted;
* authentication, permission, rate-limit, and quota errors increment the consecutive unavailable count;
* a qualifying failure updates the database count before another Key is selected;
* Provider 5xx, network errors, and timeouts do not increment the count;
* a successful call clears the count;
* the third consecutive unavailable result resets the count and starts the configured cooldown;
* a failed Key below the threshold remains eligible for a later job;
* cooldown expiry restores scheduling eligibility without a background transition;
* temporary cooldown remains catalog-visible;
* provider 5xx;
* upstream request ID capture;
* redacted error output.

Usage fixtures MUST cover ordinary, cached, cache-write, reasoning, and tool-use token fields without double-counting parent totals.

## Streaming Tests

Tests MUST cover:

* first upstream event reaches the downstream before upstream completion;
* Chat Completions chunk conversion;
* Responses event conversion;
* tool-call delta ordering;
* final usage-only Chat chunk behavior;
* successful `[DONE]`;
* safety stop, max-token truncation, empty assistant content, and tool-only output settle as success;
* no `[DONE]` after error;
* downstream disconnect while upstream continues;
* a complete upstream result after API Consumer disconnect still charges Credits, records usage, and creates the Key Owner earning;
* subscriber buffer overflow detaches the downstream without blocking upstream;
* no subscriber while final result still persists;
* no retry after the first public event;
* Crynux model stream remains completion-first simulated SSE;
* stream registry cleanup after every terminal path.

## Provider Key-Switch Tests

Tests MUST cover:

* definitely unaccepted failure switches keys inside the same goroutine without persisting intermediate Key failures;
* one job never selects the same failed Key twice;
* accepted or uncertain request is not retried;
* the final call record stores the last Shared API Key used;
* successful call settles once;
* confirmed usage is rejected when the adapter omits a public result;
* missing usage creates no automatic earning;
* repeated settle creates no duplicate records or events;
* submitted or in-progress Provider jobs from a prior process fail and release their Credits holds without resubmit;
* process-local slot counts start empty after restart.

## Settlement Tests

Tests MUST verify one transaction contains:

* job row lock;
* Credits hold release;
* call record;
* consumer charge when applicable;
* immediately available earning when applicable;
* earning account update;
* terminal job state.

Tests MUST cover:

* successful call with confirmed usage;
* failed unaccepted call;
* uncertain usage;
* zero earning after rounding;
* duplicate call-record source;
* duplicate Credits event source;
* duplicate earning source;
* account or job transaction rollback at every write step.

## Earning Availability Tests

Tests MUST cover:

* settlement immediately increases available;
* TaskIncome is immediately publishable;
* no earning-release worker or maturity state exists.

## Wallet Contract Tests

Wallet HTTP contracts are specified in [Wallet API](wallet-api.md). AS tests MUST run the exact Wallet DTO and signature fixtures for:

* event log query signature;
* withdrawal list query signature;
* fulfill signature;
* reject signature;
* 60-second Wallet signature boundary;
* event type numeric values;
* decimal amount strings;
* payload as JSON string;
* event ordering and processed-event visibility;
* withdrawal checkpoint gate;
* exact canonical user authorization message;
* personal-sign recovery with `0/1` and `27/28`;
* authorization fingerprint replay;
* Beneficial Address zero-address fallback;
* Beneficial Address mismatch;
* fixed withdrawal fee;
* daily requester and destination limits;
* idempotent fulfill;
* idempotent reject;
* fulfill/reject conflict;
* no timeout refund.

An integration test MUST run the unmodified Relay Wallet client against an AS fake server contract. It MUST cover log sync, withdrawal intake, callback retries, and balance agreement.

## Management API Tests

Tests MUST cover:

* JWT required;
* account ownership isolation;
* foreign ID returns 404;
* uniform response envelope;
* secret field write-only behavior;
* key create, enable, disable, delete, and list;
* Earn summary;
* keyset pagination for calls, events, and withdrawals;
* prepare withdrawal does not mutate balance;
* create withdrawal recomputes fee and Beneficial Address;
* precise big-integer JSON values;
* expected 400, 402, 404, 409, 429, and 503 mappings.

## Database and Migration Tests

Every migration MUST be tested against MySQL 8.1:

1. apply the complete migration chain;
2. verify new schema and indexes;
3. roll back the newest migration;
4. apply it again;
5. run representative queue, history, and Wallet queries.

`EXPLAIN` MUST confirm bounded indexed candidate selection for:

* Provider queue;
* key claim candidates;
* Key Owner call history;
* earning event history;
* withdrawal list;
* Wallet event publication;

Tests MUST reject lexicographic ordering of decimal-string priorities.

## WebUI Verification

Component and manual verification MUST cover:

* Earn navigation and authenticated route;
* available and withdrawal-locked balances;
* key upload secret clearing;
* Provider name, masked suffix, and enabled state display;
* enable, disable, and delete actions;
* no per-key concurrency input;
* call and earning keyset pagination;
* exact CNX amount formatting;
* wallet signature preparation;
* withdrawal status and explorer link;
* API failure toast with try-again-later text;
* desktop and mobile layout.

WebUI build MUST pass. E2E tests MUST be run only when explicitly requested.

## Security Regression

The final test run MUST search:

* database rows;
* test logs;
* captured errors;
* HTTP responses;
* generated OpenAPI;

for test Shared API Key plaintext and Authorization headers. Any match fails verification.
