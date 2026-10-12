# Per-Model Quotas per API Key — Design Spec

- Date: 2026-10-12
- Status: Implemented
- Related: `GLOSSARY.md` (Key Quota, Model Quota, Quota Mode, Requested Model, Inactive Model Quota, Near-Limit Quota), `docs/adr/0005-token-quotas-utc-windows.md`, `docs/adr/0007-model-quotas-per-key.md`, `docs/adr/0008-resolved-model-id-in-traffic.md`, mockup `docs/mockups/model-quotas.html` (approved)

## 1. Problem

Each API Key has one Key Quota shared by every model. Admins need a separate token budget per person per model (Ari: model A 1M/day, model B 20M/week), plus a page to monitor them. Decisions are recorded in ADR 0007/0008; this spec fixes the concrete schema, request path, API, and UI.

## 2. Scope

### In scope

- `quota_mode` per API Key (`key` | `model`) and a Model Quota table.
- `traffic_logs.model_id` (resolved Model ID) written on every request that resolves a provider.
- Model Quota enforcement in the proxy with its own 429.
- Key create/update API carries mode + Model Quotas atomically.
- Per-model approximate velocity endpoint.
- `GET /api/v1/quotas` for the Quotas page and the dashboard card.
- `quota_warn_percent` setting (default 80).
- UI: key modal Quota section, new Quotas page, dashboard Near-limit card, keys table Quota column, label rename "Token Quota" → "Key Quota".

### Out of scope

- Pattern/family pools (e.g. one budget for all Claude models).
- Quotas on Model Groups or global per-model defaults.
- Backfilling `model_id` on old traffic rows.
- Cost ($) based limits.
- Changing how access control or the disabled check match model names (§11.2).

## 3. Schema

All in `migrate()` (`internal/db/db.go`), idempotent like existing migrations.

```sql
ALTER TABLE api_keys ADD COLUMN quota_mode TEXT DEFAULT 'key';   -- 'key' | 'model'

CREATE TABLE IF NOT EXISTS api_key_model_quotas (
  api_key_id   TEXT NOT NULL,
  model_id     TEXT NOT NULL COLLATE NOCASE,
  quota_limit  INTEGER NOT NULL CHECK (quota_limit > 0),
  quota_period TEXT NOT NULL,            -- daily | weekly | monthly | total
  created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (api_key_id, model_id)
);

ALTER TABLE traffic_logs ADD COLUMN model_id TEXT COLLATE NOCASE;
CREATE INDEX IF NOT EXISTS idx_traffic_key_model_ts ON traffic_logs(api_key_id, model_id, timestamp);
```

- `quota_period` for Model Quotas excludes `none`: a row with no reset is `total` (one-time budget); "unlimited" is expressed by having no row.
- `NOCASE` matches the existing case-insensitive model matching in `keys.matchModelPattern`.
- `DeleteKey` deletes the key's Model Quota rows in the same transaction (SQLite foreign keys are not enforced in this DB).
- No backfill. Existing keys read `quota_mode = 'key'`.
- `quota_warn_percent` lives in `settings` (default 80, valid 1–100).

## 4. Domain types (`internal/keys`)

```go
type ModelQuota struct {
    ModelID        string  `json:"model_id"`
    QuotaLimit     int64   `json:"quota_limit"`
    QuotaPeriod    string  `json:"quota_period"`
    // read-only, filled on read:
    Usage          int64   `json:"usage"`
    ResetAt        *string `json:"reset_at"`        // ISO UTC, null for total
    Inactive       bool    `json:"inactive"`
    InactiveReason string  `json:"inactive_reason,omitempty"` // no_access | disabled | removed
}
```

`KeyInfo` gains:

- `QuotaMode string` (`json:"quota_mode"`, `key` | `model`).
- `ModelQuotas []ModelQuota` (`json:"model_quotas"`).

`loadLocalCache` and the SQLite fallback in `ValidateClientKey` load `quota_mode` and the key's Model Quota rows (limit + period only), so the proxy needs no extra query to find a row. The fallback currently omits quota fields; it is fixed to load `quota_limit`, `quota_period`, `quota_mode`, and Model Quotas.

Lookup: `(*KeyInfo).ModelQuotaFor(modelID string) *ModelQuota` — case-insensitive exact match.

## 5. Model ID resolution

New function in `internal/providers`:

