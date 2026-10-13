# Upstream Quota Tracker Revisions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement 8 key revisions to upstream quota tracking and credential security: AES-256-GCM secret vault with startup migration, infinite loop protection, deterministic bucket sorting, 30s refresh cooldown, protocol naming generalization, 2-column desktop modal, upstream quota reset action, and content-part `/quota` interception with arguments.

**Architecture:**
- New package `internal/vault` provides AES-256-GCM encryption/decryption with master key resolution (`NINEGUARD_ENCRYPTION_KEY` env var or `data/.master.key` file).
- In-place startup migration encrypts existing plaintext secrets in `providers`, `settings`, and `plugins`.
- Instance UUID generated and stored in SQLite `settings` table.
- Headers `X-NineGuard-Hop` and `X-NineGuard-Trace` checked in proxy and upstream client.
- `internal/upstream/router9.go` sorts buckets and adds Claude/Codex reset grant spending methods.
- `internal/proxy/chat_command.go` extracts text from array parts and filters by arguments.
- `web/static/js` views updated with 30s countdown, reset buttons, and 2-column grid modal.

---

## Plan Structure

- [x] **Task 1: Secret Vault (AES-256-GCM Encryption-at-Rest)**
  - Create package `internal/vault`:
    - `Init(dataDir string)`: resolves key from `NINEGUARD_ENCRYPTION_KEY` or `data/.master.key` (mode `0600`).
    - `Encrypt(plaintext string) (string, error)`: generates random 12-byte nonce, returns `enc:v1:<base64>`.
    - `Decrypt(ciphertext string) (string, error)`: transparently handles `enc:v1:` or legacy plaintext.
  - Implement startup in-place migration in `internal/db/db.go` for `providers`, `settings`, `plugins`.
  - Wire vault encryption into `internal/providers`, `internal/keys`, and `internal/plugins`.
  - Add unit tests for vault encryption, decryption, key generation, and migration.

- [x] **Task 2: Infinite Loop Protection & Instance Trace**
  - Ensure `instance_id` exists in `settings` table on startup.
  - In `internal/proxy/proxy.go`, check incoming `X-NineGuard-Trace` and `X-NineGuard-Hop`.
  - Reject with HTTP 508 if trace contains own `instance_id` or hop > 3.
  - In outgoing HTTP client calls, append `X-NineGuard-Trace` and increment `X-NineGuard-Hop`.
  - Add proxy unit tests for loop detection.

- [x] **Task 3: Deterministic Bucket Sorting & 30s Cooldown**
  - In `internal/upstream/router9.go`, sort buckets before returning:
    1. Warnings (`Warning == true` or lowest remaining %) first.
    2. 5h Session, Weekly 7d, Credit Balance.
    3. Alphabetical model buckets.
  - In `internal/upstream/client.go`, enforce 30s minimum cooldown on `force=1` per provider.
  - Add unit tests verifying sorting order and 30s cooldown enforcement.

- [x] **Task 4: Upstream Quota Reset Action**
  - In `internal/upstream/types.go`, add `ResetCredits` and `NextGrantID` to `ConnectionQuota`.
  - In `internal/upstream/router9.go`, extract reset credits from usage payload and implement `ConsumeReset(ctx, p, connId, grantId)`.
  - Add endpoint `POST /api/v1/upstream-quotas/{id}/connections/{connId}/reset` in `internal/handler/upstream_quota.go`.
  - Add handler tests.

- [x] **Task 5: `/quota` Slash Command Content Parts & Arguments**
  - In `internal/proxy/chat_command.go`, implement `extractMessageText` supporting `[]interface{}` content blocks.
  - Parse slash command arguments: `/quota [key|upstream|<filter>]`.
  - Filter output according to arguments.
  - Add unit tests verifying Pi-style content parts and argument filtering.

- [x] **Task 6: Web UI Revisions**
  - In `web/static/js/views/upstream_quotas.js`:
    - Add 30s cooldown timer badge on "Refresh" button.
    - Render `[🔄 N Reset]` button on connection cards with confirmation dialog.
  - In `web/static/js/views/providers.js`:
    - Rename dropdown label to `Router / Aggregator Protocol (9router, OmniRouter, CLI Proxy)`.
    - Apply 2-column grid layout on desktop (`max-width: 820px`).
  - Update frontend tests in `web/jstest/`.

- [x] **Task 7: Verification & End-to-End Testing**
  - Run `CGO_ENABLED=0 go test ./...`.
  - Run `node --test web/jstest/*.mjs`.
  - Push branch and trigger Docker build.
