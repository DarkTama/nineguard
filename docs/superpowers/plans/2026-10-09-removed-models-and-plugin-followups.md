# Removed Models Tracking and Plugin Follow-ups

**Status:** Planned, not started
**Shipped before this plan:** v1.2.2 (mobile bottom navigation, phone layout fixes, Register HTTP Plugin fix, endpoint test, secret copy, edit/rotate/delete).

Two tracks. Track A is the larger change and fixes a reported bug. Track B collects the small plugin items that did not ship in v1.2.2. They share a release so the work is reviewed and tested together.

Order: **Track B first** (small, isolated), then **Track A**.

---

## Track A: Removed Models Tracking (Soft-Delete)

### Problem

`SyncFromProviders` (`internal/models/models.go`) hard-deletes rows from `models` when a provider stops returning them. It never touches `model_groups.models`, which is a JSON list of names.

Result:
- A model group keeps names for models that no longer exist.
- Those names still count in `models_count` and still pass the key's allow check (`IsModelAllowed`).
- A key with that group sees the model in the dashboard, but a request fails upstream with a generic error.
- After a sync there is no record that a model was ever removed. Old traffic, log and report rows only store the model name as text, so they cannot say why a model has gone.

### Decision

Keep group entries stored, treat missing models as unavailable, and give admins a way to clean up. Do not prune groups silently, because one truncated upstream response would then destroy group membership for good.

To make "removed from provider" a fact and not a guess, models are soft-deleted.

### Design

**Data**
- `ALTER TABLE models ADD COLUMN removed_at DATETIME` (NULL = present).
- Sync sets `removed_at` when a provider returned a non-empty list that no longer contains the model. A model that comes back clears `removed_at`.
- A provider that returns an empty list or errors marks nothing as removed.
- Rows are hard-deleted only when the provider itself is deleted (existing path in `providers.go`) or a user deletes the model on purpose.
- Models removed before this ships cannot be recovered. Tags only appear for removals from then on.

**Behaviour**

| Area | Change |
|---|---|
| Model groups | Entries for removed models are labelled "Removed from provider". The group shows how many are unavailable. A "Remove unavailable" button deletes them from the group. Wildcards such as `openrouter/*` are never flagged. |
| Key effective list | `GetEffectiveAllowedModels` skips removed models. |
| `/v1/models` | Does not list removed models. |
| Requests to a removed model | Clear 4xx error saying the model was removed from its provider, replacing the generic upstream failure. Recorded in traffic with that message. |
| Models page | Removed models appear greyed with a "Removed from provider" note until cleared. |
| Traffic Explorer, Log Explorer | Small "removed" tag next to the model name on old rows. |
| Usage Reports | Same tag on model rows. Usage history is kept. |

The tag reads one flag (`removed_at` via the existing `LEFT JOIN models` in `traffic.go`), so it is the same everywhere.

**Risks**
- Truncated upstream list: guarded by the non-empty rule and auto-clear on reappearance. Tags and "unavailable" never delete data.
- Name-only history: rows for models that were hard-deleted earlier show no tag.
- Size: touches db, models, keys, proxy and several views.

### Steps (one test each)

1. Soft-delete in `models`: migration, `SyncFromProviders` sets and clears `removed_at`, `ListModels` returns it. Test: remove a model upstream, sync, row stays with `removed_at`. Bring it back, flag clears. Empty list marks nothing.
2. Groups and keys: group view reports unavailable entries, "Remove unavailable" endpoint, effective list and `IsModelAllowed` skip removed models, `/v1/models` filter. Test: group with a removed model, key allow check false.
3. Proxy: clear 4xx for a removed model, recorded in traffic.
4. UI tags: Models page greying, group editor labels and button, "removed" tag in Traffic, Logs and Reports.
5. Docs: update the glossary and this plan's status.

### Decisions taken
- Requests to a removed model get a 4xx with a clear message.
- The Models page greying is in scope.

---

## Track B: Plugin Follow-ups

Items that did not ship in v1.2.2. Each is small and independent.

### B1. Custom settings for HTTP plugins

**Gap:** the drawer has no field for an HTTP plugin's `default_settings`. The backend already stores it and passes the merged result to the plugin as `context.settings`, with precedence key over group over global (`internal/plugins/resolve.go`). Only the built-ins (Headroom, Caveman, Ponytail, SecretGuard) have a settings form.

**Design (proposal, to confirm)**
- Add a "Settings" block to the HTTP section of the Configure drawer: a JSON text area, monospace, with the current `default_settings`.
- Validate before save: must parse as a JSON object. Show the parse error inline, and block Save until it is valid. Cap size (for example 8 KB) so a bad paste cannot bloat the table.
- Show a one-line hint: "Sent to your plugin as `context.settings`. Scope rules can override individual keys."
- Optional on the register form, hidden under an "Advanced" disclosure so the common path stays short.
- No schema-driven form for now. A plugin-declared schema is a larger feature and can come later.

**Server:** reject non-object JSON and oversize input with a 400 in `CreatePlugin` and `UpdatePlugin`. Today any string is stored, and an invalid one is silently ignored by `mergeJSON`.

**Test:** create and update with valid, invalid, non-object and oversize settings. Check the merged settings a plugin receives.

### B2. Bypassable option in the register form

**Gap:** the register form always sends `bypassable: true`. The server forces it to false for fail-closed plugins.

**Design**
- Add a checkbox "Allow keys to bypass this plugin", on by default.
- Disabled, unchecked, with a note when Failure Policy is "Fail Closed", matching the server rule.
- The drawer's save already derives `bypassable` from the policy. Add the same checkbox there so it can be changed after registering.

**Test:** register with each combination, check the stored value.

### B3. Admin gate through a live login

**Gap:** the admin-only rule for register, edit, rotate, delete and test-url is covered by handler tests that inject a user, and by hidden buttons. It was not exercised through a real login session.

**Design:** an end-to-end check with auth enabled: sign in as an admin and as an operator, confirm the operator sees no register button or HTTP plugin controls, and that direct API calls return 403. Reuse the existing `cmd/nineguard/security_e2e_test.go` pattern if it fits.

### Decisions taken
- Admin-only registration and management stays.
- No schema-driven settings form yet.

---

## Release

Ship Track B and Track A together, or Track B first if Track A runs long. Bump the version, update the README if the screens change, and re-take affected screenshots.
