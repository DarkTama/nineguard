# Direct SDK Provider Quota Telemetry

- Date: 2026-10-26
- Status: Implemented — direct SDK snapshots; real-account telemetry remains user-dependent
- Related: ADR 0013 (aggregate shared context), ADR 0015 (embedded engine)

## Goal and boundaries

Display observed upstream account limits for Providers whose `backend` is `cliproxy`, starting with the shipped Claude and OpenAI/Codex connectors. Do not route telemetry through 9router, OmniRoute, or another external gateway. Existing HTTP-provider quota protocols remain unchanged. Client-key token budgets and enforcement are unchanged.

The SDK v8.0.43 already retains passive account observations in `Auth.Quota.Signals` with `ObservedAt`. Claude observations include unified 5h/7d utilization, status and reset headers; Codex observations include primary/secondary percentages and window/reset headers. No active Claude usage-poll API was found in the pinned SDK. Refresh rereads observations without inference, grant refresh or a new paid request.

## Root cause

`internal/upstream/client.go:GetProviderQuotas` returns unsupported when quota_type is disabled before considering Provider.Backend. Existing UI labels that result Direct LLM (No Tracker). Handler/proxy background and client-quota loops also skip disabled quota_type providers. The engine currently exposes account health and model catalogs but no safe quota snapshot.

## Data contract

1. Backend `cliproxy` automatically selects a derived `sdk` telemetry source regardless of the stored HTTP tracker setting. Do not require changing existing provider rows or selecting the 9router protocol.
2. Add a providerengine read-only snapshot method using safe, explicit DTOs. Return only Provider/account binding, safe account label/plan, family, account state, observation time, quota windows, percentage values and resets. Never expose raw Auth, arbitrary metadata, token material, request headers or SDK global catalog.
3. Quota windows have a stable id, display name, optional used/remaining percentage, optional reset timestamp, unit `percent`, and observation state. Missing numeric values are null/absent, never inferred as zero. Existing integer used/total/remaining fields in HTTP buckets must not be interpreted as token counts for SDK windows; extend quota types with an explicit percentage-only representation and update every consumer.
4. Claude utilization values are fractions: 0.53 means 53% used and 47% remaining. Validate finite numbers and clamp display percentages to 0..100. Parse 5h and 7d resets as Unix timestamps. Partial headers produce only known facts; no fabricated quota for missing windows. A rejected status without utilization can report limit reached without inventing a precise percentage. Extra/overage headers are distinct status context, not an additional token allowance.
5. Codex primary/secondary used-percent values are already percentages, not fractions. Preserve measured window minutes and reset timestamps. Reset-after values are relative to ObservedAt, never the current read time. Retain distinct named additional-limit windows when the signal schema provides them; never present them as a global budget.
6. Read only accounts owned by the selected Provider. Disabled/disconnected accounts are excluded from available-capacity aggregation; needs_login is visible as account health, not available capacity. If the newest observation is account-level use it; model-state observations can supply a newer snapshot without merging unrelated old header fields into it. Keep model-specific scope distinguishable from account-wide windows.

## Observation states and refresh

- `waiting`: account connected but no quota observation. Show Waiting for upstream telemetry. A normal request may supply it.
- `observed`: known snapshot observed within 5 minutes and before any known reset.
- `stale`: snapshot older than 5 minutes or its known reset has passed. Preserve last observation and explicitly label it stale; do not assume the quota reset to empty or claim it is current.
- `needs_login` / `disabled`: account state, never unlimited.
- `unsupported`: SDK family has no verified quota observation schema; do not suggest changing it to 9router.
- No connected accounts: show Connect an account in Providers.

SDK results carry ObservedAt separately from check/read time. Reads/Refresh do not change observation timestamps. Do not display Synced just now as if it meant a new provider measurement. If cache is used, force refresh rereads the engine snapshot and existing bounded cooldown remains applicable. Waiting/stale/malformed snapshots cannot trigger fresh depletion alerts. Active, fresh measured low capacity can use the existing alerts without mixing it with per-key budgets.

## Surfaces and privacy

### Quotas dashboard

