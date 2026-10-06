# Plugin System — Design Spec

- Date: 2026-10-06
- Status: Draft, awaiting review
- Related: `GLOSSARY.md`, `docs/adr/0001-http-plugins.md`, `docs/adr/0002-model-group-dual-role.md`

## 1. Goal

Let NineGuard transform chat completion requests through **plugins** — token savers by default (Caveman, Ponytail, Headroom), plus third-party plugins — and control which plugins apply per **API Key**, per **Model Group**, or globally.

Unlike 9router, where token savers are global, NineGuard scopes plugins so different agents and model families get different behaviour.

## 2. Scope

### In scope (v1)

- Plugin engine inside the proxy request path.
- Built-in plugins: **Caveman**, **Ponytail**, **Headroom connector**.
- Third-party **HTTP Plugins** registered by URL.
- **Plugin Bindings** at Global, Model Group, and API Key scope with precedence.
- Global **Plugin Pipeline** order.
- Failure policy, rejection, per-request bypass header.
- Telemetry: plugins applied, tokens saved, plugin overhead, plugin errors, plugin latency.
- Dashboard UI and REST API for all of the above.
- RBAC and audit logging.

### Out of scope (v1)

- Response transformation (including SSE).
- Endpoints other than `POST /v1/chat/completions`. Other `/v1/*` requests pass through without plugins.
- RTK (tool-output compression filters) — planned v2, possibly as an HTTP Plugin.
- Automatic `X-9Router-Token-Saver: off` injection. Users disable 9router's RTK manually.
- Encryption of stored secrets (tracked separately, see §13).
- Token estimation via tokenizer.

## 3. Request Flow

Current flow in `internal/proxy/proxy.go`, with the new step inserted:

1. Authenticate API Key (401).
2. Read and buffer request body.
3. Per-key model access check (403 `model_not_allowed`).
4. Global model firewall (403 `model_disabled`).
5. Resolve provider by prefix (502 `no_provider`).
6. **NEW — Plugin Pipeline** (only for `POST /v1/chat/completions`):
   1. Resolve effective plugin set for (API Key, model).
   2. Drop bypassable plugins if `X-NineGuard-Plugins: off` is present.
   3. Run plugins in global pipeline order on the request body.
   4. On rejection → 403 `plugin_rejected`. On fail-closed error → 503 `plugin_unavailable`. On fail-open error → continue with the body from before that plugin.
7. Rewrite model ID (strip prefix) and forward upstream.
8. Stream/relay response, record traffic (now including plugin telemetry).

Plugins see the request **before** prefix stripping (model as the client sent it), so plugin context matches what the user configured.

The `X-NineGuard-Plugins` header is removed before forwarding upstream.

## 4. Plugin Model

### 4.1 Plugin interface (Go)

```go
type Plugin interface {
    ID() string
    Transform(ctx context.Context, req *ChatRequest, pc PluginContext) (Result, error)
}

type PluginContext struct {
    APIKeyName string
    Model      string   // as sent by client
    Provider   string   // provider ID
    Groups     []string // names of Model Groups the model belongs to
    Settings   map[string]any // effective merged settings
}

type Result struct {
    Request        *ChatRequest // nil when rejected
    Rejected       bool
    RejectMessage  string
    TokensSaved    int // reported by plugin; 0 if unknown
    TokensOverhead int // tokens added (injection plugins)
}
```

`ChatRequest` is the decoded JSON body as `map[string]any` with typed accessors for `messages`, so unknown fields are preserved verbatim.

### 4.2 Plugin record

Each registered plugin (built-in or HTTP) has:

| Field | Meaning |
|---|---|
| `id` | Stable ID. Built-ins: `caveman`, `ponytail`, `headroom`. HTTP: generated. |
| `kind` | `builtin` or `http` |
| `name`, `description` | Display |
| `url` | HTTP only (also used by Headroom connector) |
| `secret` | HTTP only, generated on registration |
| `timeout_ms` | Default 3000 (HTTP), 8000 (Headroom), n/a for injection built-ins |
| `failure_policy` | `open` or `closed`. Default `open`. |
| `bypassable` | Bool. Forced `false` when `failure_policy = closed`. |
| `pipeline_order` | Integer, global order (ascending) |
| `default_settings` | JSON |

### 4.3 Built-in: Caveman

- Prepends a `system` message containing the Caveman prompt.
- Settings: `variant` (`caveman`, `ultracave`, `megacave`; default `caveman`), `prompt_override` (string; empty = bundled default for the chosen variant).
- Variants map 1:1 to upstream skills `skills/caveman`, `skills/ultracave`, `skills/megacave` (≈4.0 KB, 2.3 KB, 2.6 KB). `megacave` replies in classical Chinese; UI labels it as such.
- Bundled prompt text from upstream Caveman (Apache-2.0) with attribution in `NOTICE`.
- Reports `TokensOverhead` = `ceil(len(injected_text) / 4)`, labelled "overhead (est.)" in UI. This is the only estimated number; Tokens Saved is never estimated.

