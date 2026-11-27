# Process Lifecycle

This document specifies Crynux AS process startup and graceful shutdown for the whole service.

## Startup Order

`main.go` MUST initialize the process in this order:

1. load and validate ordinary configuration;
2. read and validate every configured encryption secret file;
3. initialize logging;
4. initialize MySQL;
5. run migrations;
6. initialize blockchain clients and existing external clients;
7. initialize Service Provider adapters;
8. validate and remove the encryption-key file;
9. fail submitted or in-progress Provider jobs, and `pending_submit` Provider jobs with `phase = admitting`, from the prior process until no matching rows remain, and recover Credits holds;
10. start blockchain deposit processors and usage-stats workers;
11. refresh loaded-models, models-catalog, and queued-priority caches;
12. start Bridge poller, Provider scheduler, task-job retention cleanup, and the Credits-lock reconciler;
13. start the HTTP server.

HTTP serving MUST begin only after step 9 completes. Provider-specific startup failure behavior is specified in [provider-key-sharing/operations.md](./provider-key-sharing/operations.md). Provider encryption-secret file rules are specified in [provider-key-sharing/key-security.md](./provider-key-sharing/key-security.md).

## Graceful Shutdown

On `SIGINT` or `SIGTERM`, shutdown MUST:

1. stop accepting new HTTP requests;
2. stop Provider claims and stop other intake workers through the existing context flow;
3. allow already-accepted HTTP handlers, including Provider SSE subscribers, to finish within `provider.shutdown_timeout_seconds`;
4. allow active Provider calls to continue until `provider.shutdown_timeout_seconds`;
5. commit final usage and settlement whenever obtained, including when a downstream subscriber later disconnects;
6. detach remaining live downstream subscribers and cancel remaining Provider job contexts only after `provider.shutdown_timeout_seconds` elapses.

Within the shutdown deadline, Provider SSE MUST continue delivering remaining events through the post-settlement terminal event and `[DONE]` marker. Forced termination or a hard timeout cannot preserve an unfinished upstream stream. The next startup MUST fail unrecovered in-flight Provider jobs under the pre-send or uncertain rules rather than replaying the request, as specified in [provider-key-sharing/job-execution.md](./provider-key-sharing/job-execution.md).
