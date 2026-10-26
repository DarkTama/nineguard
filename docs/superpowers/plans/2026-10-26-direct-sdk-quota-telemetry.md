# Direct SDK Quota Telemetry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show passive Claude/Codex account limit telemetry directly from embedded SDK providers in Quotas, client quota commands, and the portal without using 9router or creating inference requests.

**Architecture:** Extend the existing upstream quota contract with explicit percentage-only window fields and observation state. Inject a read-only SDK source into the upstream manager; SDK backend detection overrides the legacy disabled tracker setting. Existing HTTP tracker paths and key-budget enforcement remain unchanged.

**Tech Stack:** Existing Go/SQLite gateway, CLIProxyAPI v8.0.43, vanilla JS dashboard/portal, existing test and browser tooling.

**Spec:** `docs/superpowers/specs/2026-10-26-direct-sdk-quota-telemetry-design.md`

## Global Constraints

- Do not route telemetry through 9router, OmniRoute, or another external gateway.
- Existing HTTP-provider quota protocols remain unchanged. Client-key token budgets and enforcement are unchanged.
- Refresh rereads observations without inference, grant refresh or a new paid request.
- Missing numeric values are null/absent, never inferred as zero.
- SDK measurements are stale after 5 minutes or after their known reset passes; reads never advance ObservedAt.
- Client-facing shared pools expose no account ids, emails, labels or counts; they are aggregate context, not individual key metrics.
- SDK refresh must not preserve deleted accounts through the HTTP adapters' stale-on-error cache fallback.
- Follow SQLite single-connection rules, existing UI tokens and English code/spec conventions; Indonesian architecture/provider docs retain their language.
- Do not change OAuth credentials, inference body transformations, disabled/access checks or entitlement handling as side effects.

## Review Focus

- Zero utilization is real data, unlike absent/malformed headers; reject NaN/Inf (Task 1).
- Reading a reset-after timestamp later must not move reset forward (Task 1).
- A disconnected account must disappear even when an old upstream cache entry has buckets (Task 2).
- One unknown or stale account must not yield a healthy, complete multi-account aggregate (Task 3).
- A percentage-only limit must not be printed as 53 tokens out of 100 on any surface (Tasks 3 and 4).

## Contracts and file map

- `internal/upstream/types.go`: extend QuotaBucket with `Unit string` ("percent" only for SDK, omitted for legacy), `UsedPercent *float64`, `RemainingPercent *float64`, `Scope string` (account or model id), `WindowMinutes int64`, `ObservedAt *time.Time`, `ObservationState string`, `LimitReached bool`, `Incomplete bool`. JSON names are snake_case with omitempty except pointer values remain present when measured zero. Keep legacy numeric fields for HTTP buckets unchanged; SDK fields `Used/Total/Remaining` stay zero and MUST NOT be rendered as token totals. Add provider/connection optional `ObservedAt`, `ObservationState` and `Source`="sdk".
- `internal/providerengine/quota.go` / `quota_test.go`: SDK snapshot selection and Claude/Codex parsers. Imports `upstream` DTOs, never the reverse concrete package; `upstream` depends only on a source interface.
- `internal/upstream/client.go`: inject `SDKQuotaSource` with `SDKQuotas(context.Context, providers.Provider, time.Time) (*ProviderQuotaResult,error)` and `SetSDKQuotaSource(source SDKQuotaSource)`. Add `EffectiveQuotaType(p providers.Provider) string` and `SupportsQuotaTracking(p providers.Provider) bool` in `capability.go`; cliproxy always derives "sdk", HTTP uses existing QuotaType.
- `cmd/nineguard/main.go`: inject providerEngine into shared upstreamMgr.
- `internal/handler/upstream_quota.go`, `internal/proxy/proxy.go`: replace quota-type-only skips with effective capability predicate and obtain current SDK snapshots rather than cached observations.
- `internal/upstream/pool.go`, `alert.go`, `internal/proxy/chat_command.go`: explicit percentage/unknown semantics through privacy-preserving aggregation, alerts and Markdown.
- `web/static/js/upstreamtelemetry.js`: shared pure display predicates/labels for percent windows; `web/jstest/upstreamtelemetry.test.mjs` covers consumer-visible unit/state logic only.
- `web/static/js/views/upstream_quotas.js`, `views/providers.js`, `portal.js`: actual surfaces, reusing existing styles. No source-text/wiring-only tests.