Label the provider SDK account telemetry rather than Direct LLM. Show account label, state, quota window percentage, reset and Last observed time. Percentage-only rows show no Used/Total token number. Render waiting and stale states neutrally, not as errors or green 100% availability. Refresh tooltip says Read latest SDK snapshot; no inference request is sent.

### GET /v1/quota, /quota and portal

Include SDK Providers even when their stored quota_type is disabled. Preserve key permissions and current provider/model visibility rules. Shared SDK capacity remains aggregate context, never added to the key's usage or limit. Do not expose account ids, emails, labels or counts in client-facing aggregate pools (existing ADR privacy rules).

For account-wide windows with the same family/window identity, choose the lowest known remaining percentage among eligible accounts and clearly describe this as the most constrained observed account, not pooled total capacity. The aggregate becomes stale if a contributing measurement is stale and marks incomplete data if any eligible account lacks the window; unknown-only remains waiting. Do not combine windows with different scopes, durations or reset definitions. Per-model windows keep scope or are omitted from global aggregate rather than mislabelled.

The /quota Markdown and portal show percentage/reset plus observation state and never pass SDK windows through token-formatting helpers. Admin dashboard receives per-account details; client pool projection stays privacy-preserving.

### Providers page

For SDK-backed entries show capacity source Automatic SDK telemetry, not the HTTP tracker dropdown. Hide HTTP-only tracker/password controls for SDK connections while preserving existing HTTP provider editing. Explicitly note observations depend on upstream responses.

## Integration and dependencies

Use the existing upstream Manager cache/dispatch pattern with an injected SDK quota source; avoid import cycles between providers, engine and upstream. Providerengine returns safe telemetry types defined in a neutral package or upstream types only if that creates no cycle. Do not scrape SDK private management endpoints or expose arbitrary API-call endpoints.

Update all skip predicates in admin handlers, background synchronization, proxy unified quotas and self-service responses to test effective quota capability (SDK backend versus HTTP tracker), not only the persisted quota_type. Update shared bucket merge/render code, command formatting, portal and dashboard alerts for percentage-only/unknown telemetry. Do not reinterpret percentage readings as quota enforcement decisions.

## Verification

- Failing regression first: cliproxy Claude with quota_type=disabled is SDK telemetry, never unsupported/no-tracker.
- Fraction/percent parsing, zero, over-100, malformed/nonfinite, absent windows, rejected-without-utilization, reset-after anchoring and stale/reset-passed cases.
- Provider/account isolation, excluded disconnected/disabled accounts, model-specific scope and newest observation selection.
- Client projection has no account identity and never averages/sums unknown capacities or falsely marks partial data complete.
- Refresh makes no upstream inference/auth call and does not advance ObservedAt.
- Real embedded SDK plus local upstream headers supplies observed values, then existing dashboard/API/command/portal show them with correct units. Real provider account telemetry cannot be claimed verified from fixtures.
- Existing HTTP 9router/NineGuard tracking and key budgets keep their behavior.
- Full server-tag Go tests/build, existing JS tests, syntax checks for edited JS; rebuild/restart scratch binary for actual desktop/mobile light/dark browser smoke. No live provider mutations or production restart.

## Delivery workflow

Write and approve implementation plan after this spec review. Implement on a feature branch, update README/Provider Guide/glossary/request-flow docs where applicable, and record measured verification limits. No changes to API keys, OAuth grants, inference bodies or account entitlement are required for telemetry reads.

## Verified implementation

- Actual NineGuard→CLIProxyAPI→local upstream response headers produced 47% remaining for Claude utilization 0.53; client quota refresh made no extra inference request.
- Regression tests cover numeric parsing, zero/unknown, invalid reset timestamp, stale observation, account/provider isolation, disabled/disconnected accounts, conservative aggregation, private client projection, alert scope separation and command units.
- Full server-tag Go tests/build passed; race checks passed for providerengine/upstream. JavaScript suite passed 73 tests; edited modules passed syntax checks.
- Real browser smoke used clearly labelled response fixtures for SDK dashboard/portal observed, stale, waiting and incomplete display on desktop/mobile 390px. No actual provider account was accessed or modified.
- Independent backend/surface review findings were addressed: restricted-key visibility, nonserializable reset bounds, scope-specific alert state, missing-percent completeness/recovery and minimum-account provenance.
