# 0007. Per-API-Key Model Quotas with an exclusive Quota Mode

- Status: Accepted
- Date: 2026-10-12

## Context

ADR 0005 gave each API Key one cumulative Key Quota across all models. Admins need to give each person (API Key) a separate token budget per model, e.g. Ari gets models A, B, C with different limits on each, plus a dedicated page to monitor them.

Options considered:

1. **Combination**: Key Quota and per-model limits both enforced; blocked if either is exceeded.
2. **Exclusive mode**: a key enforces either one cumulative Key Quota or per-model limits, never both.
3. **Global per-model defaults** with per-key overrides.
4. **Limits on Model Groups**: as a template copied to keys, inherited live, or as one pool shared by the group.
5. **Targets**: exact Models only, patterns as a shared pool, or patterns expanded per model.

## Decision

1. **Quota Mode** per API Key: `key` (one Key Quota, existing behaviour) or `model` (Model Quotas). Exactly one is enforced. The inactive mode's settings are kept, not deleted, so switching back restores them.
2. **No global defaults.** Model Quotas exist only on an API Key.
3. **Model Groups carry no quota role.** They already serve as access template and plugin scope (ADR 0002); a third role would let one group edit change access, plugins, and budgets for many people at once. The editor only uses groups to list pickable models.
4. **Exact Models only.** One row per (API Key, Model). No wildcards, no overlap rules.
5. Each Model Quota has its **own Quota Period** (none/daily/weekly/monthly/total, UTC-anchored per ADR 0005) and a **limit > 0**. A Model with no Model Quota is unlimited; blocking a model is the job of access control.
6. A Model Quota can only be **created** for a Model the key can use. If access is later lost (group/allowed-models change, Disabled or Removed Model) the row becomes an **Inactive Model Quota**: kept, not enforced, and applied again if access returns.
7. **Enforcement** stays Soft Post-Facto (ADR 0005). In `model` mode the check runs after body parsing, model resolution, access check and disabled check, and before plugins. A blocked request gets HTTP 429 with `type: insufficient_quota`, `code: model_quota_exceeded`, a message naming the model, and `Retry-After` from that row's period. In `key` mode the existing pre-body check is unchanged.
8. Usage is aggregated from `traffic_logs` by resolved Model ID (ADR 0008), so switching modes mid-window counts tokens already used in that window.

## Consequences

- Existing keys default to `key` mode; no data migration of limits.
- Clients can tell "this model is exhausted" (`model_quota_exceeded`) from "this key is exhausted" (`quota_exceeded`).
- Admins type limits per key; repetitive for many keys with the same models. Group templates can be revisited later if this hurts.
- Pattern pools (e.g. one budget for all Claude models) are not possible in v1.
- A Model's ID changing (provider prefix rename) leaves the old Model Quota Inactive; the admin must add a new one.
