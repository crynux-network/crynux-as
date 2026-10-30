# Provider Job Execution

## Authority

This document extends [Task Job Processing](../llm-job-processing.md) for Service Provider execution. Existing Bridge job behavior remains authoritative where this document does not explicitly distinguish Provider jobs.

## Execution Sources

Every accepted job MUST persist one execution source:

* `crynux`: execute through Crynux Bridge using the existing bounded poller;
* `openai`, `anthropic`, or `google`: execute through the corresponding Service Provider adapter.

Execution source MUST be selected before Credits reservation. It MUST NOT change after job creation. There is no fallback between Bridge and a Service Provider.

## Shared Admission

The request path MUST run these steps in order:

1. authenticate the project;
2. parse the public request sufficiently to identify model and API type;
3. resolve execution source;
4. normalize the request into the execution source's persisted request form;
5. resolve effective Cost Level;
6. create the immutable billing estimate and pricing snapshot;
7. estimate the maximum charge;
8. generate the stable public job ID and register its bounded live-stream subscriber when the request is streaming;
9. lock `credit_accounts` with `SELECT ... FOR UPDATE`;
10. require `balance - locked >= estimated_credits`;
11. increase `locked` and insert the job in one transaction;
12. wait, return a background Responses object, or expose retained polling according to API type.

The streaming subscriber MUST be registered before the job row becomes visible to the Provider scheduler. A failed admission transaction MUST unregister it. This ordering MUST prevent a Provider goroutine from publishing the first event before the requesting handler can receive it.

Provider job creation MUST persist:

* exact public model name;
* Service Provider;
* API type;
* normalized Provider request data;
* complete usage-weight snapshot;
* effective priority;
* estimated usage;
* estimated Credits and Credits hold;
* resolved queue timeout seconds, using the request `queue_timeout` when present and `provider.default_queue_timeout_seconds` otherwise;
* stream and background flags.

A Shared API Key MUST NOT be selected during admission. Key selection belongs to the Provider scheduler and, for in-worker key switches, to the Provider job goroutine.

Provider queue expiration MUST be calculated as:

```text
queue_expires_at = task_jobs.created_at + task_jobs.queue_timeout
```

AS MUST NOT persist a separate Provider queue deadline.

## Top-Level Worker Structure

`RunJobWorkers` MUST supervise two independent loops:

```text
RunBridgeJobPoller(ctx)
RunProviderScheduler(ctx)
```

Each loop MUST return errors to its own task boundary. Termination of one loop MUST NOT cancel the other loop.

## Bridge Job Poller

`RunBridgeJobPoller` is the existing Bridge job loop from [Task Job Processing](../llm-job-processing.md), moved under `RunJobWorkers`. Its behavior MUST remain unchanged.

The only required change is that the existing query MUST load only `execution_source = crynux` jobs. It MUST NOT load Provider jobs.

This document MUST NOT redefine Bridge submit, status, download, retry, or settlement rules. Those rules remain in [Task Job Processing](../llm-job-processing.md).

## Provider Scheduler

The Provider scheduler MUST use the database as the queue source of truth.

Candidate selection MUST:

* filter `execution_source IN (openai, anthropic, google)`;
* filter job status `pending_submit`;
* order by effective priority descending and job ID ascending;
* select candidate IDs with all membership filters and `LIMIT` before joining other high-volume tables.

For each selected candidate, the scheduler MUST calculate queue expiration from `created_at + queue_timeout`. An expired job MUST fail and release its Credits hold without selecting a key or starting an upstream request.

For each candidate, the scheduler MUST find a Shared API Key that:

* belongs to the job's Service Provider;
* has `owner_enabled = true`;
* is not deleted;
* has `cooldown_until IS NULL OR cooldown_until <= now`;
* has `max_input_tokens IS NULL OR used_input_tokens < max_input_tokens`;
* has `max_output_tokens IS NULL OR used_output_tokens < max_output_tokens`.

The database query MUST exclude keys in cooldown and keys whose set token limits are exhausted. From the returned keys, the process-local key-slot manager MUST exclude keys with no available slot and keys already tried by the current job. The scheduler MUST select one remaining key uniformly at random. It MUST NOT persist fairness counters or last-assigned state.

The scheduler MUST claim each job at most once. Switching to another Key after a definitely unaccepted key-specific failure belongs to the Provider job goroutine, not to a second scheduler claim.

## Atomic Claim

Claim MUST run in one database transaction:

1. reserve one process-local slot for the selected key;
2. lock the job row;
3. verify that it is still `pending_submit` and that `created_at + queue_timeout` has not expired;
4. set the job to `submitted` and set `started_at` when absent.

The transaction MUST fail without database side effects when any condition changes. Any failed claim transaction MUST release the reserved process-local slot.

The claim transaction MUST NOT lock or re-check the selected Shared API Key row. A key selected by the preceding eligibility query MAY still receive this claim when enable, delete, cooldown, or token-limit state changes concurrently after that query.

Only a successful commit MAY start `RunProviderJob(job_id, shared_api_key_id)`. A queued job MUST have no goroutine.

## Provider Job Goroutine

One goroutine MUST own one claimed job from start to terminal handling:

1. reload the job and Shared API Key metadata;
2. decrypt the key into a short-lived buffer;
3. build the adapter request from persisted job data;
4. initiate the upstream request;
5. read every upstream event until terminal completion;
6. normalize and publish stream events;
7. assemble the final normalized result;
8. normalize final usage;
9. invoke common job settlement with the final Shared API Key ID;
10. release the key slot;
11. clear plaintext buffers and exit.

Slot release MUST be idempotent.

## In-Worker Key Switch

