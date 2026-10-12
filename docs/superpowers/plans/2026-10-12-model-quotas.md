# Per-Model Quotas per API Key Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let each API Key enforce either one Key Quota or separate Model Quotas per exact Model, with a Quotas page, dashboard card, and keys-table column.

**Architecture:** `api_keys.quota_mode` (`key`|`model`) plus table `api_key_model_quotas`. The proxy resolves the Requested Model to a canonical Model ID (`<prefix>/<model>`), stores it in `traffic_logs.model_id`, and sums usage by `(api_key_id, model_id, timestamp)`. Soft Post-Facto gating (ADR 0005) is unchanged. UI is vanilla JS.

**Tech Stack:** Go 1.27, SQLite (`modernc.org/sqlite`, **one open connection**), vanilla ES modules, `node --test`.

**Spec:** `docs/superpowers/specs/2026-10-12-model-quotas-design.md`, plus ADR 0007, ADR 0008, `GLOSSARY.md`, mockup `docs/mockups/model-quotas.html`. Where this plan and the spec differ, **this plan wins** (see "Spec corrections", applied to the spec in Task 7).

## How this plan is built

The implementation was written and verified in a scratch copy of the repo before this plan was finalized. The verified code is stored next to the plan:

```
R=docs/superpowers/plans/model-quotas-reference
$R/new/<repo path>      new files, copy as-is to the same path in the repo
$R/patches/<name>.diff  edits to existing files (git apply --ignore-whitespace)
$R/e2e/                 headless-Chrome scripts used for the UI check (Task 6)
```

Each task: copy its new files, apply its patches, run its tests. Do not re-derive the code; if a patch does not apply because the file moved, re-apply that diff by hand and keep the behavior. `R/` is scaffolding and is deleted in Task 7.

Helper used in every task (run from the repo root, bash):

```bash
R=docs/superpowers/plans/model-quotas-reference
put() { for f in "$@"; do mkdir -p "$(dirname "$f")"; cp "$R/new/$f" "$f"; done; }
patch_() { for p in "$@"; do git apply --ignore-whitespace "$R/patches/$p.diff" || return 1; done; }
```

The repo uses CRLF working files with `autocrlf=true`; applied edits and copied files are normalized by git on commit.

## Verified so far (scratch copy)

- `CGO_ENABLED=0 go test -tags server ./...` all packages pass; `node --test web/jstest/*.mjs` 28 pass (18 existing + 10 new).
- Real server + fake upstream + `curl`: model-mode key hit the limit after 4x30 tokens against a 100 limit → 5th request 429 `model_quota_exceeded`, bare `opus` also 429, `mock/haiku` on the same key still 200, `traffic_logs.model_id` correct on 200 and 429 rows.
- Headless Chrome against that server: Quotas page, dashboard card, keys column, `?edit=` deep link, edit modal (load rows, velocity on focus, Add Model picker, empty-limit error, save, mode switch keeps rows, access change flags unsaved rows), filters in the URL, collapse, dashboard "View all", dark and light themes. No page errors.
- **Not verified:** mobile layout, the `weekly`/`monthly` windows in a live run (unit-tested only), streaming responses with a Model Quota, auth-enabled mode.

## Global Constraints

- Tests: `CGO_ENABLED=0 go test -tags server ./...` and `node --test web/jstest/*.mjs` (use the glob; a bare directory fails on Node 24).
- No new Go dependencies. Raw SQL. Migrations are idempotent in `internal/db/db.go` `migrate()`.
- **SQLite has one connection.** Never run a query while another `rows` is open; inside a transaction use only `tx`. (`loadLocalCache` reads Model Quotas first and closes that query before reading keys; `UpdateKeyAll`, `CreateKeyWithOptions`, `DeleteKey` use only `tx`.)
- Model Quota limit `> 0`; periods `daily|weekly|monthly|total` (no `none`). UTC windows via `traffic.CalcQuotaWindow`. Model ids compare case-insensitively (`COLLATE NOCASE`).
- Do not change the access or disabled checks (spec §11.6).
- Atomic commits per task, conventional messages.

## Spec corrections (found while building; applied in Task 7)