### 4.4 Built-in: Ponytail

- Same mechanism as Caveman.
- Settings: `level` (`lite`, `full`, `ultra`; default `full`), `prompt_override`.
- Bundled text from Ponytail `AGENTS.md` (MIT) with attribution in `NOTICE`.

### 4.5 Injection position

- New `system` message **prepended at index 0**. Client's own system message is untouched.
- Text is deterministic for a given settings combination so the provider prefix cache stays stable.
- When both Caveman and Ponytail apply, each prepends in pipeline order; final order is deterministic.

### 4.6 Built-in: Headroom connector

- Calls `POST {url}/v1/compress` with `{messages, model, config}`.
- `system` and `tools` from the original request are preserved by NineGuard (Headroom ignores them).
- Settings:
  - `url` (default `http://127.0.0.1:8787`)
  - `mode` — `incremental` (default) or `full`
    - `incremental`: `config.frozen_message_count` = index after the last `assistant` message; only newer messages are compressed. Preserves provider prefix cache.
    - `full`: compress entire history every turn.
  - `compress_user_messages` (default `false`)
  - `token` — optional `HEADROOM_PROXY_TOKEN`
- `TokensSaved` = Headroom's `tokens_saved`.
- If Headroom returns `compression_skipped: true`, treat as success with 0 saved.
- Requires `HEADROOM_COMPRESS_ALLOW_REMOTE=1` on the Headroom side when it runs in a separate container (see §12).

### 4.7 HTTP Plugin contract

Request from NineGuard:

```http
POST {url}
Content-Type: application/json
X-NineGuard-Plugin-Secret: {secret}

{
  "request": { ...chat completion body... },
  "context": {
    "api_key_name": "Cursor IDE",
    "model": "9router/claude-sonnet",
    "provider": "9router",
    "groups": ["Claude"],
    "settings": { ... }
  }
}
```

Client IP is **not** sent.

Responses accepted:

```json
{ "request": { ...modified body... }, "tokens_saved": 120, "tokens_overhead": 0 }
```

```json
{ "action": "reject", "message": "PII detected in prompt" }
```

`tokens_saved` and `tokens_overhead` are optional. Any non-2xx status, invalid JSON, missing `request`, or timeout is a plugin error and goes through the failure policy.

The plugin must not change `model`. If it does, NineGuard restores the original value and logs a warning.

## 5. Bindings and Resolution

### 5.1 Binding record

| Field | Meaning |
|---|---|
| `plugin_id` | |
| `scope_type` | `global`, `group`, `key` |
| `scope_id` | empty for global, group ID, or API key ID |
| `state` | `on`, `off`, `inherit` |
| `settings` | JSON override, merged shallowly over broader settings |

One binding per (plugin, scope_type, scope_id). Global bindings exist for every plugin; default state `off` (opt-in).

### 5.2 Resolution per plugin

For a request with API Key K and model M:

1. Start with the Global binding (state, settings).
2. Find every Model Group whose members match M (same pattern rules as access control: exact, `*`, `prefix/*`, `*.suffix`, flexible provider prefix). Among those with a non-`inherit` binding for this plugin, take the one with the highest **Group Priority**; ties broken by group name ascending. Apply its state and merge its settings.
3. If K has a non-`inherit` binding, apply its state and merge its settings.
4. Plugin runs if final state is `on`.

Settings merge order: plugin `default_settings` ← global ← group ← key.

### 5.3 Group Priority

- New integer column on `model_groups`, default `0`.
- UI warns when two groups with equal priority have conflicting bindings for the same plugin and share at least one model pattern overlap (best-effort detection: same pattern or one is a wildcard covering the other).

### 5.4 Caching

Plugins, bindings, and group membership are loaded into memory (same pattern as `keys.Manager.ReloadGroupCache`) and reloaded on any write. Resolution must not touch the database per request.

## 6. Pipeline Execution

- Applicable plugins run sequentially in ascending `pipeline_order`.
- Each plugin receives the output of the previous one.
- Per-plugin timeout via `context.WithTimeout`.
- Total pipeline has no extra timeout beyond the sum of plugin timeouts.
- Recommended default order: Headroom (compress) → Ponytail → Caveman, so compression never processes injected prompts.

### 6.1 Failure handling

