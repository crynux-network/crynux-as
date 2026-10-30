# Provider Key Sharing

## Purpose

Provider Key Sharing lets a Crynux AS user make an API key issued by a third-party model service available to all Crynux AS projects. Calls routed through that key consume the caller's Credits and create CNX earnings for the user who owns the key.

This directory is the authority for the Provider Key Sharing design. The existing documents remain authoritative for account identity, project authentication, the Crynux Bridge execution path, the Credits ledger, and the current OpenAI-compatible request contract.

## Terminology

* **Service Provider**: an external model API service. The first release supports OpenAI, Anthropic, and Google.
* **Key Owner**: the Crynux AS user who uploads a Shared API Key and owns the resulting CNX earnings.
* **Shared API Key**: an API key uploaded by a Key Owner and authorized for use by Crynux AS. A Shared API Key belongs to one Key Owner and one Service Provider.
* **API Consumer**: a project owner whose project submits a model call and pays Credits.
* **Provider model**: a model declared under a Service Provider in the AS YAML configuration.
* **Provider job**: a `task_jobs` row whose `execution_source` is `openai`, `anthropic`, or `google`. Crynux Bridge jobs use `execution_source = crynux`.

The word `provider` in field and package names means Service Provider. It never means Key Owner.

## Scope

The first release:

* supports Chat Completions and Responses for every configured Provider model through Service Provider adapters;
* rejects Completions for Provider models;
* keeps official Service Provider model names, such as `gpt-5.6-sol`, without a prefix, suffix, or alias;
* routes Crynux repository model names through the existing Bridge path;
* routes configured Provider model names through the corresponding Service Provider adapter;
* applies one global concurrency limit to every Shared API Key;
* lets Key Owners set per-key maximum input and output token limits and reset usage counters;
* supports create, list, enable, disable, and soft-delete Key management;
* streams Provider output in real time;
* charges API Consumers in Credits;
* records the complete CNX-equivalent task fee as Key Owner earnings;
* pays earnings through an unmodified `crynux-relay-wallet` deployment connected to AS-compatible Wallet APIs.

The first release does not:

* support Provider image, audio, video, or Raw Task execution;
* discover or publish models from an upstream `/models` response;
* allow Key Owners to set prices or per-key concurrency;
* maintain a per-key model allowlist;
* fall back from a Provider model to Crynux Bridge;
* run more than one AS process;
* run more than one active AS Wallet process;
* guarantee recovery or transparent replay of an upstream request whose acceptance is uncertain.

## Primary Invariants

1. A model name resolves to exactly one execution source.
2. A Provider model name resolves to exactly one Service Provider.
3. A Shared API Key is never stored in plaintext.
4. Disabling a Shared API Key stops later eligibility queries from selecting it but does not cancel an in-flight Provider call.
5. A queued Provider job has no execution goroutine until the scheduler atomically claims a key slot.
6. Bridge polling and Provider execution use independent background loops.
7. Credits reservation and job creation commit in one transaction.
8. Credits release, call record creation, charge, Key Owner earning creation, and terminal job update commit under one job settlement boundary.
9. A job creates at most one permanent call record and at most one consumer charge event.
10. A billable Provider call record creates at most one Key Owner earning source.
11. Confirmed billable usage is settled only with a valid public result, a success call record, and a completed job.
12. Credits charge, token usage recording, and Key Owner earning follow only whether the Service Provider completed the request. Settlement MUST complete when the Service Provider finishes, including after API Consumer disconnect or when no subscriber remains.
13. Wallet event and withdrawal APIs remain byte-for-byte compatible with the data contract expected by the unmodified Relay Wallet.

## Document Index

* [Requirements](requirements.md)
* [Architecture](architecture.md)
* [Job Execution](job-execution.md)
* [Key Security](key-security.md)
* [Wallet Settlement](wallet-settlement.md)
* [Wallet API](wallet-api.md)
* [Pricing and Billing](pricing-billing.md)
* [API Compatibility](api-compatibility.md)
* [Data Model](data-model.md)
* [Provider Adapters](provider-adapters.md)
* [Management API](management-api.md)
* [Operations](operations.md)
* [Testing](testing.md)

## Related Existing Specifications

* [Crynux AS Requirements](../requirements.md)
* [Crynux AS Architecture](../architecture.md)
* [Task Job Processing](../llm-job-processing.md)
* [Credits Billing](../credits-billing.md)
* [OpenAI-Compatible API](../llm-api.md)
* [Model Compatibility Architecture](../model-compatibility/architecture.md)