1. §7: tokens are comma-formatted (`1,004,211 / 1,000,000`), not `1.00M`. A `total` quota says `Lifetime quota, does not reset.` and sends no `Retry-After` (both Key and Model Quota). Body is built with `json.Marshal`, includes `"param":"model"` for Model Quotas only.
2. §9: inactive reason order is `removed` → `disabled` → `no_access`; an id missing from `models` counts as `removed`. Helper is `models.LookupStatus(db,id) (found, enabled, removed)` + `(*Manager).Status`, not `LookupEnabled`.
3. §10.1: access fields come from the payload (as `UpdateKey` always did); new rows are checked against the payload's access. Unknown key on update stays HTTP 400.
4. §10.2: list responses carry `model_quota_summary` only; **new `GET /api/v1/keys/{id}`** returns the full key with `model_quotas`.
5. §10.4: `key_quota` also has `status`; a model-mode key with only inactive rows has status `unlimited`; `summary.model_quotas` counts all rows of model-mode keys; `near_limit`/`exhausted` count enforced rows only.
6. §10.3/§8: `GetModelUsageSince` compares timestamps in UTC (stored timestamps are UTC); a local-time start silently returned nothing.
7. §11.1: Add Model is an inline panel; the model list is filtered client-side from `/providers` (mirrors `ResolveModelID`); limit inputs use `step=1`; velocity history sits on its own row.
8. §11.3: key name links to `#/endpoints?edit=<id>`; Endpoints opens the modal and clears the param.

---

### Task 1: Data layer (schema, model id resolution, traffic queries)

**Files:** patches `internal_db_db.go`, `internal_providers_providers.go`, `internal_traffic_traffic.go`, `internal_traffic_velocity.go`; new `internal/models/lookup.go`, `internal/models/lookup_test.go`, `internal/db/model_quota_migration_test.go`, `internal/providers/resolve_test.go`, `internal/traffic/modelquota.go`, `internal/traffic/modelquota_test.go`.

**Produces:** column `api_keys.quota_mode`; table `api_key_model_quotas`; column `traffic_logs.model_id` + index `idx_traffic_key_model_ts`; setting `quota_warn_percent`=80; `providers.(*Manager).ResolveModelID(requested) (*Provider, actual, modelID string, err)` and `IsCanonicalModelID(id) bool`; `models.LookupStatus`; `traffic.LogEntry.ModelID`; `traffic.GetModelQuotaUsage`, `GetModelUsageSince`, `(*Manager).GetModelVelocityStats`, `GetQuotaWarnPercent`/`SetQuotaWarnPercent`.

- [ ] **Step 1:** Copy the new test files only and run them to see them fail:

```bash
put internal/db/model_quota_migration_test.go internal/providers/resolve_test.go internal/models/lookup_test.go internal/traffic/modelquota_test.go
CGO_ENABLED=0 go test -tags server ./internal/db ./internal/providers ./internal/models ./internal/traffic 2>&1 | tail -15
```

Expected: FAIL (missing column, undefined `ResolveModelID`, `LookupStatus`, `GetModelQuotaUsage`...).

- [ ] **Step 2:** Add the implementation:

```bash
patch_ internal_db_db.go internal_providers_providers.go internal_traffic_traffic.go internal_traffic_velocity.go
put internal/models/lookup.go internal/traffic/modelquota.go
```

- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./internal/db ./internal/providers ./internal/models ./internal/traffic`. Expected: PASS (existing velocity tests included).
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat: model quota schema, ResolveModelID, model usage queries"`

---

### Task 2: Keys (types, cache, atomic save, usage, overview)

**Files:** patch `internal_keys_keys.go`; new `internal/keys/modelquota.go`, `internal/keys/overview.go`, `internal/keys/modelquota_test.go`, `internal/keys/modelquota_write_test.go`, `internal/keys/overview_test.go`.

**Produces:** `KeyInfo.QuotaMode/ModelQuotas/ModelQuotaSummary`; `ModelQuota`; `NormalizeQuotaMode`, `IsModelQuotaPeriod`, `QuotaPercent` (floor, uncapped), `QuotaStatus`; `(*KeyInfo).EnforcesModelQuotas`, `ModelQuotaFor`; `CreateKeyOptions.QuotaMode/ModelQuotas` (one transaction); `KeyUpdate` + `(*Manager).UpdateKeyAll` (one transaction; nil `ModelQuotas` = keep, non-nil = replace all); `AccessPreview`, `DeriveAccessMode`; `GetModelQuotas`; `AnnotateModelQuotas`, `SummarizeModelQuotas`; `QuotaOverview`; `DeleteKey` removes Model Quotas in the same transaction; cache and `ValidateClientKey` fallback load quota fields.