```go
// ResolveModelID returns the canonical Model ID for a Requested Model:
// "<prefix>/<actual>" when the routed provider has a prefix, otherwise "<actual>".
func (m *Manager) ResolveModelID(requested string) (provider *Provider, actualModel, modelID string, err error)
```

Built on `FindProviderForModel`, so routing behaviour is unchanged. Examples, with default provider `9router` (prefix `9router`):

| Requested Model | Model ID |
|---|---|
| `9router/gpt-4o` | `9router/gpt-4o` |
| `gpt-4o` | `9router/gpt-4o` |
| `openai/gpt-4o` (provider `openai` exists) | `openai/gpt-4o` |

A Model ID is **canonical** when `ResolveModelID(id).modelID == id` (case-insensitive). Only canonical IDs can be saved as Model Quotas.

## 6. Request path (`internal/proxy/proxy.go`)

Order after this change:

1. Key auth (unchanged).
2. `GET /v1/models` (unchanged).
3. **Key Quota check**: runs only when `QuotaMode == "key"`; otherwise identical to today (pre-body).
4. Read and parse body.
5. Access check (403 `model_not_allowed`): unchanged, uses Requested Model.
6. Disabled check (403 `model_disabled`): unchanged, uses Requested Model.
7. Provider resolution via `ResolveModelID` (502 `no_provider` unchanged). Replaces the current `FindProviderForModel` call; same position.
8. **Model Quota check** (new): when `QuotaMode == "model"`, the request has a non-empty model, and `ModelQuotaFor(modelID)` returns a row:
   - `consumed = GetModelQuotaUsage(keyID, modelID, period, now)`.
   - If `consumed >= limit` → 429 (§7), log, return.
   - Otherwise continue. Admitted requests run to completion (Soft Post-Facto, ADR 0005).
9. Body rewrite, plugins, upstream (unchanged).

Requests without a `model` field skip step 8 (no Model to count against).

`LogEntry` gains `ModelID string`; every `Record` call made after step 7 sets it, including the success/stream paths and plugin rejections. `Record` writes it to `traffic_logs.model_id` (NULL when empty). Entries before step 7 (401, Key Quota 429, 403s) leave it NULL.

## 7. 429 responses

Model Quota exceeded:

```
HTTP 429
Content-Type: application/json
Retry-After: <seconds until reset, min 1>     (omitted for period total)

{"error":{
  "message":"Model quota exceeded for '9router/claude-opus-4' (1,004,211 / 1,000,000 tokens daily). Resets in 6h 12m (at 07:00 WIB / 00:00 UTC).",
  "type":"insufficient_quota",
  "code":"model_quota_exceeded",
  "param":"model"
}}
```

- Token and reset formatting reuse the existing Key Quota code, extracted into one helper `quotaExceededMessage(prefix, consumed, limit, period, resetAt)` used by both checks.
- Period `total`: message ends "Lifetime quota, does not reset." and no `Retry-After`. This also fixes the Key Quota `total` case, which today reports a reset in year 9999.
- Traffic log: `Model` = Requested Model, `ModelID`, `ProviderID`, status 429, level WARN, tokens 0, `ErrorMessage` = `model quota exceeded (<used>/<limit> tokens <period>) for <modelID>`.

The Key Quota 429 body and code (`quota_exceeded`) are otherwise unchanged.

## 8. Usage queries (`internal/traffic`)

```go
// Exact; used for enforcement and all displayed usage.
GetModelQuotaUsage(d, apiKeyID, modelID, period string, now time.Time) (consumed int64, resetAt time.Time, err error)
// SELECT COALESCE(SUM(total_tokens),0) FROM traffic_logs
//  WHERE api_key_id = ? AND model_id = ? AND timestamp >= ?

// Approximate; only for the editor's velocity hints (ADR 0008).
GetModelVelocityStats(apiKeyID, modelID string, bareName string, now time.Time) (*VelocityStats, error)
// rows where model_id = ?  OR (model_id IS NULL AND (model = ? COLLATE NOCASE OR model = ? COLLATE NOCASE))
```

`bareName` is the Model ID without its provider prefix, passed only when that provider is the current default (bare requests went there). Same 24h / 7d avg / 30d logic as `GetVelocityStats`.

## 9. Inactive Model Quotas

A row is Inactive when, for the key's **current** access settings:

