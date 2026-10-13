# Upstream Quota Tracker Revisions — Design Spec

- Date: 2026-10-13
- Status: Approved Design
- Related: `GLOSSARY.md`, ADR 0009, `docs/superpowers/specs/2026-10-13-upstream-quota-tracker-design.md`

---

## 1. Scope of Revisions

### 1. Secret Vault (AES-256-GCM Encryption-at-Rest)
- **Problem**: Host users without Docker run NineGuard with SQLite directly on disk. Plaintext `providers.api_key`, `providers.quota_auth_secret`, `settings.router_api_key`, and `plugins.secret` are exposed if the database file is copied or leaked.
- **Master Key Resolution**:
  1. Priority 1: `NINEGUARD_ENCRYPTION_KEY` environment variable (32 raw bytes or 64 hex characters).
  2. Priority 2: Auto-generated 32-byte cryptographically secure random key saved to `data/.master.key` (mode `0600`).
- **Cipher Specification**:
  - AES-256-GCM (Galois/Counter Mode) authenticated encryption.
  - Per-record random 12-byte nonce.
  - Storage format: `enc:v1:<base64(12-byte nonce || ciphertext || 16-byte GCM tag)>`.
- **Backward Compatibility & Automatic Migration**:
  - `vault.Decrypt(s)`: If string begins with `enc:v1:`, decrypt and verify auth tag. If not, return original string (transparent fallback).
  - On startup inside `migrate()`, scan all rows in `providers`, `settings`, and `plugins`. Any plaintext credential is encrypted and updated in-place. Raw secrets are removed from disk.

### 2. Infinite Loop Protection (Recursion Guard)
- Persist unique `instance_id` (UUID) in SQLite `settings` table on first boot.
- Outgoing proxy and upstream client calls attach:
  - `X-NineGuard-Hop: <count>`
  - `X-NineGuard-Trace: <instance_id>,<instance_id>...`
- If incoming request contains current `instance_id` in trace, or hop count exceeds 3, abort immediately with HTTP `508 Loop Detected`.

### 3. Deterministic Bucket Sorting
- Order buckets per connection:
  1. Warnings pinned to top (⚠️ `<25%` or exhausted).
  2. Core windows next (5h Session, 7d Weekly).
  3. Specific models alphabetically (`Claude...`, `Gemini...`, `GPT...`).
  4. Credit balances at bottom.

### 4. Anti-Spam Cooldown on Upstream Quota Fetch
- Backend: Enforce 30-second minimum cooldown on `force=1` per provider. If called within 30s, return fresh cache silently without hitting upstream.
- Frontend: Disable "Refresh" button with 30s countdown badge (`Cooldown 30s`).

### 5. Protocol Naming Generalization
- Rename UI label from `9router API` to `Router / Aggregator Protocol (9router, OmniRouter, CLI Proxy)`.
- Badge displays `Router Protocol`. Database value stays `9router`.

### 6. Desktop Provider Modal Layout
- Expand modal dialog on desktop (`max-width: 820px`) with 2-column grid layout:
  - Left column: Core connection (Name, Prefix, Target URL, API Key, Default checkbox).
  - Right column: Telemetry & Token Saving (Protocol selector, Password, Token saving options).

### 7. Quota Reset Action Button (`[🔄 N Reset]`)
- Detect Claude limit reset grants and Codex rate-limit reset credits from 9router payload (`resetCredits` / `grants`).
- Admin UI displays `[🔄 N Reset]` on connection card.
- Clicking opens confirmation modal: *"Spend 1 irreversible reset grant for <Account>?"*.
- NineGuard backend endpoint `POST /api/v1/upstream-quotas/{id}/connections/{connId}/reset` relays to 9router (`claude-reset` or `codex-reset-credits`) with stored dashboard auth, then re-fetches fresh quotas.

### 8. `/quota` Slash Command Interceptor Fix & Argument Filtering
- **Fix**: Support content blocks (`content: [{"type": "text", "text": "..."}]`) from Pi/Monocode/OpenWebUI.
- **Argument Filtering**:
  - `/quota` or `/limits` → Full report.
  - `/quota key` → Only client key token budget.
  - `/quota upstream` → Only upstream pools.
  - `/quota <filter>` (e.g. `/quota gemini`) → Filter pools to matching name.

---

## 2. API Endpoints

- `POST /api/v1/upstream-quotas/{id}/connections/{connId}/reset`
  - Auth: Admin session.
  - Action: Dispatches reset grant to upstream 9router and invalidates cache.