| Event | Fail-open plugin | Fail-closed plugin |
|---|---|---|
| Timeout / network error / bad response | Skip plugin, keep previous body, record error | 503 `plugin_unavailable` |
| Explicit reject | 403 `plugin_rejected` | 403 `plugin_rejected` |

Error bodies use OpenAI format:

```json
{ "error": { "message": "...", "type": "permission_error", "param": null, "code": "plugin_rejected" } }
```

```json
{ "error": { "message": "Plugin 'pii-filter' is unavailable.", "type": "server_error", "param": null, "code": "plugin_unavailable" } }
```

Both are recorded in `traffic_logs`.

### 6.2 Bypass

- Header `X-NineGuard-Plugins: off` skips all plugins with `bypassable = true` for that request.
- Non-bypassable plugins still run.
- Header is stripped before forwarding.

## 7. Data Model

New tables:

```sql
CREATE TABLE IF NOT EXISTS plugins (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,              -- builtin | http
  name TEXT NOT NULL,
  description TEXT DEFAULT '',
  url TEXT DEFAULT '',
  secret TEXT DEFAULT '',
  timeout_ms INTEGER DEFAULT 3000,
  failure_policy TEXT DEFAULT 'open',
  bypassable INTEGER DEFAULT 1,
  pipeline_order INTEGER DEFAULT 100,
  default_settings TEXT DEFAULT '{}',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS plugin_bindings (
  plugin_id TEXT NOT NULL,
  scope_type TEXT NOT NULL,        -- global | group | key
  scope_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'inherit',
  settings TEXT DEFAULT '{}',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plugin_id, scope_type, scope_id)
);
```

Column additions (via existing `ALTER TABLE ... ADD COLUMN` migration style in `internal/db/db.go`):

```sql
ALTER TABLE model_groups ADD COLUMN priority INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN plugins_applied TEXT DEFAULT '';   -- comma-separated IDs
ALTER TABLE traffic_logs ADD COLUMN tokens_saved INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN tokens_overhead INTEGER DEFAULT 0;
ALTER TABLE traffic_logs ADD COLUMN plugin_errors TEXT DEFAULT '';     -- comma-separated IDs
ALTER TABLE traffic_logs ADD COLUMN plugin_ms INTEGER DEFAULT 0;
```

Seed on startup (idempotent): insert built-ins `headroom` (order 10), `ponytail` (order 20), `caveman` (order 30) and their Global bindings with state `off`.

Cascade rules:
- Deleting an API Key deletes its bindings.
- Deleting a Model Group deletes its bindings (group deletion is already blocked while linked to keys; plugin bindings do not block deletion).
- Built-in plugins cannot be deleted.

## 8. REST API