- [ ] **Step 1:** `put internal/keys/modelquota_test.go internal/keys/modelquota_write_test.go internal/keys/overview_test.go` and run `go test ./internal/keys` → FAIL (undefined).
- [ ] **Step 2:** `patch_ internal_keys_keys.go; put internal/keys/modelquota.go internal/keys/overview.go`
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./internal/keys ./internal/handler`. Expected: PASS, including `TestUpdateKeyAllRollsBack` and the fallback-path test.
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat(keys): quota mode, model quotas, atomic save, usage and overview"`

---

### Task 3: Proxy enforcement

**Files:** patch `internal_proxy_proxy.go`; new `internal/proxy/quota.go`, `internal/proxy/quota_message_test.go`, `internal/proxy/model_quota_proxy_test.go`.

**Behavior:** Key Quota check skipped for `quota_mode=model`; provider resolved with `ResolveModelID`; after the access (403) and disabled (403) checks and provider resolution, a Model Quota row for the resolved id with `consumed >= limit` returns 429 `model_quota_exceeded` and logs `model_id`; `ModelID` is set on the plugin-reject, upstream-502 and success log entries; shared 429 writer (`writeQuotaExceeded`).

- [ ] **Step 1:** `put internal/proxy/quota_message_test.go internal/proxy/model_quota_proxy_test.go`; `go test ./internal/proxy` → FAIL (undefined `quotaExceededBody`).
- [ ] **Step 2:** `patch_ internal_proxy_proxy.go; put internal/proxy/quota.go`
- [ ] **Step 3:** `CGO_ENABLED=0 go test -tags server ./internal/proxy`. Expected: PASS (existing `TestProxyQuotaExceededReturns429`/`UnderLimitProceeds` unchanged).
- [ ] **Step 4: Commit** `git add -A internal && git commit -m "feat(proxy): enforce model quotas, log resolved model_id, shared 429 helper"`

---

### Task 4: API

**Files:** patches `internal_handler_handler.go`, `cmd_nineguard_main.go`; new `internal/handler/modelquota.go`, `internal/handler/model_quota_handler_test.go`.

**Produces:** `POST /api/v1/keys`, `PUT|POST /api/v1/keys/{id}` accept `quota_mode`, `model_quotas` (validation errors → 400 naming the model; update uses one `UpdateKeyAll`); `GET /api/v1/keys/{id}`; `GET /api/v1/keys/{id}/model-velocity?model=`; `GET /api/v1/quotas`; `quota_warn_percent` in `/settings/traffic` (1..100).

- [ ] **Step 1:** `put internal/handler/model_quota_handler_test.go`; `go test ./internal/handler` → FAIL.
- [ ] **Step 2:** `patch_ internal_handler_handler.go cmd_nineguard_main.go; put internal/handler/modelquota.go`
- [ ] **Step 3:** `CGO_ENABLED=0 go build -tags server ./... && CGO_ENABLED=0 go test -tags server ./...`. Expected: build OK, all packages PASS.
- [ ] **Step 4: Commit** `git add -A internal cmd && git commit -m "feat(api): model quota save, key detail, velocity, quotas overview, warn setting"`

---

### Task 5: JS helpers

**Files:** new `web/static/js/modelquotas.js`, `web/jstest/modelquotas.test.mjs`.

- [ ] **Step 1:** `put web/jstest/modelquotas.test.mjs`; `node --test web/jstest/*.mjs` → FAIL (module missing).
- [ ] **Step 2:** `put web/static/js/modelquotas.js`
- [ ] **Step 3:** `node --test web/jstest/*.mjs`. Expected: 28 pass, 0 fail.
- [ ] **Step 4: Commit** `git add -A web && git commit -m "feat(web): pure helpers for model quotas"`

---

### Task 6: UI

**Files:** patches `web_static_js_app.js`, `web_static_js_views_endpoints.js`, `web_static_js_views_dashboard.js`, `web_static_js_views_traffic.js`, `web_static_styles_main.css`; new `web/static/js/views/keyquota.js`, `web/static/js/views/quotas.js`.

What it does: key modal Quota section (mode switch, per-model rows, inline Add Model panel, velocity on focus, save payload with `quota_mode`/`model_quotas`, blocking on invalid or not-allowed rows); keys-table quota cell for model mode; Quotas page (summary cards, filters kept in the URL, collapsible keys); dashboard "Quotas near limit" card; `quota_warn_percent` input in Traffic settings; `?edit=<id>` deep link.

