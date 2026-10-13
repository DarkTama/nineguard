# 0009. Upstream Quota Tracking and Chat Command Interception

- Status: Accepted
- Date: 2026-10-13

## Context

NineGuard acts as a central AI gateway and proxy. Upstream targets are diverse:
1. Multi-account aggregators like **9router** managing rolling session limits (e.g. 5-hour Anthropic/Codex windows, weekly limits, credit balances).
2. **Cascaded NineGuard gateways** (e.g. company NineGuard `comp-nineguard` relaying to `my-nineguard`).
3. **Direct LLM endpoints** (e.g. NanoGPT, OpenAI, Groq) that lack real-time quota APIs.

Administrators and AI agent clients need visibility into upstream capacities to avoid sudden 429 exhaustion mid-task.

Key design tensions:
1. **Domain Collision**: NineGuard already defines `Quota` (ADR 0005, ADR 0007) for client key token budgets. Conflating client budgets with upstream capacity causes operational and vocabulary ambiguity.
2. **Multi-Account Privacy vs Diagnosis**: Upstream aggregators like 9router manage multiple accounts per provider. Admins need granular connection-level diagnostics, while client agents should not receive raw account emails or identity metadata.
3. **Cascaded Gateways**: In corporate environments (`my-nineguard -> comp-nineguard -> comp-9router`), client instances lack administrative credentials on the parent gateway and only hold a client key (`sk-ng-...`).
4. **Client Accessibility**: Agents operating in terminal/IDE environments (Cursor, Cline, Continue) need quota checks without leaving the chat interface or manually constructing curl commands.

## Decision

1. **Explicit Domain Separation**:
   - Internal client budgets remain **Key Quota** and **Model Quota**.
   - External provider capacity is named **Upstream Quota** (or Provider Limit).
2. **Provider Quota Types**:
   - Each upstream provider record in SQLite `providers` carries a `quota_type`:
     - `disabled`: Standard OpenAI endpoints without quota tracking (e.g. NanoGPT).
     - `9router`: Direct connection to 9router HTTP API (`/api/providers/client` and `/api/usage/[connectionId]`).
     - `nineguard`: Cascaded upstream NineGuard gateway queried via `GET /v1/quota`.
3. **Privacy-Preserving Client Views**:
   - **Admin UI (`/quotas` Upstream Tab)**: Displays individual account cards with connection IDs, labels, reset timers, and per-account reload triggers.
   - **Client API (`GET /v1/quota`)**: Filters models by the requesting key's Model Group access. Merges accounts from the same provider into a single **Merged Account Pool**:
     - `used` = sum of used capacity across accounts.
     - `total` = sum of total capacity across accounts.
     - `reset_at` = earliest reset timestamp across accounts.
     - Account identifiers, counts, and email addresses are omitted.
4. **Chat Command Interception (`/quota`)**:
   - In `/v1/chat/completions`, if the final user message equals `/quota`, `/limits`, or `/usage` (trimmed, case-insensitive), the proxy short-circuits.
   - It bypasses upstream LLM forwarding and immediately returns a formatted Markdown status report (JSON or SSE chunks matching request mode).
   - Consumes 0 tokens and incurs zero upstream API cost.
5. **Caching & Graceful Degradation**:
   - Upstream quota responses are cached in memory with a 60-second TTL.
   - Upstream provider errors (401, 429, timeout) return `status: "error"` or `status: "degraded"` with explicit error descriptions and advisory badges (⚠️ for capacity < 25%). Upstream quota failures never block normal proxy traffic.
6. **Authentication Handshake**:
   - Upstream credentials (including 9router dashboard passwords or cascaded NineGuard client keys) are stored in SQLite and managed through the NineGuard UI without modifying host environment files or restarting processes.

## Consequences

- Clear distinction in terminology between client token caps and upstream pool capacity.
- Zero token waste for agent developers checking quota balances inside IDE chat windows.
- Full compatibility with cascaded organizational gateways.
- Privacy maintained for multi-account pool owners.