All under existing session auth.

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/v1/plugins` | any | List plugins (secret masked) with global binding |
| POST | `/api/v1/plugins` | admin | Register HTTP plugin; returns secret once |
| PUT | `/api/v1/plugins/{id}` | admin for `url`/`secret`/`failure_policy`; operator for the rest | Update plugin |
| POST | `/api/v1/plugins/{id}/rotate-secret` | admin | New secret, returned once |
| DELETE | `/api/v1/plugins/{id}` | admin | Delete HTTP plugin (built-ins rejected) |
| POST | `/api/v1/plugins/{id}/test` | any | Send dummy request, return latency + result |
| PUT | `/api/v1/plugins/order` | any | Body: ordered list of IDs |
| GET | `/api/v1/plugins/{id}/bindings` | any | All bindings for plugin |
| PUT | `/api/v1/plugins/{id}/bindings` | any | Upsert one binding `{scope_type, scope_id, state, settings}` |
| GET | `/api/v1/plugins/resolve?key_id=&model=` | any | Preview effective plugins and settings for a key + model |
| POST | `/api/v1/plugins/{id}/reset-prompt` | any | Clear `prompt_override` (Caveman/Ponytail) |

`PUT /api/v1/model-groups/{id}` accepts optional `priority`.

Admin checks follow the existing pattern `user.Role != "admin"` in `internal/handler/handler.go`. Every write emits an audit log entry (`plugin.create`, `plugin.update`, `plugin.delete`, `plugin.rotate_secret`, `plugin.binding.update`, `plugin.order.update`).

## 9. Dashboard UI

New page **Gateway → Plugins** (`web/static/js/views/plugins.js`):

- Banner on first view after upgrade: "Plugins available — all off by default."
- Plugin list with drag-to-reorder (pipeline order), kind badge, failure policy, bypassable, global state toggle, Test button with latency.
- Plugin detail drawer:
  - Settings form (built-ins: typed fields; HTTP: JSON editor).
  - Prompt editor with "Reset to default" (Caveman/Ponytail).
  - Bindings table: Global row, one row per Model Group, one row per API Key, each with `on/off/inherit` and settings override.
  - Conflict warning for equal-priority groups.
- "Register HTTP Plugin" modal (admin only): name, URL, timeout, failure policy, bypassable. Shows generated secret once with Copy button.
- **Resolve preview**: pick API Key + model → shows which plugins run and with what settings.

Changes to existing pages:

- **Model Groups**: priority field.
- **Endpoints & Keys**: per-key "Plugins" section linking to bindings.
- **Traffic Explorer**: columns for plugins applied, tokens saved, overhead, plugin errors, plugin ms.
- **Dashboard**: "Tokens Saved" KPI card.
- **Usage Reports**: tokens saved per key and per plugin.

## 10. Telemetry

- `tokens_saved`: sum of plugin-reported values only. Never estimated.
- `tokens_overhead`: sum of plugin-reported overhead; for Caveman/Ponytail this is a character-based estimate labelled "(est.)".
- `plugin_ms`: wall time of the whole pipeline.
- `plugin_errors`: IDs of plugins that failed (fail-open or fail-closed).
- System log entries (`source=plugin`) for every plugin error with plugin ID, key name, model, and error.

## 11. Security

- HTTP Plugin registration, URL change, secret rotation, deletion, and failure-policy change are admin-only and audited.
- Secrets stored plain text in v1, consistent with provider keys (see §13).
- Secret shown once on create/rotate; masked everywhere else.
- Plugin URL must be `http` or `https`. No other validation in v1 (admin trust boundary). Loopback and private addresses are allowed because Headroom and local plugins live there.
- Client IP never sent to plugins.
- `X-NineGuard-Plugins` header never forwarded upstream.
- Fail-closed plugins cannot be bypassable (enforced server-side).

## 12. Deployment Notes

For the current VM (NineGuard with `--network host`, Headroom in Docker with published port 8787):

- Headroom sees NineGuard's call as non-loopback (Docker bridge) and returns 404. Set `HEADROOM_COMPRESS_ALLOW_REMOTE=1` on the Headroom container.
- Close 8787, 8317, and 20128 in the cloud Security Group. Docker-published ports bypass ufw.
- Turn off RTK in 9router dashboard to avoid double compression.
- On a 2 vCPU / 2 GB VM, Headroom adds roughly 1–3 s per request. Plugin latency is visible in Traffic Explorer.

These notes go into the README plugin section.

## 13. Related Issues (separate tasks)

1. **Client API keys stored in plain text.** `internal/keys/keys.go` writes `rawKey` to `api_keys.key`, but README says only hashes are stored. Provider API keys and (after this feature) plugin secrets are also plain text. Fix all together: hash client keys (SHA-256 lookup), encrypt provider keys and plugin secrets with `NINEGUARD_SECRET_KEY`.
2. **Requests without `model` skip per-key ACL.** `IsModelAllowed("")` returns `true`; `/v1/embeddings` or similar with missing `model` is forwarded.

## 14. Open Questions

1. **Ponytail level text.** Upstream `AGENTS.md` is one compact prompt; levels `lite/full/ultra` are selected by an instruction line. Confirm during plan whether a single bundled text plus a level line is sufficient.

## 15. Testing

- **Unit**
  - Resolution: global/group/key precedence, `inherit`, group priority, tie-break, pattern matching parity with access control.
  - Settings merge.
  - Pipeline: ordering, fail-open skip, fail-closed 503, reject 403, bypass header respects `bypassable`, model field restored if plugin changes it.
  - Caveman/Ponytail injection: prepend position, deterministic output, client system message untouched.
  - Headroom adapter: `frozen_message_count` calculation, `system`/`tools` preservation, `compression_skipped` handling (with `httptest` server).
  - HTTP plugin contract: secret header, timeout, malformed responses.
- **Integration** (`internal/proxy`)
  - End-to-end chat request through plugins to a fake upstream; verify forwarded body and traffic log columns.
  - Non-chat endpoints bypass plugins.
  - `X-NineGuard-Plugins` stripped before upstream.
- **Handler**
  - RBAC: operator cannot register/delete HTTP plugins or change URL/secret/failure policy.
  - Audit entries written.
  - Built-in deletion rejected.
- **Migration**
  - Fresh DB and existing DB both end with seeded built-ins, all global `off`.
- Build checks: `go test ./...` and `CGO_ENABLED=0 go test -tags server ./...`.

## 16. Licensing

- Add `NOTICE` with attributions:
  - Caveman — Apache License 2.0, Copyright 2026 Julius Brussee.
  - Ponytail — MIT License, Copyright 2026 DietrichGebert.
- Bundled prompt files stored under `internal/plugins/builtin/prompts/` with their license headers.