| Reason | Condition |
|---|---|
| `removed` | model id missing from `models`, or `removed = 1` |
| `disabled` | model row exists with `enabled = 0` |
| `no_access` | `!keyInfo.IsModelAllowed(modelID)` |

Checked in the order `removed` → `disabled` → `no_access`; first match wins. The disabled lookup must be read-only: `models.IsModelEnabled` auto-registers unknown IDs, so `models.LookupStatus(db, id) (found, enabled, removed bool)` (and `(*Manager).Status`) is used. An id missing from `models` counts as `removed`.

Enforcement does not need this: a request for a `no_access` or `disabled` Model is already rejected at steps 5–6. Inactive is a read-side state for API/UI. Inactive rows report `usage` but are excluded from a key's "max %" and from Near-Limit lists.

## 10. API

### 10.1 Key create/update

`POST /api/v1/keys` and `PUT|POST /api/v1/keys/{id}` accept two new optional fields:

```json
{
  "quota_mode": "model",
  "model_quotas": [
    {"model_id": "9router/claude-opus-4", "quota_limit": 1000000, "quota_period": "daily"},
    {"model_id": "openai/gpt-4o",          "quota_limit": 500000,  "quota_period": "monthly"}
  ]
}
```

- `quota_mode` absent → unchanged (create: `key`). Invalid → 400.
- `model_quotas` absent → rows unchanged. Present (even `[]`) → **replaces** the key's full set.
- `quota_limit`, `quota_period` (Key Quota) keep current behaviour and are stored regardless of mode (inactive mode is kept, ADR 0007).
- Everything (key fields, access settings, mode, Key Quota, Model Quota set) is written in **one transaction**. Today `UpdateKey` and `UpdateKeyQuota` are separate writes; they are combined.

Row validation (400 with a message naming the offending model):

1. `quota_limit > 0`.
2. `quota_period` ∈ `daily|weekly|monthly|total`.
3. No duplicate `model_id` (case-insensitive) in the payload.
4. For rows whose `model_id` is **not already stored** on this key:
   - exists in `models` table and is not a Removed Model and is enabled;
   - is canonical (§5);
   - `IsModelAllowed` under the access settings **in the same payload** (or current ones if the payload omits them).

   Rows already stored are accepted as-is even if now Inactive, so editing a key never forces deleting an orphan.

Responses return the full `KeyInfo` including `quota_mode` and `model_quotas` with usage/inactive fields.

### 10.2 Key list

`GET /api/v1/keys` (legacy and paged) adds per key:

- `quota_mode`
- `model_quota_summary`: `{"count": 4, "active": 3, "top_model_id": "openai/gpt-4o", "top_percent": 102}` (null in key mode or with no rows). `top_*` from active rows only.

Full `model_quotas` are not included in list responses.

### 10.3 Per-model velocity

`GET /api/v1/keys/{id}/model-velocity?model=<modelID>` → `VelocityStats` + `"approximate": true`. Same auth as `/velocity`.

### 10.4 Quotas overview

`GET /api/v1/quotas` — same auth as `GET /api/v1/keys`.

```json
{
  "warn_percent": 80,
  "summary": {"keys_with_quota": 14, "model_quotas": 37, "near_limit": 5, "exhausted": 2},
  "keys": [
    {
      "id": "...", "name": "ari-laptop", "is_active": true, "quota_mode": "model",
      "top_percent": 102, "status": "exhausted",
      "key_quota": null,
      "model_quotas": [
        {"model_id": "openai/gpt-4o", "quota_limit": 500000, "quota_period": "monthly",
         "usage": 512000, "percent": 102, "reset_at": "2026-11-01T00:00:00Z",
         "status": "exhausted", "inactive_reason": ""}
      ]
    },
    {
      "id": "...", "name": "support-bot", "quota_mode": "key", "top_percent": 82, "status": "near",
      "key_quota": {"quota_limit": 5000000, "quota_period": "daily", "usage": 4100000, "percent": 82, "reset_at": "..."},
      "model_quotas": []
    }
  ]
}
```

- `status`: `ok` (< warn), `near` (≥ warn, < 100), `exhausted` (≥ 100), `inactive` (Model Quota rows only), `unlimited` (key with no enforced quota).
- `percent` is integer, not capped (can exceed 100).
- Keys sorted by `top_percent` desc, then name. Rows inside a key sorted the same, inactive last.
- Keys with status `unlimited` are included; the UI hides them by default. Inactive (disabled) API keys are included and flagged with `is_active`.
- Usage computed with one grouped query per key: `SELECT model_id, SUM(...)` per distinct period window, not one query per row.
- The dashboard card uses this same endpoint and filters `near`/`exhausted` client-side; no separate endpoint.

