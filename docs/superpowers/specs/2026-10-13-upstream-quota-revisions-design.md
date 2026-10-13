# Upstream Quota Tracker Revisions — Design Spec

- Date: 2026-10-13
- Status: Approved Design
- Related: `GLOSSARY.md`, ADR 0009, `docs/superpowers/specs/2026-10-13-upstream-quota-tracker-design.md`

---

## 1. Scope of Revisions

1. **Infinite Loop Protection (Recursion Guard)**:
   - Persist unique `instance_id` (UUID) in SQLite `settings` table on first boot.
   - Outgoing proxy and upstream client calls attach:
     - `X-NineGuard-Hop: <count>`
     - `X-NineGuard-Trace: <instance_id>,<instance_id>...`
   - If incoming request contains current `instance_id` in trace, or hop count exceeds 3, abort immediately with HTTP `508 Loop Detected`.

2. **Deterministic Bucket Sorting**:
   - Order buckets per connection:
     1. Warnings pinned to top (⚠️ `<25%` or exhausted).
     2. Core windows next (5h Session, 7d Weekly).
     3. Specific models alphabetically (`Claude...`, `Gemini...`, `GPT...`).
     4. Credit balances at bottom.

3. **Anti-Spam Cooldown on Upstream Quota Fetch**:
   - Backend: Enforce 30-second minimum cooldown on `force=1` per provider. If called within 30s, return fresh cache silently without hitting upstream.
   - Frontend: Disable "Refresh" button with 30s countdown badge (`Cooldown 30s`).

4. **Protocol Naming Generalization**:
   - Rename UI label from `9router API` to `Router / Aggregator Protocol (9router, OmniRouter, CLI Proxy)`.
   - Badge displays `Router Protocol`. Database value stays `9router`.

5. **Desktop Provider Modal Layout**:
   - Expand modal dialog on desktop (`max-width: 820px`) with 2-column grid layout:
     - Left column: Core connection (Name, Prefix, Target URL, API Key, Default checkbox).
     - Right column: Telemetry & Token Saving (Protocol selector, Password, Token saving options).

6. **Quota Reset Action Button (`[🔄 N Reset]`)**:
   - Detect Claude limit reset grants and Codex rate-limit reset credits from 9router payload (`resetCredits` / `grants`).
   - Admin UI displays `[🔄 N Reset]` on connection card.
   - Clicking opens confirmation modal: *"Spend 1 irreversible reset grant for <Account>?"*.
   - NineGuard backend endpoint `POST /api/v1/upstream-quotas/{providerId}/connections/{connId}/reset` relays to 9router (`claude-reset` or `codex-reset-credits`) with stored dashboard auth, then re-fetches fresh quotas.

7. **`/quota` Slash Command Interceptor Fix & Argument Filtering**:
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
