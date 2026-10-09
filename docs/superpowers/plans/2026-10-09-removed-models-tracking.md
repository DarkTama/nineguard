# Removed Models Tracking (Soft-Delete)

**Status:** Planned, not started
**Raised:** model groups keep models that upstream no longer returns.

---

## Problem

`SyncFromProviders` (`internal/models/models.go`) hard-deletes rows from `models` when a provider stops returning them. It never touches `model_groups.models`, which is a JSON list of names.

Result:
- A model group keeps names for models that no longer exist.
- Those names still count in `models_count` and still pass the key's allow check (`IsModelAllowed`).
- A key with that group sees the model in the dashboard, but a request fails upstream with a generic error.
- After a sync there is no record that a model was ever removed. Old traffic, log and report rows only store the model name as text, so they cannot say why a model has gone.

## Decision

Option 3 from the discussion: **keep group entries stored, treat missing models as unavailable, and give admins a way to clean up.** Do not prune groups silently, because one truncated upstream response would then destroy group membership for good.

To make "removed from provider" a fact and not a guess, models are soft-deleted.

## Design

### Data
- `ALTER TABLE models ADD COLUMN removed_at DATETIME` (NULL = present).
- Sync sets `removed_at` when a provider returned a non-empty list that no longer contains the model. A model that comes back clears `removed_at`.
- A provider that returns an empty list or errors marks nothing as removed.
- Rows are hard-deleted only when the provider itself is deleted (existing path in `providers.go`) or a user deletes the model on purpose.
- Models removed before this ships cannot be recovered. Tags only appear for removals from then on.

### Behaviour

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

### Risks
- **Truncated upstream list:** guarded by the non-empty rule and auto-clear on reappearance. Tags and "unavailable" never delete data.
- **Name-only history:** rows for models that were hard-deleted earlier show no tag.
- **Size:** touches db, models, keys, proxy and several views.

## Steps (one test each)

1. Soft-delete in `models`: migration, `SyncFromProviders` sets and clears `removed_at`, `ListModels` returns it. Test: remove a model upstream, sync, row stays with `removed_at`. Bring it back, flag clears. Empty list marks nothing.
2. Groups and keys: group view reports unavailable entries, "Remove unavailable" endpoint, effective list and `IsModelAllowed` skip removed models, `/v1/models` filter. Test: group with a removed model, key allow check false.
3. Proxy: clear 4xx for a removed model, recorded in traffic.
4. UI tags: Models page greying, group editor labels and button, "removed" tag in Traffic, Logs and Reports.
5. Docs: update the glossary and this plan's status.

## Decisions taken
- Requests to a removed model get a 4xx with a clear message.
- The Models page greying is in scope.
- Done after the small plugin leftovers, so those ship together with the reports layout fix.