### 10.5 Settings

`GET/POST /api/v1/settings/traffic` gains `quota_warn_percent` (1–100, default 80).

## 11. UI

Layout per approved mockup `docs/mockups/model-quotas.html`.

### 11.1 Key modal — Quota section (`views/endpoints.js`)

- Section title "Quota (Rate Limiting)". Segmented control: **Key Quota (total, all models)** | **Per Model**.
- Key Quota: existing fields, velocity, 1.5x/2x buttons. Label text "Token Limit" → "Key Quota limit"; help text "Max tokens across all models. 0 = unlimited."
  - When Model Quotas are stored: collapsed line "N Model Quotas saved (inactive while in Key Quota mode)".
- Per Model: table Model | Limit | Reset period (Daily, Weekly, Monthly, Lifetime) | Used (current window) | remove.
  - Velocity line + 1.5x/2x buttons shown under the focused row only, labelled "approx.", loaded from §10.3 on focus.
  - Inactive rows greyed, badge with reason, "not enforced".
  - Empty state: "No Model Quotas. All models are unlimited for this key."
  - **+ Add Model** inline panel: search, grouped by Model Group, then "Other allowed models". Source: `GET /api/v1/models` filtered by enabled, not removed, canonical, and allowed under the modal's current access settings (client mirror of `matchModelPattern`). Already-added Models shown checked and disabled. Multi-select → "Add N rows" with empty limits; Save is blocked until each new row has a limit > 0.
  - Changing the access settings in the same modal re-evaluates which unsaved rows are allowed and flags them.
- One Save sends name, access, `quota_mode`, Key Quota, and `model_quotas` together (§10.1).

### 11.2 Keys table Quota column

- Key mode: unchanged bar.
- Model mode: bar + percent from `model_quota_summary.top_percent`, text `<short model> · N limits`. No active rows: "Per model · N inactive". No rows: "unlimited".

### 11.3 Quotas page (`views/quotas.js`, new)

- Nav: Gateway group, directly after "Endpoints & Keys"; label "Quotas"; keywords `quota limit per model usage budget near limit exhausted`; icon `chart` (existing `i-chart` symbol).
- Summary cards: Keys with quotas, Model Quotas, Near limit (≥ warn %), Exhausted.
- Filters: search (key name or model), Mode (All/Key Quota/Per Model), Status (All/Near limit/Exhausted/Inactive), "Show unlimited" toggle (off). Filter state in URL query like other views.
- Table grouped by key, expandable; model-mode keys start collapsed except those with status `near`/`exhausted`. Key-mode keys are single rows.
- Bar colours: green < warn, amber ≥ warn, red ≥ 100.
- Reset shown relative ("in 6h 12m") with absolute UTC + viewer-local in tooltip; Lifetime shows "never".
- Row click on key name opens the existing key edit modal.
- Auto-refresh with the app's existing refresh control; no polling of its own.

### 11.4 Dashboard card (`views/dashboard.js`)

"Quotas near limit": top 5 rows (Key Quota or Model Quota) with status `near`/`exhausted`, exhausted first then by percent. Hidden when none. "View all →" opens Quotas with Status = Near limit.

### 11.5 Settings

`quota_warn_percent` input in the existing traffic settings dialog (`views/traffic.js`) next to `heavy_token_threshold`.

### 11.6 Known limitation, unchanged by this spec

Access control and the disabled check still compare the Requested Model, so disabling `9router/gpt-4o` does not block a bare `gpt-4o` request. Model Quotas are not affected because they use the resolved Model ID. Tracked separately.

## 12. Testing

Go tests, in-memory SQLite like existing `quota_*_test.go`.

**traffic**

- `GetModelQuotaUsage` counts only matching `api_key_id` + `model_id` inside the window; case-insensitive; other models/keys excluded.
- Window boundaries daily/weekly/monthly/total reuse `CalcQuotaWindow` (existing tests extended for `total` reset handling).
- `GetModelVelocityStats` includes legacy rows matched by raw `model` only when `model_id` is NULL.

**providers**

- `ResolveModelID`: prefixed, bare → default provider prefix, provider without prefix, no providers → error.

