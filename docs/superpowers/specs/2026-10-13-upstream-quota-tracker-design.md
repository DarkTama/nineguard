# Upstream Quota Tracker and Chat Command Interception — Design Spec

- Date: 2026-10-13
- Status: Draft / Approved Design
- Related: `GLOSSARY.md` (Upstream Quota, Provider Quota Type, Cascaded Quota, Merged Account Pool, Chat Command Interceptor), `docs/adr/0009-upstream-quota-tracking-and-interception.md`

---

## 1. Problem

NineGuard proxies AI completions to diverse upstream backends:
1. Multi-account aggregators like **9router** with dynamic rolling quota ceilings (5-hour session buckets, weekly allowances, credit balances).
2. **Cascaded NineGuard gateways** (e.g. `my-nineguard -> comp-nineguard -> comp-9router`).
3. **Direct LLM providers** (e.g. NanoGPT, OpenAI) without usage limit APIs.

Currently, NineGuard tracks internal token consumption (Key/Model Quotas) but has no visibility into upstream capacities. When upstream accounts deplete (429 / credit exhaustion), users and AI agents fail abruptly without warning. Furthermore, agent developers working inside IDEs (Cursor, Cline) lack an in-band way to inspect remaining quotas without leaving their chat workspace.

---

## 2. Scope

