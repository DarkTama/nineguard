# Upstream Quota Tracker and Chat Command Interception Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide visibility into upstream provider capacities (e.g. 9router rolling 5h/weekly limits, cascaded company NineGuard gateways) in the NineGuard Admin UI, via client REST API (`GET /v1/quota`), and via in-band slash command interception (`/quota`) in chat completions at zero token cost.

**Architecture:** 
- New package `internal/upstream` handles protocol adapters for `9router`, `nineguard`, and standard `disabled` providers with an in-memory 60s TTL cache.
- Account aggregation logic merges multi-account pools for client agents (summing used/total capacity, hiding account counts and emails).
- Proxy hook in `internal/proxy/proxy.go` intercepts `/quota` prompts in `/v1/chat/completions` and returns synthetic Markdown completions.
- Web UI provides an "Upstream Quotas" sub-tab in `/quotas` matching 9router visual styles.

**Tech Stack:** Go 1.22+, SQLite (`modernc.org/sqlite`, single connection), vanilla ES modules, `node --test`.

**Related Specs:** `docs/superpowers/specs/2026-10-13-upstream-quota-tracker-design.md`, `docs/adr/0009-upstream-quota-tracking-and-interception.md`, `GLOSSARY.md`.

---

## Plan Structure

- [ ] **Task 1: Schema Migrations & Provider Data Model**
  - Update SQLite schema in `internal/db/db.go`.
  - Extend `Provider` struct and CRUD methods in `internal/providers/providers.go`.
  - Add unit tests for migrations and provider CRUD.

- [ ] **Task 2: Upstream Quota Client Engine**
  - Create package `internal/upstream/client.go` and `internal/upstream/types.go`.
  - Implement 9router client adapter (connection discovery, usage fetch, auto-login session cookie).
  - Implement cascaded NineGuard client adapter (`GET /v1/quota`).
  - Implement 60s in-memory TTL caching with force-refresh bypass.
  - Add unit tests with mock HTTP upstreams.

- [ ] **Task 3: Merged Account Pool & Privacy Scoping**
  - Implement `MergeAccountPools` in `internal/upstream/pool.go`.
  - Implement model-group access filtering for client API keys.
  - Implement privacy sanitizer (scrub emails, connection IDs, and account counts).
  - Add unit tests for summing math, earliest reset calculation, and privacy scrubbing.

- [ ] **Task 4: REST API Endpoints & Admin Handlers**
  - Add Admin API `GET /api/v1/upstream-quotas` in `internal/handler/handler.go`.
  - Add Client API `GET /v1/quota` with key authentication.
  - Update Provider create/update handlers to accept `quota_type` and `quota_auth_secret`.
  - Add handler tests in `internal/handler/`.

- [ ] **Task 5: Chat Command Interceptor in Proxy**
  - Implement slash prompt detector in `internal/proxy/proxy.go` for `/quota`, `/limits`, `/usage`.
  - Implement synthetic OpenAI response generator (JSON and SSE chunks).
  - Add setting check for `chat_commands_enabled`.
  - Add proxy unit tests verifying zero token consumption and response formatting.

- [ ] **Task 6: Web UI Integration**
  - Add "Upstream Quotas" sub-tab to `web/static/js/views/quotas.js`.
  - Implement provider cards, progress bars, reset timers, and manual refresh buttons.
  - Add Quota Protocol selector and Auth Secret field to Provider modal in `web/static/js/views/providers.js`.
  - Add frontend unit tests in `web/jstest/`.

- [ ] **Task 7: End-to-End Verification & Sanity Check**
  - Run full test suite: `CGO_ENABLED=0 go test ./...`.
  - Run frontend test suite: `node --test web/jstest/*.mjs`.
  - Test live integration via `curl` against local mock instances.

---

### Task 1: Schema Migrations & Provider Data Model

**Files:**
- Modify `internal/db/db.go`
- Modify `internal/providers/providers.go`
- Modify `internal/providers/providers_test.go`
- Create `internal/db/upstream_quota_migration_test.go`

**Details:**
1. In `migrate()` (`internal/db/db.go`), add idempotent columns:
   ```sql
   ALTER TABLE providers ADD COLUMN quota_type TEXT DEFAULT 'disabled';
   ALTER TABLE providers ADD COLUMN quota_auth_secret TEXT DEFAULT '';
   INSERT OR IGNORE INTO settings (key, value) VALUES ('chat_commands_enabled', 'true');
   ```
2. In `internal/providers/providers.go`:
   - Add fields to `Provider` struct:
     ```go
     QuotaType       string `json:"quota_type"`
     QuotaAuthSecret string `json:"-"` // Never expose in public JSON
     HasAuthSecret   bool   `json:"has_auth_secret"`
     ```
   - Update `ListProviders`, `GetProvider`, `CreateProvider`, and `UpdateProvider` queries and scanning.
3. Write test verifying database migration on existing databases and CRUD persistence.

---

### Task 2: Upstream Quota Client Engine

**Files:**
- Create `internal/upstream/types.go`
- Create `internal/upstream/client.go`
- Create `internal/upstream/router9.go`
- Create `internal/upstream/nineguard.go`
- Create `internal/upstream/client_test.go`