- [ ] **Step 1:** `patch_ web_static_js_app.js web_static_js_views_endpoints.js web_static_js_views_dashboard.js web_static_js_views_traffic.js web_static_styles_main.css; put web/static/js/views/keyquota.js web/static/js/views/quotas.js`
- [ ] **Step 2:** `for f in web/static/js/*.js web/static/js/views/*.js; do node --check $f || echo "BAD $f"; done`. Expected: no `BAD`.
- [ ] **Step 3: Rebuild and run.** The web files are embedded in the binary, so rebuild after every UI change:

```bash
CGO_ENABLED=0 go build -tags server -o /tmp/ng.exe ./cmd/nineguard
NINEGUARD_PORT=18080 NINEGUARD_AUTH_ENABLED=false NINEGUARD_DB_FILE=/tmp/ng-e2e/ng.db NINEGUARD_AUTH_FILE=/tmp/ng-e2e/auth.json /tmp/ng.exe
```

- [ ] **Step 4: Manual check** against `docs/mockups/model-quotas.html`, dark and light:
  1. Providers: add a provider with a prefix, set default, run Models "Fetch from Providers" (or point `$R/e2e/upstream.js` at port 18999 and use it as the provider).
  2. Create a key, choose **Per Model**, add two rows via **Add Model**. Save with an empty limit → blocked with a message. Fill limits, save.
  3. Send chat requests with `curl` until the limit → 429 `model_quota_exceeded`; another model on the key still works.
  4. Quotas page: key listed, rows, `exhausted` badge; filters change the URL; collapse works; unlimited keys hidden until "Show unlimited".
  5. Dashboard card appears and "View all" goes to `#/quotas?status=near`; keys-table column shows `<pct>% <model> · N limits`.
  6. Switch the key to **Key Quota** and back: rows are kept; the hint reads `N Model Quotas saved (inactive while in Key Quota mode)`.
  7. Change access to Custom with a model list that excludes a new row: that row is flagged and Save is blocked.
  Optional automation: `$R/e2e/cdp.js` drives Chrome through DevTools (`chrome --headless=new --remote-debugging-port=19222`); `s1.js`, `s2.js`, `s8.js` are the scripts used.
- [ ] **Step 5: Commit** `git add -A web && git commit -m "feat(web): model quota editor, Quotas page, dashboard card, keys column"`

---

### Task 7: Docs, cleanup, final verification

**Files:** modify `docs/superpowers/specs/2026-10-12-model-quotas-design.md`, `docs/ARCHITECTURE.md`, `docs/FUTURE_PLANNED_FEATURES.md`; delete `docs/superpowers/plans/model-quotas-reference/`.

- [ ] **Step 1:** Apply "Spec corrections" 1-8 to the spec (§7 example and `total` behavior, §8 UTC note, §9 order and helper name, §10.1-10.4, §11.1, §11.3); set its status to `Implemented`.
- [ ] **Step 2:** `docs/ARCHITECTURE.md` §2: add "Cek Key Quota (mode key)" before the model checks and "Cek Model Quota (mode model, Model ID hasil resolve)" after provider matching.
- [ ] **Step 3:** `docs/FUTURE_PLANNED_FEATURES.md` §3: add an "Extension: Model Quotas (ADR 0007/0008)" note; rename "Token Quotas" to "Key Quotas" in that section.
- [ ] **Step 4:** `git rm -r docs/superpowers/plans/model-quotas-reference` (or `rm -r` if untracked).
- [ ] **Step 5:** `CGO_ENABLED=0 go test -tags server ./...` and `node --test web/jstest/*.mjs`. Expected: all PASS.
- [ ] **Step 6: Commit** `git add -A docs && git commit -m "docs: model quotas spec corrections and architecture notes"`

---

## Self-Review

- **Spec coverage:** §3 schema → T1; §4 types and cache → T2; §5 → T1; §6, §7 → T3; §8 → T1; §9 → T2; §10 → T2/T4; §11 → T5/T6; §12 tests → in T1-T5 (UI by manual check, T6); §13 → T1-T3 data/enforcement, T4 API, T5-T6 UI, T7 docs.
- **Out of scope (unchanged):** access/disabled bare-name bypass (§11.6); ADR 0005 `Retry-After` wording for `total` (note it when telling the senior); the mockup's sample 429 text.
- **Type consistency:** names above are identical in the reference code (it compiled and passed as one unit).
- **Known risk:** patches were generated against the current repo files; `git apply --check` passed for all of them at the time of writing. If other work lands first, re-check with `git apply --check` before Task 1.