### In Scope
- Provider metadata column `quota_type` (`disabled`, `9router`, `nineguard`) and optional `quota_auth_secret` in `providers`.
- Upstream fetch engine for 9router (`/api/providers/client` and `/api/usage/[connectionId]`) with automatic session auth.
- Cascaded NineGuard client protocol via `GET /v1/quota`.
- In-memory quota cache with 60s TTL and explicit force-refresh.
- Privacy-preserving **Merged Account Pool** calculation for client access (summing capacity, hiding individual accounts and emails).
- Management endpoint `GET /api/v1/upstream-quotas` for NineGuard Admin UI.
- Client REST endpoint `GET /v1/quota` (scoped to the calling API key's model permissions).
- Chat Command Interceptor for `/quota`, `/limits`, `/usage` in `/v1/chat/completions` (0 tokens, immediate Markdown table response, stream/non-stream support).
- Web UI: Sub-tab in `/quotas` view for Upstream Quotas, per-provider cards, progress bars, and near-depletion warnings (<25%).

### Out of Scope
- Automatic failover / rerouting between multiple accounts (9router already handles account cycling internally).
- Direct scraping of third-party websites without an API.
- Modification of 9router codebase.
- Cost/dollar balance synchronization with payment processors (Stripe, etc.).

---

## 3. Schema & Migrations

Idempotent migrations in `migrate()` (`internal/db/db.go`):

```sql
-- Track integration protocol for quota checking per provider
ALTER TABLE providers ADD COLUMN quota_type TEXT DEFAULT 'disabled'; -- 'disabled' | '9router' | 'nineguard'

-- Optional auth password / secret for quota API (e.g. 9router dashboard password)
ALTER TABLE providers ADD COLUMN quota_auth_secret TEXT DEFAULT '';

-- Global toggle for slash command interception in chat completions
INSERT OR IGNORE INTO settings (key, value) VALUES ('chat_commands_enabled', 'true');
```

---

## 4. Domain Types (`internal/upstream`)

```go
package upstream

import "time"

// Normalized individual quota window (e.g. 5h session, weekly, balance)
type QuotaBucket struct {
    Name                string     `json:"name"`                 // e.g. "5h Session", "Weekly", "Credits"
    Used                int64      `json:"used"`
    Total               int64      `json:"total"`
    Remaining           int64      `json:"remaining"`
    RemainingPercentage int        `json:"remaining_percentage"` // 0 - 100
    ResetAt             *time.Time `json:"reset_at,omitempty"`
    IsCreditBalance     bool       `json:"is_credit_balance"`
    Warning             bool       `json:"warning"`              // true if remaining_percentage < 25
}

// Single upstream account connection (Admin view)
type ConnectionQuota struct {
    ConnectionID string        `json:"connection_id"`
    Provider     string        `json:"provider"`             // e.g. "anthropic", "google", "openai"
    AccountLabel string        `json:"account_label"`        // Sanitized/masked or raw for admin
    Plan         string        `json:"plan,omitempty"`
    Buckets      []QuotaBucket `json:"buckets"`
    Status       string        `json:"status"`               // "ok", "degraded", "error"
    ErrorMsg     string        `json:"error_msg,omitempty"`
    LastUpdated  time.Time     `json:"last_updated"`
}

// Merged view for client API keys & chat prompts
type MergedPoolQuota struct {
    Provider            string        `json:"provider"`             // e.g. "9router", "nineguard kantor"
    Family              string        `json:"family"`               // e.g. "Gemini", "Claude & GPT"
    Buckets             []QuotaBucket `json:"buckets"`
    AllowedModels       []string      `json:"allowed_models"`
    Status              string        `json:"status"`
    Warning             bool          `json:"warning"`
}

// Unified response for GET /v1/quota
type UnifiedQuotaResponse struct {
    KeyQuota      *ClientKeyBudget  `json:"key_quota,omitempty"`
    ModelQuotas   []ModelQuotaUsage `json:"model_quotas,omitempty"`
    UpstreamPools []MergedPoolQuota `json:"upstream_pools"`
}
```

---

## 5. Merged Account Pool Calculation

When multiple connections belong to the same provider and quota family:
1. **Summed Limits**:
   $$\text{Total}_{\text{pool}} = \sum \text{Total}_i$$
   $$\text{Used}_{\text{pool}} = \sum \text{Used}_i$$
   $$\text{RemainingPercentage}_{\text{pool}} = \text{round}\left(\frac{\text{Total}_{\text{pool}} - \text{Used}_{\text{pool}}}{\text{Total}_{\text{pool}}} \times 100\right)$$
2. **Earliest Reset**:
   $$\text{ResetAt}_{\text{pool}} = \min(\text{ResetAt}_i) \quad \text{for active countdowns}$$
3. **Privacy Masking**:
   - Connection count, connection IDs, and account emails are stripped completely from client responses.

---

## 6. Endpoints & REST API

### Admin API: `GET /api/v1/upstream-quotas`
- **Auth**: NineGuard Admin session.
- **Query Param**: `force=1` (bypass 60s memory cache).
- **Response**: Array of providers with raw `ConnectionQuota` items, diagnostic error strings, and individual account cards.

### Client API: `GET /v1/quota`
- **Auth**: NineGuard API Key (`Bearer sk-ng-...`).
- **Access Filter**: Only includes models and upstream pools allowed by the key's Model Groups.
- **Response**: `UnifiedQuotaResponse` (Client Key/Model usage + Merged Upstream Pools).

---

## 7. Chat Command Interception Pipeline

In `internal/proxy/proxy.go` on `POST /v1/chat/completions`:

1. **Pre-Flight Hook**:
   - Check if `chat_commands_enabled == "true"`.
   - Inspect JSON body: check if the last message in `messages` array has:
     ```
     role == "user" && strings.TrimSpace(content) in ["/quota", "/limits", "/usage"]
     ```
2. **Short-Circuit Execution**:
   - If match found, do NOT forward to upstream provider.
   - Fetch key's allowed models and merged upstream quotas.
   - Render clean Markdown table:
     ```markdown
     📊 **NineGuard Quota Report**

     **API Key Budget:** 450,000 / 1,000,000 tokens remaining (Daily, resets in 3h 12m)

     ### Upstream Model Pools
     | Pool / Family | Window | Used / Total | Remaining | Next Reset |
     |---|---|---|---|---|
     | 9router / Claude & GPT | 5h Session | 0 / 2,000 | 100% | in 4h 19m |
     | 9router / Gemini | Weekly | 1,555 / 2,000 | ⚠️ 22% | in 22h 12m |
     | nineguard kantor | Daily | 200k / 500k | 60% | in 5h 00m |
     ```
3. **Response Emulation**:
   - If `stream: false`: Return `chat.completion` JSON object with assistant message.
   - If `stream: true`: Yield SSE chunks (`chat.completion.chunk`) followed by `[DONE]`.

---

## 8. Web UI Specification

1. **Tab in `/quotas`**:
   - Toggle buttons: `[Client Quotas]` and `[Upstream Quotas]`.
2. **Upstream Quotas View**:
   - Provider summary cards with health tags:
     - `9router` (Green badge: Direct Aggregator)
     - `nineguard kantor` (Blue badge: Cascaded Gateway)
     - `NanoGPT` (Gray badge: Direct API - No Quota Tracker)
   - Connection cards matching 9router visual style:
     - Color-coded progress bars (Green >70%, Yellow 30-70%, Red <30%).
     - Countdown timer badges (`in 3h 22m`).
     - Manual refresh icon button per provider.
3. **Provider Edit Modal**:
   - Add dropdown: `Quota Tracker Protocol`:
     - `Disabled`
     - `9router API` (shows password field for dashboard authentication)
     - `NineGuard Cascaded` (uses configured upstream API key)

---

## 9. Error Handling & Degradation

| Scenario | Behavior |
|---|---|
| Upstream timeout (>5s) | Mark pool `status: "degraded"`, serve stale cache if available, log warning. |
| 9router Auth 401 | Mark pool `status: "error"`, UI prompts: *"Check 9router dashboard password"*. Normal proxying unaffected. |
| Chat prompt not exact `/quota` | Normal chat proxy forwarding (no false-positive command execution). |
| All upstream accounts 0% | Quota displays 0% with red alert ⚠️. Upstream 9router handles circuit-breaking. |
