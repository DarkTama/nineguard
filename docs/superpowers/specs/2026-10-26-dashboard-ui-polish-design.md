# Dashboard UI Polish

- Date: 2026-10-26
- Status: Implemented
- Scope: web dashboard only; no API, schema or enforcement changes.

## Problem

Several admin pages drifted from the shared components: misaligned header actions, long unsectioned dialogs, rows that navigate when users expect them to expand, a Plugins page that renders blank while loading and hides where a plugin applies, cost settings duplicated on observability pages, and emoji used as icons (some referenced icons, such as `dollar-sign`, were missing from the sprite and rendered empty).

## Decisions

- Keep the existing visual language (JetBrains Mono, `.card`, `.seg`, `.skel`, `.page-actions`, `menu()`, `formDialog()`). Polish aligns pages to it instead of adding a new style.
- Icons come from the Lucide sprite in `web/static/index.html`. Emoji are removed from UI copy; `<option>` text is plain.
- Cost & Currency is a global setting and lives only under **Providers → Configure**. Traffic, Usage Reports and Models no longer expose it.

## Page changes

| Page | Change |
|---|---|
| Providers | `Configure` + `Add provider` in `.page-actions`; skeleton while loading; empty state has an add action. |
| Endpoints & Keys | Key table first; `Generate key` in page head; row actions are `Edit` + overflow menu; key dialog grouped into Name and access, Quota, Plugins and cost; one segmented-control style; English copy; name required on create; feedback while the dialog loads. |
| Quotas | Clicking a per-model row toggles it; key name no longer navigates, an `Edit key` button does; skeleton; `.tabs` for Client/Upstream; upstream provider cards collapse with a one-line summary. |
| Plugins | Skeleton and retry error state; each card shows where it applies; `Apply to…` dialog binds a plugin to a key or model group; precedence explained once; vertical reorder controls. |
| Usage Reports | Eight summary cards: existing five plus Error rate, Avg latency and Tokens saved, all derived from the existing report response. |
| Traffic | Lucide icons for Images, Fallback, Lost and Thinking chips and row badges; Live, Alerts (`bell`) and Export grouped at the toolbar end. |

## Fixed defects

- Missing sprite icons (`dollar-sign`, `data_usage`).
- Low remaining upstream bars rendered green (`danger` tone had no style).
- Upstream reset confirmation ignored its button text (`okText` vs `confirmText`).
- Plugin stacking warning reappeared on every enable (acknowledgement never stored).
- Upstream tab re-render kept stale instances alive.

## Verification

`node --check` on edited files, `node --test web/jstest/*.mjs`, server build, and a browser pass of each page at desktop and 390px width in light and dark themes.

## Follow-up fixes

- Plugin cards show their pipeline position as 1, 2, 3… with a visible up/down rail; the stored `pipeline_order` (5, 10, 20…) is an internal sort key and is no longer displayed.
- Providers explains routing with a worked model id under the header instead of a disclosure beside the search box.
- Usage Reports PDF prints as a white, paginated A4 document in any theme; the portal has a **Download PDF** summary that never includes the raw key.
- SDK account limits use colored remaining bars (green above 70%, amber 30–70%, red below 30%) in Quotas and the portal; unknown or stale observations stay grey or dimmed.
- The plugin probe uses the searchable model select instead of a browser datalist.