### Task 1: Safe, read-only SDK quota snapshots

**Files:** modify `internal/upstream/types.go`; create `internal/providerengine/quota.go`, `quota_test.go`.

**Interfaces:** produces `(*Engine).SDKQuotas(ctx context.Context,p providers.Provider,now time.Time) (*upstream.ProviderQuotaResult,error)`; package-private `parseQuotaSignals(family string, signals map[string]string, observedAt,now time.Time,scope string) []upstream.QuotaBucket`.

- [ ] Write `TestSDKQuotaSignals` table cases: Claude 0.53/0 utilization become 53/0 used and 47/100 remaining; no absolute token totals; nonfinite, malformed and missing values are not zero. Rejected-without-utilization sets limit_reached and leaves percentages nil. Known 5h/7d resets parse Unix timestamps. Codex primary/secondary percent values are not multiplied by 100, duration is preserved and reset-after is anchored to ObservedAt. Additional named windows stay distinct. At 5 minutes plus 1ns and when reset passed, state is stale and original observation time remains unchanged.
- [ ] Write `TestSDKQuotaAccountIsolation`: restore two providers/accounts, inject SDK observations through public runtime mutation, verify only selected Provider accounts appear; disconnected/disabled accounts do not supply available capacity; needs_login state is visible. Test newest account/model snapshot selection without unioning old headers and preserve model scope.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/providerengine -run 'TestSDKQuota'` and observe failure before code.
- [ ] Implement safe DTO projection. Prefer current account snapshot, add distinct newer scoped model snapshots without converting model limits to global. Read runtime clones, close SQLite rows before querying runtime-bound metadata. Waiting state has no percentage and no invented timestamp. No token or raw Signals map appears in output. Use measured percentage clamping and existing time/reset style.
- [ ] Run targeted tests, expect PASS. Commit `feat(upstream): expose safe direct SDK quota snapshots`.

### Task 2: SDK-aware quota dispatch and lifecycle freshness

**Files:** modify `internal/upstream/client.go`, `internal/handler/upstream_quota.go`, `internal/proxy/proxy.go`, `cmd/nineguard/main.go`; create `internal/upstream/capability.go`, `sdk_test.go`; extend existing handler/proxy quota tests.

**Interfaces:** consumes Task 1 `SDKQuotas`; produces `EffectiveQuotaType`, `SupportsQuotaTracking`, upstream `SetSDKQuotaSource`. SDK snapshots bypass old HTTP stale-cache fallback and are read on every quota request (read-only/in-memory plus account binding), regardless of force; HTTP cache/cooldown unchanged.

- [ ] Write `TestSDKQuotaOverridesDisabledTracker`: cliproxy Claude with persisted quota_type=disabled dispatches to SDK, returns quota_type=sdk, never unsupported; HTTP disabled remains unsupported and HTTP 9router/NineGuard retain adapters.
- [ ] Write `TestSDKQuotaRefreshDoesNotInferOrReDate`: repeated force/non-force reads perform no auth/inference requests, retain observed timestamp; disconnect followed by read removes old buckets despite a previous cached result. Unknown/unsupported family returns capability-specific message without suggesting 9router.
- [ ] Write handler/proxy integration assertions that SDK providers with disabled tracker appear in admin API and `/v1/quota`/chat-command preparation; existing budget and model access behavior remains unchanged.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/upstream ./internal/handler ./internal/proxy -run 'TestSDKQuota'`; observe failures.
- [ ] Implement source injection and all skip-predicate replacements, including background sync and key-detail unified quota endpoint. For SDK `GetCachedQuota` returns a current read-only snapshot or nil causing safe source dispatch, never reused post-disconnect data. Do not import concrete providerengine into upstream. Initialize shared source once at startup.
- [ ] Run targeted tests and existing HTTP quota tests, expect PASS. Commit `fix(upstream): recognize embedded SDK quota capability automatically`.

