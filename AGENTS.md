## Coding Requirements

### Full Picture

Before making any changes, consult `./README.md` and `./docs/AGENTS.md` for a high-level project overview. All modifications must be consistent with the existing architecture and design.

### Clean Code

Before adding new code, first check whether existing logic can be reused. Prefer extracting reusable code into a dedicated function, class, or file, and place it in the most appropriate location. Remove duplicated code and avoid adding redundant implementations of the same functionality.

### Backward Compatibility

Do not add backward-compatibility designs or legacy-behavior handling unless backward compatibility is explicitly required. Without such a requirement, implement only the current contract.

### Data Integrity

Because Crynux AS handles user payments and the Credits ledger, the correctness of financial data must be strictly guaranteed under all circumstances.

Processing of deposits and charges may be delayed, but any data that has been processed must remain correct and consistent. In particular, during unexpected exceptions and shutdown, ensure that in-flight operations do not stop at a point that leaves the Credits ledger in an inconsistent state.

### Database Queries

Do not use SQL `LIKE` in queries. Query by exact column values instead. If pattern-based filtering is needed, fetch rows by indexed exact conditions and filter in application code, or store a dedicated column that supports exact matching.

Treat task, call-record, Credits-event, deposit, and usage-statistics tables as high-volume tables. Queries against these tables MUST use indexed predicates and bounded batches.

When an API or worker needs only a limited number of rows, it MUST select the candidate row IDs with all membership filters, deterministic ordering, and `LIMIT` before joining other high-volume tables. The join MUST operate only on that bounded candidate set. Do not join unbounded portions of two high-volume tables and apply `LIMIT` afterward.

When the database can produce the final result in one query, candidate selection, bounded joins, set combination, final ordering, and final `LIMIT` MUST stay in one SQL statement using derived tables, CTEs, or equivalent indexed subqueries. Do not return candidate IDs or partial result sets to application code for joining, merging, sorting, or truncation unless one SQL statement cannot express the required behavior correctly.

If a filter from another table determines which rows belong in the result, the query MUST use an indexed semi-join or store the required exact-match field on the candidate table. It MUST NOT fetch a broad join and filter it in application code.

Large histories MUST use keyset pagination or a persisted cursor. Do not use a large `OFFSET` against a high-volume table. Cleanup and aggregation workers MUST process deterministic, bounded batches and commit between batches.

Every new or changed query spanning high-volume tables MUST be checked with MySQL 8.1 `EXPLAIN` to confirm that candidate selection uses the intended indexes and that joins do not scan an unbounded high-volume table.

### Proper Error Handling

All function errors must be propagated up the call stack until handled. Any unhandled error reaching the `main` function must be logged and trigger an alert to operators.

### Proper Logging

Add sufficient logging at appropriate points in the code with the correct log levels, so both operators and developers can identify and diagnose issues easily.

### Clean Comment

Do not explain code changes in comments, such as "added xxx" or "removed xxx because xxx". Only describe the functionality of the final code. Keep comments concise and only add them for complex or non-obvious logic.

Do not use comments to delete code; directly remove the code without adding explanations about what was deleted.
