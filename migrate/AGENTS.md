## Migration Authoring Rules

### Versioning Contract

Migrations are versioned and executed once by the migration framework.

Migration code in this directory MUST NOT add defensive re-execution guards such as:

- `HasTable` checks before `CreateTable` or `DropTable`
- `HasColumn` checks before `AddColumn` or `DropColumn`
- `HasIndex` checks before `CreateIndex` or `DropIndex`

Write migration steps as direct, deterministic schema transitions for the target version.

### No Unrequested Data Backfill

Migrations MUST NOT include data backfill logic unless the user explicitly approves that specific backfill in the current task.

Before requesting approval, state:

- The source and target tables and columns.
- Which existing rows will be read and updated.
- Why the schema change cannot work correctly without the backfill.
- The expected row count and database load.
- The batching, locking, restart, and rollback behavior.

An inferred need for compatibility, auditability, consistency, or preserving historical behavior is not approval. A broad request to add a feature, redesign a table, or create a migration is not approval. If implementation appears to require historical data changes, stop and ask the user before adding the backfill to a plan or writing any code.

When a migration creates a table or column, leave historical rows empty and let new application writes populate it unless the user has explicitly approved a backfill. Do not silently copy values from another table, derive values from current configuration, create historical records, or initialize new columns from old rows.

An approved data backfill MUST be listed as a separate plan item from the schema migration. Keep schema changes and data backfills in separate migration steps unless the user explicitly approves running the backfill as part of the automatic schema migration.

### Approved Backfill Design for High-Volume Tables

User approval authorizes the historical data change only. It does not relax query-cost, locking, batching, consistency, or restart requirements.

The following tables MUST always be treated as high-volume tables:

- `llm_jobs`
- `llm_call_records`
- `credit_events`
- `deposits`
- `account_usage_hourly_stats`
- `project_usage_hourly_stats`
- `project_model_usage_10m_stats`
- `project_duration_usage_10m_stats`
- `project_model_usage_snapshots`
- `project_duration_histogram_snapshots`
- `usage_stats_progress`

Any backfill that reads from or writes to one of these tables MUST follow the requirements below. Other backfill source and target tables MUST also be treated as high-volume unless the user provides evidence that they are small.

An approved backfill MUST:

- Select candidate IDs through indexed predicates, all membership filters, deterministic keyset order, and a fixed `LIMIT`.
- Process a bounded batch and commit before selecting the next batch.
- Join only the bounded candidate set to other tables. It MUST NOT join unbounded source and target tables and apply `LIMIT` afterward.
- Be restartable without duplicating or corrupting data.
- Avoid large `OFFSET`, unbounded updates, one transaction covering the full table, and unbounded joins.
- Define how concurrent application writes interact with each batch and use row locking only on the bounded candidate rows when locking is required.
- Be checked with MySQL 8.1 `EXPLAIN` before execution.

### Local Structs Only

Migration code MUST NOT reference structs from the `models` package. Define a local struct inside the migration file, frozen to the schema at the time the migration is written, with a `TableName()` method for the target table.

Live `models` structs keep evolving after a migration ships. A migration that references them produces a different schema every time the models change, which breaks replaying the migration chain on a fresh database.

Migration structs MUST NOT use GORM soft delete unless it is explicitly required. Do not embed `gorm.Model` and do not add a `gorm.DeletedAt` / `deleted_at` column. When a table needs ID and timestamps, define `ID`, `CreatedAt`, and `UpdatedAt` fields explicitly. This matches the model rule in `models/AGENTS.md`.

### GORM and Migration Library Versions

Use these versions when writing migrations:
- `go-gormigrate` `v2.1.0`
- `gorm` `v1.25.2`

Before editing a migration, check the official documentation for these exact versions to confirm the correct syntax and APIs. Do not hand-write SQL unless there is no supported GORM-based approach.

### Target DB

The online system is using MySQL v8.1.0 as the DB server. Make sure the migrations are compatible with it.

MySQL migrations MUST create tables with `CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`. `InitMigration` sets this through `gorm:table_options`. Application MySQL DSNs MUST include `collation=utf8mb4_unicode_ci` and MUST NOT include `charset=`. The Go MySQL driver applies `charset=` as MySQL 8 default `utf8mb4_0900_ai_ci`, which conflicts with table `utf8mb4_unicode_ci` on string UNION queries.

### Mandatory Local MySQL Testing

Every new or modified migration MUST be tested locally against a MySQL 8.1.0 instance before it is considered complete. SQLite-based unit tests do not satisfy this requirement.

The test MUST run the full migration chain from `migrate.InitMigration` on a clean, empty database, then roll back the newest migration and re-apply it. The migration is complete only when all of these runs succeed.