Automatic Key switching MUST run only inside the Provider job goroutine. Intermediate Key failures, tried-Key state, and Key switches MUST remain in memory and MUST NOT return the job to `pending_submit` for another scheduler claim.

The goroutine MAY switch to another eligible Shared API Key after a definitely unaccepted key-specific failure when all of these conditions hold:

* no public output event has been emitted;
* the adapter can prove the Service Provider did not accept billable work;
* no upstream request ID or usage indicates accepted work;
* `created_at + queue_timeout` has not expired;
* the number of upstream send tries for this job is below `provider.max_attempts_per_job`;
* another eligible key remains available.

Eligible failures are authentication invalid, permission denied, rate limited, and quota exhausted when the adapter can also prove that the request was not accepted as billable work. Local request-validation failures and Provider-wide failures MUST fail the job without switching Keys.

A key switch MUST:

1. atomically increment the failed Key's `consecutive_unavailable_count` in the database and, on the third consecutive unavailable result, reset the count and set `cooldown_until`;
2. add the failed Key to the job's in-memory tried-Key set;
3. release the current process-local key slot;
4. query database-eligible keys, exclude full and already-tried keys in memory, randomly select one remaining key, and reserve its process-local slot;
5. continue in the same goroutine with the selected Key.

The same job MUST never select a Key already present in its tried-Key set. A failed Key that has not reached cooldown may be selected by a later job. Confirmed billable usage, any public downstream event, and uncertain acceptance MUST stop further Key switches.

Connection loss after request bytes may have reached the Service Provider, read timeout after headers, stream truncation, and process termination after send MUST be classified as uncertain. Uncertain outcomes MUST NOT be retried automatically.

When any retry condition fails, the job MUST enter the applicable terminal or uncertain handling path.

## Real-Time Streaming

For a Provider streaming request:

* the HTTP handler MUST subscribe to the job before waiting;
* the upstream request MUST use the Service Provider's streaming protocol;
* OpenAI events MUST be validated and normalized;
* Anthropic and Google events MUST be translated into the selected public OpenAI-compatible protocol;
* the first public data event establishes the no-retry boundary;
* after the first public event, no key switch is allowed;
* a downstream disconnect or slow subscriber MUST detach only the subscriber;
* the Provider goroutine MUST continue reading upstream until the Service Provider completes the request;
* Credits charge, token usage recording, and Key Owner earning MUST follow only whether the Service Provider completed the request and MUST NOT follow API Consumer connection or public-delivery state;
* the adapter MUST withhold the public terminal event and `[DONE]` marker after receiving a valid terminal upstream event;
* Credits and earnings settlement MUST commit successfully before the public terminal event and `[DONE]` marker are emitted;
* a settlement storage failure after confirmed usage MUST keep the result and usage in the active goroutine, retry settlement, and keep the job non-terminal;
* settlement retry MUST continue only while the active goroutine remains alive, independently of downstream timeout or disconnect, and the job MUST become terminal only after settlement commits;
* a still-connected subscriber receives the terminal event and `[DONE]` only after the successful settlement commit.

When a live subscriber is absent, the goroutine MUST still assemble the final result and complete settlement.

## Terminal Handling

Credits charge, token usage recording, and Key Owner earning MUST be determined only by whether the Service Provider completed the request. When the Service Provider completes the request, settlement MUST complete. API Consumer disconnect, a detached or absent subscriber, and any other public-delivery outcome MUST NOT cancel, defer, or change that settlement.

Confirmed billable usage and a valid final public result MUST occur together. Safety stops, max-token truncation, empty assistant content, and tool-only output remain valid public results. The charge path MUST create a success call record and mark the job completed. AS MUST NOT charge a failed job.

### Confirmed usage

The common settlement transaction MUST:

1. lock the job;
2. confirm that no terminal call record exists;
3. release the Credits hold;
4. create one success `task_call_records` row;
5. lock the final Shared API Key row and add the settled input and output token totals to its usage counters;
6. create the consumer task-charge event and debit Credits;
7. create one processed Key Owner earning event referencing the new call record and increase the available balance;
8. update the earning account;
9. write final temporary job result fields;
10. mark the job completed and billed.

This transaction MUST run after a completed Service Provider response even when the API Consumer has disconnected or no subscriber remains.

### No confirmed usage

The failure transaction MUST release the hold, create a failed call record, and mark the job failed. It MUST NOT create a consumer charge or Key Owner earning.

### Missing final usage

The job MUST fail without consumer charge or Key Owner earning. Observed partial usage and upstream request identity MUST NOT be persisted.

## Disable, Delete, and Token Limits

Disable, delete, or token-limit exhaustion after claim MUST NOT interrupt a request that already loaded its key material.

The key MUST become unavailable for new claims and for in-worker key switches immediately after the lifecycle update commits, or after settlement pushes a set limit to `used >= max`.

## Shutdown and Recovery

Graceful shutdown MUST:

1. stop the Provider scheduler from claiming new jobs;
2. stop accepting new HTTP requests;
3. detach downstream stream subscribers;
4. allow active Provider goroutines to finish within the configured shutdown deadline;
5. stop the Bridge poller and other workers through the existing context flow.

On startup:

* queued Provider jobs MUST remain eligible for normal scheduling;
* submitted or in-progress Provider jobs from the prior process MUST fail without consumer charge and release their Credits holds;
* Credits holds MUST be reconciled through the existing hold reconciler.

Startup handling MUST NOT resubmit an upstream request from a prior process.

## Retention

Temporary Provider request and formatted result data on `task_jobs` MUST follow the existing job retention period. Permanent call records, Credits events, earning events, and withdrawals MUST not be deleted by job retention.
