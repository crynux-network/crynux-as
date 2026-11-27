## Documentation Index

| Document | Description |
|----------|-------------|
| [requirements.md](./requirements.md) | Functional requirements: wallet login and accounts, deposits and Credits, projects and private model APIs, Crynux and Service Provider execution, Key Owner earnings, and usage statistics |
| [architecture.md](./architecture.md) | High-level technical architecture only: system boundary, component map, and cross-cutting flow summaries |
| [configuration.md](./configuration.md) | YAML configuration contracts: HTTP public origin, blockchains, Bridge/Relay/Hugging Face, Credits referral, usage stats, and pointers to domain-specific config specs |
| [data-model.md](./data-model.md) | Persistent MySQL tables and row ownership overview |
| [process-lifecycle.md](./process-lifecycle.md) | Whole-process startup order and graceful shutdown |
| [deposit-processing.md](./deposit-processing.md) | Deposit processing specification: per-network scan workers, cursor and log filter rules, `credits_per_token` conversion, unknown-sender ignore rules, and atomic Credits ledger updates |
| [llm-api.md](./llm-api.md) | OpenAI-compatible API specification: private endpoints, merged model catalog, Crynux VRAM resolution and Bridge forwarding, Provider routing references, Cost Level, call records, and Task Fee Estimation |
| [llm-job-processing.md](./llm-job-processing.md) | Task execution and ledger ownership: `task_jobs`, `task_call_records`, `credit_events`, and `credit_accounts` responsibilities, request processing order, settle and failure transactions, Responses retention, Recent Requests merge, and retention cleanup |
| [model-compatibility/](./model-compatibility/) | OpenAI-compatible request conversion, canonical task persistence and raw-task transport, raw assistant-text parsing, and public response construction |
| [credits-billing.md](./credits-billing.md) | Credits billing and shared `billable_gwei` task-fee inputs: project Cost Level `priority_gwei`, Relay execution-time coefficients, VRAM weight, settle rules, and pricing-examples API |
| [raw-task-api.md](./raw-task-api.md) | Project-authenticated Raw Task routes, local ID isolation, batch behavior, LLM, Image, and Video results, public result download hashes, concrete dtype resolution, model catalogs, pricing, and settlement |
| [mcp.md](./mcp.md) | Remote Streamable HTTP MCP and local stdio/mcpb packages: auth, tool lists, Image/Video public download links, local save tools, and release layout |
| [provider-key-sharing/](./provider-key-sharing/) | Provider Key Sharing specification: Service Provider model routing, encrypted Shared API Keys, independent Bridge and Provider workers, real streaming, pricing, Key Owner earnings, Wallet-compatible withdrawals, APIs, operations, and testing |

## Architecture Document Scope

[architecture.md](./architecture.md) MUST stay concise and programmatic.

It MUST describe only the system boundary, major components and their responsibilities at a high level, pointers to configuration, data-model, and process-lifecycle docs, and short end-to-end flow summaries.

It MUST NOT contain table inventories, field-level configuration contracts, API request/response rules, cache refresh algorithms, dtype or model-resolution rules, pricing formulas, ledger transaction steps beyond a short flow outline, or other implementable detail.

All such detail MUST go into the dedicated document that owns the topic, as listed in the Documentation Index. When architecture names a component or flow, it MUST link to that dedicated document instead of restating its rules.

## Doc Update Requirements

When updating documentation files:

1. Read the entire document first to understand its structure, sections, and flow
2. Find the most appropriate location to integrate new content based on:
   - Logical relationship with existing sections
   - Document flow and narrative
   - Where readers would naturally expect to find the information
3. Integrate new content into the dedicated normative section that owns the topic
4. Do NOT simply create a new top-level section and place all new content there
5. Only create a new section if the topic is truly distinct from all existing content
6. Do NOT add detail into [architecture.md](./architecture.md); follow Architecture Document Scope above
7. Do NOT introduce a new feature by appending it to an example list, an illustrative phrase, an index blurb, an overview paragraph, or any other summary-style text. Write the full contract in the dedicated document or section that owns the topic. Update a summary or index line only when that line's own wording must change to stay accurate, and keep it as a short pointer rather than a growing feature catalog

Write documentation as a specification.

Documentation MUST state clear, final decisions and requirements.

Documentation MUST NOT include:
- Recommendations or advice.
- Options or alternatives.
- Speculation or uncertainty.
- Future-facing placeholders.

Documentation MUST use definitive language that can be implemented and tested:
- Requirement keywords: MUST, MUST NOT, SHALL, SHOULD. Use SHOULD only when a requirement level is intended.
- Exact behavior, constraints, and interfaces.

## Terminology Requirements

Crynux AS is one off-chain service that works with multiple configured blockchain networks. The term `network` MUST mean blockchain network unless the document explicitly defines another scope.

## Chat Content Isolation

Documentation MUST be generated from task requirements and authoritative project sources only.
User chat instructions about removing content are editing actions, not document content.
The final document MUST NOT restate removal instructions.
If a content type is removed, it must be absent from the final document.

Example chat cycle:
- AI draft includes setup commands.
- User says remove setup commands and keep only flow.
- Wrong final doc line: This document does not include setup commands.
- Right final doc line: Run the flow in order: prepare environment, start services, execute deposit and withdraw, then verify results.