**Details:**
1. In `types.go`: Define `QuotaBucket`, `ConnectionQuota`, `ProviderQuotaResult`.
2. In `router9.go`:
   - Fetch `/api/providers/client` to get connections.
   - For each connection, fetch `/api/usage/{id}?force={1|0}`.
   - Parse buckets (5h session, weekly, credit balance).
   - If HTTP 401 received and `quota_auth_secret` exists, execute `POST /api/auth/login`, retain session cookie in memory, retry request.
3. In `nineguard.go`:
   - Fetch `/v1/quota` using configured upstream API key (`Bearer sk-ng-...`).
4. In `client.go`:
   - `Manager` struct with `sync.RWMutex` cache map: `providerID -> { data, expiresAt }`.
   - Method `GetProviderQuotas(ctx, provider, force bool) (*ProviderQuotaResult, error)`.
   - Default cache TTL = 60 seconds.
5. Unit tests with `httptest.Server` mocking 9router and NineGuard responses.

---

### Task 3: Merged Account Pool & Privacy Scoping

**Files:**
- Create `internal/upstream/pool.go`
- Create `internal/upstream/pool_test.go`

**Details:**
1. `MergeAccountPools(connections []ConnectionQuota) []MergedPoolQuota`:
   - Group buckets by family name (e.g. "5h Session", "Weekly").
   - Sum `Used` and `Total`.
   - Compute `RemainingPercentage = round((Total - Used) / Total * 100)`.
   - Set `ResetAt` to minimum `ResetAt` among active connections.
   - Set `Warning = true` if `RemainingPercentage < 25`.
2. `FilterPoolsByModelAccess(pools []MergedPoolQuota, allowedModels []string) []MergedPoolQuota`:
   - Exclude pools that do not serve any of the models permitted for the given key.
3. Unit test with test vectors verifying exact sums and privacy stripping.

---

### Task 4: REST API Endpoints & Admin Handlers

**Files:**
- Modify `internal/handler/handler.go`
- Create `internal/handler/upstream_quota.go`
- Create `internal/handler/upstream_quota_test.go`

**Details:**
1. Admin Handler `GET /api/v1/upstream-quotas`:
   - Reads all active providers from `providers.Manager`.
   - Concurrently fetches quotas using `upstream.Manager`.
   - Returns full diagnostic array with connections, status, and raw errors.
2. Client Handler `GET /v1/quota`:
   - Authenticates NineGuard API key (`sk-ng-...`).
   - Retrieves key's current token budget (from `traffic.Manager`).
   - Retrieves key's Model Group permissions.
   - Fetches and merges upstream pools filtered to allowed models.
   - Returns `UnifiedQuotaResponse`.
3. Update Provider handlers in `handler.go`:
   - Accept `quota_type` and `quota_auth_secret` in `CreateProvider` and `UpdateProvider`.
4. Unit tests covering both endpoints with unauthorized and authorized requests.

---

### Task 5: Chat Command Interceptor in Proxy

**Files:**
- Modify `internal/proxy/proxy.go`
- Create `internal/proxy/chat_command.go`
- Create `internal/proxy/chat_command_test.go`

**Details:**
1. In `chat_command.go`:
   - `IsChatCommand(messages []Message) (command string, isCmd bool)`.
   - Match exact trimmed `/quota`, `/limits`, `/usage`.
   - `FormatQuotaMarkdown(quota UnifiedQuotaResponse) string`.
2. In `proxy.go`:
   - Pre-check on `/v1/chat/completions`: if enabled and `IsChatCommand`:
     - Short-circuit immediately.
     - Build Markdown report.
     - If `stream == true`: Send SSE `chat.completion.chunk` events and `data: [DONE]`.
     - If `stream == false`: Send `chat.completion` JSON response.
3. Unit test verifying that chat prompt `/quota` never contacts upstream mock LLM and returns correct markdown.

---

### Task 6: Web UI Integration

**Files:**
- Modify `web/static/js/views/quotas.js`
- Modify `web/static/js/views/providers.js`
- Create `web/static/js/views/upstream_quotas.js`
- Create `web/jstest/upstream_quota_test.mjs`

**Details:**
1. In `quotas.js`:
   - Add tab switcher at top: `Client Quotas` (existing) | `Upstream Quotas` (new).
2. In `upstream_quotas.js`:
   - Render provider cards with status pill, protocol badge, and refresh button.
   - Render quota rows with color-coded progress bars (`ok` >70%, `warn` 30-70%, `danger` <30%).
   - Display countdown badges and error callouts.
3. In `providers.js`:
   - Add Quota Protocol selector (`Disabled`, `9router API`, `NineGuard Cascaded`) in provider edit modal.
   - Conditional input for 9router Dashboard Password.
4. Unit test in Node test runner testing quota percentage calculation and time formatting.

---

### Task 7: End-to-End Verification & Sanity Check

**Details:**
1. Run all Go tests:
   ```bash
   CGO_ENABLED=0 go test ./...
   ```
2. Run all frontend tests:
   ```bash
   node --test web/jstest/*.mjs
   ```
3. Test curl requests against local NineGuard:
   - `curl -H "Authorization: Bearer sk-ng-..." http://localhost:8080/v1/quota`
   - `curl -X POST http://localhost:8080/v1/chat/completions -d '{"messages":[{"role":"user","content":"/quota"}]}'`
