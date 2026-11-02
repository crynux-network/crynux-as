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

* new key is enabled and uses the configured default input and output token limits;
* enable and disable idempotency;
* disable excludes a key from later eligibility queries;
* disable does not cancel an in-flight call;
* enable does not clear an active cooldown;
* delete is idempotent and excludes the Key from later eligibility queries;
* deleted Key cannot be enabled;
* delete retains the soft-deleted row, history, and earnings;
* every key uses the global concurrency limit;
* Key Owner cannot modify concurrency or pricing;
* setting max input or output to null leaves that side unlimited;
* zero or negative max values are rejected;
* successful settlement increments used input and output counters by the call record totals;
* failed settlement does not change usage counters;
* repeating settlement for a terminal job does not double-count usage;
* a key with `used_input_tokens >= max_input_tokens` or `used_output_tokens >= max_output_tokens` is excluded from eligibility;
* resetting usage zeros both counters without changing limits and restores eligibility when under limits;
* lowering a max below current used excludes the key without a reset;
* token-limit exhaustion does not cancel an in-flight claim and does not remove catalog membership.

## Model Routing Tests

Tests MUST cover:

* exact official Provider model match after lowercase normalization;
* no Provider prefix or alias;
* Provider lookup matches when request or config letter case differs;
* unchanged Hugging Face normalization on a non-Provider model;
* every configured Provider model routes through Chat Completions and Responses;
* OpenAI public Responses executes against OpenAI Chat Completions and never against OpenAI Responses;
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
* Provider shared admission floor: `min_task_price_cnx.providers` converts into one Cost Level lower bound from the cheapest configured Provider model under the current request's estimated input and maximum generated tokens;
* Provider shared admission floor: model order does not change the shared Cost Level lower bound;
* Provider shared admission floor: every selected Provider model uses the same shared Cost Level lower bound for one request;
* Provider shared admission floor: weights keep relative model price ratios when Cost Level is held at that shared lower bound;
* Crynux LLM admission floor scales by maximum completion tokens over `llm.default_max_tokens`;
* Crynux Image admission floor scales by pixel-step units over `1 * 1024 * 1024 * 30`;
* Provider shared admission floor: static mode rejects when estimated Credits exceed `auto_max_credits_per_task` without changing priority;
* Provider shared admission floor: static mode rejects when fixed Cost Level is below the shared lower bound;
* Provider shared admission floor: auto mode clamps queue-target Cost Level into `[shared_min_priority, max_priority_within_auto_max_credits]` and rejects when that interval is empty;
* billing_config Provider min hints use the fixed reference request `10 input + 50 output`;
* `min_task_price_cnx.crynux` or `min_task_price_cnx.providers` of `0` disables that admission floor;
* configuration accepts only the fixed keys `crynux` and `providers` for `min_task_price_cnx`;
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

## Responses previous_response_id Tests

Tests MUST cover:

* same-project completed retained previous job expands into the Provider request input before submission;
* expanded Provider request bodies omit `previous_response_id`;
* OpenAI, Anthropic, and Google upstream bodies omit `previous_response_id`;
* missing, cross-project, expired, non-Responses, incomplete, and failed previous IDs return the same invalid-request shape without revealing other projects;
* a later Provider Responses call may chain from a prior Provider Responses job in the same project;
* a later Provider Responses call may chain from a prior Crynux Responses job in the same project when the prior job remains retained;
* public Responses `id` values used for chaining and `GET /responses/:id` are the AS job `PublicID`.

## Adapter Contract Tests

Each adapter MUST run against a local fake server and cover:

* credential valid;
* credential invalid;
* validation service unavailable;
* non-streaming success;
* streaming success;
* omitted public max-token fields inject `llm.default_max_tokens` into OpenAI, Anthropic, and Google upstream bodies;
* an explicit positive public max-token field overrides `llm.default_max_tokens` in those upstream bodies;
* safety stop, max-token truncation, empty assistant content, and tool-only output as success;
* tools and tool results;
* tool definitions, every supported tool choice, tool-call history, and tool-result history for OpenAI, Anthropic, and Google;
* text, JSON object, and JSON schema structured output for OpenAI, Anthropic, and Google;
* JPEG, PNG, and WebP base64 image inputs on Chat Completions and Responses;
* ordered mixed text and image content, image-only user messages, and multi-image requests;
* rejection of HTTP image URLs, unsupported MIME types, invalid base64, and payloads above `provider.max_input_image_bytes`;
* OpenAI `detail` passthrough and Anthropic/Google `detail` drop;
* `previous_response_id` history that preserves image MIME, base64, order, and detail;
* image input-token estimation for OpenAI, Anthropic, and Google model families;
* text admission estimates that skip image base64 payloads;
* settlement that uses actual input tokens without a second image charge;
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
* Responses `response.completed` only after settlement commits;
* Chat Completions usage chunk and `[DONE]` only after settlement commits;
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
* in-worker key switch releases the old process-local slot and the job's final release targets only the currently held key;
* one job never selects the same failed Key twice;
* accepted or uncertain request is not retried;
* the final call record stores the last Shared API Key used;
* successful call settles once;
* confirmed usage is rejected when the adapter omits a public result;
* missing usage creates no automatic earning;
* repeated settle creates no duplicate records or events;
* submitted or in-progress Provider jobs from a prior process fail and release their Credits holds without resubmit;
* `pending_submit` Provider jobs with `phase = admitting` from a prior process fail and release their Credits holds without resubmit;
* `FailPriorProcessProviderJobs` continues until no matching rows remain;
* a failed publish from `admitting` to `queued` fails the job, unregisters the subscriber, and releases the Credits hold;
* process-local slot counts start empty after restart.

## Graceful Shutdown Tests

Tests MUST cover:

* `SIGINT` / `SIGTERM` stops new HTTP requests while already-accepted short requests can complete;
* Provider claiming stops after intake cancellation;
* already-accepted non-streaming waits can receive a terminal response within `shutdown_timeout_seconds`;
* already-accepted Provider SSE can receive remaining events through terminal and `[DONE]` within `shutdown_timeout_seconds`;
* active Provider `Execute` continues after intake cancellation and settles confirmed usage;
* confirmed usage still settles when the downstream subscriber has disconnected;
* hard timeout detaches remaining stream subscribers and cancels remaining Provider job contexts;
* hard-timeout leftovers that remain submitted or in progress, and abandoned `admitting` streaming jobs, fail on the next startup through `FailPriorProcessProviderJobs` without upstream resubmit;
* `provider.shutdown_timeout_seconds` is the shutdown deadline used by the process.

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