### Task 3: Privacy-preserving shared percentages, alerts and commands

**Files:** modify `internal/upstream/pool.go`, `pool_test.go`, `alert.go`, `alert_test.go`, `internal/proxy/chat_command.go`; add command regression in existing quota-command tests.

**Interfaces:** consumes explicit SDK QuotaBucket contract; keeps MergeAccountPools signature unchanged. Same account-wide family/window identity aggregates minimum known RemainingPercent, max UsedPercent, earliest relevant reset and oldest contributing observation; waiting/stale/incomplete are propagated. No account identity/count appears in client DTOs.

- [ ] Write `TestSDKQuotaPoolsConservativeAndPrivate`: accounts at 53% and 10% used aggregate 47% left, not 68.5%/137%; unknown contributor sets incomplete, stale contributor sets stale, unknown-only stays waiting. Exclude disabled/needs_login accounts from capacity while retaining safe aggregate state. Never merge different scopes/window durations; model-only windows are omitted from global pool. SDK percent and HTTP token buckets never mix by name.
- [ ] Write `TestSDKQuotaAlertsRequireFreshCompleteMeasurement`: waiting, stale, incomplete, malformed cannot trigger fresh depletion; fresh measured low capacity produces existing edge-triggered warning; no token count is fabricated.
- [ ] Write `TestSDKQuotaCommandUsesPercentUnits`: Markdown contains measured percent, reset and observed/stale/waiting labels; does not format SDK integer defaults as token limits; client key budget block is unchanged.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/upstream ./internal/proxy -run 'TestSDKQuota(Pools|Alerts|Command)'`; observe failure.
- [ ] Implement percent path alongside unmodified legacy aggregation. Add safe aggregate provenance description Most constrained observed account, not pooled tokens. Source/observed/state data survives client pool projection; sensitive identity remains excluded. Update command footer so snapshot read time is not claimed as new SDK measurement.
- [ ] Run tests and existing pool/alert/chat-command suite, expect PASS. Commit `feat(quota): aggregate observed SDK limits without inventing capacity`.

### Task 4: Honest SDK telemetry on dashboard, Providers and portal

**Files:** create `web/static/js/upstreamtelemetry.js`, `web/jstest/upstreamtelemetry.test.mjs`; modify `web/static/js/views/upstream_quotas.js`, `views/providers.js`, `portal.js`, and existing quota alert helper tests only if their consumer behavior changes.

**Interfaces:** shared helpers `isPercentBucket(bucket)`, `telemetryStateLabel(state)`, `remainingLabel(bucket)`, `observationLabel(observedAt,state)` exported; all renderers use actual source timestamps and null-aware percentages. Existing request/response schema unchanged except additive fields from Task 1.

- [ ] Write pure helper tests for zero versus unknown, percentage units, stale and waiting labels and malformed timestamp; write fresh/stale filtering regression for dashboard alert consumer. No DOM wiring tests.
- [ ] Run `node --test web/jstest/upstreamtelemetry.test.mjs web/jstest/upstream_quota.test.mjs`; observe failures before helper implementation.
- [ ] Read antislop-ui and frontend-design before editing layout. Quotas labels SDK account telemetry, account rows show percent/reset, no Used/Total token claim; Last observed replaces Synced just now for SDK, Refresh tooltip reads Read latest SDK snapshot; no connected account and unsupported family states have accurate actions. Stale retains previous value but visibly labelled; unknown shows a dash/waiting, not a red empty bar.
- [ ] Portal accepts percent buckets even when Total is zero; shows aggregate source, unknown/stale/incomplete and no account identity. Preserve key budget design and its enforcement. Providers SDK form replaces HTTP tracker/password controls with Automatic SDK telemetry hint; custom HTTP form/behavior remains unchanged.
- [ ] Run edited JS syntax checks and existing JS suite. Rebuild/restart scratch binary; browser smoke at desktop/390px, both themes, observed zero/53%, waiting, stale, incomplete, revoked, no-account; exercise Refresh and verify observation time unchanged. Do not mutate production providers or fabricate real-account verification.
- [ ] Commit `feat(ui): display direct SDK quota telemetry and observation freshness`.

### Task 5: Actual SDK-header integration proof and documentation

**Files:** extend `internal/providerengine/gateway_test.go` or create `quota_integration_test.go`; update `README.md`, `GLOSSARY.md`, `docs/PROVIDERS.md`, `docs/ARCHITECTURE.md`, approved spec status.

- [ ] Add `TestSDKQuotaFromRealSDKResponseHeaders`: local fake upstream emits Claude unified 5h utilization=0.53, 7d=0.0 and reset headers; issue a real admitted request through NineGuard→SDK→local server, then verify upstream admin result and privacy-preserving client quota result report 47%/100% left without token totals. Repeat with changed headers and one force read; only normal inference may increment upstream request count. Exercise buffered and SSE header observation where SDK supports them; document if a provider omits headers.
- [ ] Observe regression failure before final integration fixes, then PASS. Run `CGO_ENABLED=0 go test -tags server ./...`, `CGO_ENABLED=0 go build -tags server ./...`, `CGO_ENABLED=1 go test -race -tags server ./internal/providerengine ./internal/upstream`, `node --test web/jstest/*.mjs`, and `node --check` all edited JS.
- [ ] Request independent branch review for percentage units, unknown/stale semantics, account privacy, post-disconnect freshness and unchanged HTTP behavior. Fix important evidence-backed issues with regressions; report only checks actually exercised.
- [ ] Document passive SDK snapshots versus active HTTP tracker fetches, supported Claude/Codex windows, waiting/stale states, Refresh's no-inference meaning and difference from client key token quota. Update implemented spec status only after full changed paths are proven; retain explicit real-account limit. No ADR unless implementation introduces a further hard-to-reverse decision.
- [ ] Commit feature docs with changes and keep branch ready for one squash feature commit before main integration. Stop scratch services/tabs and remove throwaway artifacts. No production restart, push, main merge or version bump without user instruction.

## Review result and handoff

Coverage self-review: backend capability/snapshot contracts (Tasks 1–2), safe client aggregation and commands/alerts (Task 3), every named UI consumer and auto source hint (Task 4), runtime header flow, HTTP regression gates and docs (Task 5). No unresolved placeholder; source interface is owned by upstream and implemented by providerengine to avoid cycles. Percentage DTOs are additive and legacy fields never carry fabricated capacity. Unknown, incomplete and stale stay explicit all the way to clients.

Recommended execution is native in this session with one independent whole-branch review; tasks share the percentage DTO and effective-capability contract. Written plan approval is required before application code changes.

## Execution evidence

Implemented on `feat/sdk-quota-telemetry`. Server-tag Go tests/build passed, JavaScript 73/73 passed, and race checks passed for providerengine/upstream. Real embedded SDK plus local upstream response headers validated 53% used/47% remaining and no extra inference on force quota reads. Browser smoke used labelled response fixtures at desktop/mobile390 for observed/stale/waiting/incomplete states; no real-account telemetry or production service was exercised.

Independent backend/surface reviews identified provider visibility, malformed reset serialization, alert scope collisions and minimum-account provenance; corrected with regression coverage. Keep model-only observations scoped and omitted from global account pool; read SDK projections without HTTP caching so disconnect is immediately reflected. Existing HTTP trackers and client key budgets remain separate. Implementation commits are staged by backend/UI/docs and must be squashed to one feature commit before main integration. No push or merge performed.
