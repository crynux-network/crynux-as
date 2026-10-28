# Shared API Key Security

## Security Objective

The key-encryption design MUST prevent an attacker who obtains only the AS database, a database backup, or files remaining after startup from directly recovering Shared API Keys.

The design MUST raise the cost of memory extraction through process and deployment hardening. It does not guarantee that plaintext keys are impossible to extract when an attacker can execute arbitrary code as the AS process user or has host administrator privileges.

## Encryption Key

Configuration MUST provide one 32-byte AES-256-GCM encryption key through a file. The encryption key MUST NOT be stored in MySQL or an ordinary YAML value.

## Encryption Format

Every Shared API Key MUST use direct AES-256-GCM encryption:

1. generate a unique 12-byte nonce;
2. encrypt the plaintext API key with the configured encryption key;
3. persist the ciphertext and nonce.

The database MUST store:

* logical Shared API Key ID;
* Service Provider;
* key ciphertext;
* key nonce;
* SHA-256 fingerprint;
* masked suffix;
* creation and deletion timestamps.

The database MUST NOT store plaintext key material or the encryption key.

## Authenticated Data

AES-GCM additional authenticated data MUST bind ciphertext to its immutable identity:

```text
crynux-as:shared-api-key:<key-public-id>:<owner-user-id>:<service-provider>
```

Decryption MUST fail when any bound value differs. Mutable fields such as enable state, unavailable count, or cooldown MUST NOT be authenticated data.

## Fingerprint

AS MUST compute:

```text
SHA256(plaintext_api_key)
```

The `(service_provider, fingerprint)` pair MUST be unique among non-deleted Keys. The fingerprint MUST NOT be returned by any API.

## Secret File Loading

Configuration MUST declare one encryption-key file.

Startup MUST:

1. parse ordinary YAML without deleting files;
2. collect every distinct secret path;
3. read all files;
4. validate encoding and exact key length;
5. validate configuration and initialize every dependent component;
6. remove the configured encryption-key file;
7. fail startup when removal fails.

AS MUST NOT delete any secret file until every configured secret has been read and validated. A partial read MUST leave all files in place.

Secret files MUST contain base64-encoded 32-byte values followed by optional whitespace. Other lengths and encodings MUST fail startup.

The deployment mechanism MUST provision the files again for every process start. The files MUST be created with permissions readable only by the AS runtime user.

## Plaintext Lifetime

The upload path MUST:

1. bind the request body without logging it;
2. copy the secret into a mutable byte buffer;
3. validate and fingerprint it;
4. encrypt it;
5. clear request-owned and temporary mutable buffers as soon as possible.

The execution path MUST:

1. load the selected Key's encrypted secret fields;
2. decrypt the API key immediately before request construction;
3. attach it to the upstream request;
4. clear mutable plaintext-key buffers after transport setup no longer needs them.

Code MUST avoid unnecessary conversion of secrets to immutable Go strings. Adapters MUST accept secret byte buffers where the HTTP library permits.

Go runtime behavior can leave copies outside application control. Documentation, tests, and logs MUST NOT claim guaranteed memory zeroization.

## Logging and Error Redaction

The following data MUST never appear in logs, traces, metrics, errors, database diagnostics, panic values, or HTTP responses:

* Shared API Key plaintext;
* full Authorization header;
* key ciphertext;
* encryption key;
* request body fields that can contain a key.

Key-related logs MAY include:

* logical key ID;
* Key Owner user ID;
* Service Provider;
* masked suffix;
* lifecycle and health states;
* adapter error class;
* upstream request ID when it is not a secret.

Provider error bodies MUST pass through adapter redaction before logging or persistence.

## Upload Validation

Upload validation MUST use the adapter for the selected Service Provider.

* Validation MUST call the adapter's authenticated, non-billable Service Provider validation operation.
* A returned model list proves authentication only and MUST NOT alter AS model configuration.
* Validation MUST use bounded timeouts.
* Validation MUST NOT submit billable model work.
* Authentication failure MUST reject the upload.
* Temporary Service Provider unavailability MUST return a service-unavailable management error and MUST NOT persist the key.

## Enable, Disable, and Delete

* Enable sets `owner_enabled = true`.
* Disable sets `owner_enabled = false`.
* Delete atomically sets `deleted_at`, sets `owner_enabled = false`, and clears the fingerprint, key ciphertext, and key nonce.
* Disable and delete exclude the Key from subsequent scheduler and in-worker Key-switch eligibility queries.
* A claim that selected the Key before a concurrent disable, delete, or cooldown update MAY continue.
* Active calls that already loaded key material continue with that material.
* Enable does not clear an active cooldown.
* A deleted Key cannot be enabled or restored through the public API.
* The soft-deleted row retains ownership, Service Provider, masked suffix, and historical references.

## Backup and Restore

Database backups contain encrypted key material and MUST be protected as sensitive data.

Restoring a backup requires the same encryption key that encrypted its non-deleted Keys. AS MUST fail startup when the configured encryption-key file is missing or invalid.

Backups MUST NOT contain startup secret files, plaintext exports, or memory dumps.

## Runtime Hardening

Production deployment MUST:

* run AS under a dedicated non-root operating-system user;
* disable core dumps;
* omit `CAP_SYS_PTRACE` and all unnecessary capabilities;
* use a read-only container filesystem except explicit data paths;
* avoid an interactive shell and debugging tools in the runtime image;
* apply a minimal seccomp profile;
* restrict database, SSH, container-runtime, and host administrator access;
* prevent secrets from entering command-line arguments or environment dumps;
* use encrypted or disabled swap according to the host security policy.

Removing debug symbols MAY increase analysis effort but MUST NOT be treated as encryption or access control.