**keys**

- Migration: existing keys read `quota_mode = key`, no Model Quotas.
- Replace semantics: `model_quotas` absent keeps rows; `[]` clears; set replaced atomically; failure rolls back key fields too.
- Validation: limit ≤ 0, bad period, duplicate (case-insensitive), unknown model, non-canonical bare ID, Removed Model, disabled, not allowed — each 400; already-stored Inactive row accepted unchanged.
- Switching mode keeps the other mode's settings.
- Inactive reason ordering; `IsModelEnabled` not called (no auto-register) during reads.
- `DeleteKey` removes Model Quotas.
- Cache: `ValidateClientKey` (cached and fallback paths) returns `QuotaMode` and Model Quotas.

**proxy**

- Model mode, under limit → forwarded; `traffic_logs.model_id` set to the resolved ID.
- Model mode, at/over limit → 429 `model_quota_exceeded`, `Retry-After` correct, log row has `model_id`, other Model on same key still forwarded.
- Bare Requested Model counts against the prefixed Model Quota.
- Model without a row → unlimited.
- Model mode ignores Key Quota even if exhausted; key mode ignores Model Quotas even if exhausted.
- Period `total`: no `Retry-After`, "does not reset" message (both modes).
- Access 403 still precedes the Model Quota check.

**handler**

- `GET /api/v1/quotas`: statuses, sorting, summary counts, inactive excluded from `top_percent`, unlimited keys present.
- `model_quota_summary` in key list.
- `model-velocity` returns `approximate: true`.
- `quota_warn_percent` validation 1–100.

**UI** — manual check against the mockup in both themes: create key in model mode, add rows via picker, save, hit limit with a test client, verify 429 and Quotas page/dashboard card/keys column.

## 13. Delivery

One branch, three commits-worth of phases; each phase ships green tests.

1. **Data + enforcement**: schema, `ResolveModelID`, `model_id` logging, `KeyInfo` fields + cache, usage query, proxy check, 429 helper.
2. **API**: atomic create/update with validation, list summary, `/quotas`, `/model-velocity`, settings.
3. **UI**: key modal, keys column, Quotas page, dashboard card, settings input, label rename.

Docs: update `docs/ARCHITECTURE.md` §2 (Request Lifecycle) and `docs/FUTURE_PLANNED_FEATURES.md` §3 status after phase 3.

## 14. Implementation notes (where the build differs from the text above)

The build is authoritative; these points replace the matching text above.

1. **Message and headers (§7).** Token counts are comma-formatted (`1,004,211 / 1,000,000`). The 429 body is built with `json.Marshal`; `param: "model"` appears for Model Quotas only. Period `total` sends no `Retry-After`, for Key Quota and Model Quota alike.
2. **UTC timestamps (§8).** `GetModelUsageSince` compares in UTC (`start.UTC().Format(...)`); stored timestamps are UTC and a local-time start silently returns nothing.
3. **Access fields on save (§10.1).** Access fields come from the request payload, as `UpdateKey` always did; new rows are checked against the payload's access. An unknown key on update stays HTTP 400. `UpdateKeyAll` runs in one transaction; absent `model_quotas` keeps rows, present (even `[]`) replaces them.
4. **Key reads (§10.2).** List responses carry `model_quota_summary` only. `GET /api/v1/keys/{id}` is new and returns the full key including `model_quotas`.
5. **Overview (§10.4).** `key_quota` also carries `status`. A model-mode key whose rows are all inactive has status `unlimited`. `summary.model_quotas` counts every row of model-mode keys; `near_limit` and `exhausted` count enforced rows only.
6. **Add Model (§11.1).** The picker is an inline panel. Its list is filtered client-side from `/providers` (mirrors `ResolveModelID`). Limit inputs use `step=1`. Velocity history is its own full-width row.
7. **Quotas page (§11.3).** The key name links to `#/endpoints?edit=<id>`; Endpoints opens the modal and clears the parameter.
8. **Provider prefix match is case-sensitive.** `OPENAI/gpt-4o` is not a canonical id; `openai/GPT-4o` is (model part compares case-insensitively in the database).
9. **Bare unknown model.** A bare name with no `models` row is rejected as "unknown model" (400) on save, with no canonical hint.
10. **Traffic API.** `GET /api/v1/traffic` does not expose `model_id` in log JSON (it is stored in the database). Surfacing it is a follow-up.
