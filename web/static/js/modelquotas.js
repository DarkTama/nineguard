// Pure helpers for Model Quotas (ADR 0007/0008). No DOM access, so `node --test` can run them.
// matchModelPattern / isModelAllowed mirror internal/keys (Go); isCanonicalModelId mirrors
// providers.ResolveModelID. The server stays the authority: these only filter what the UI offers.

export const MODEL_QUOTA_PERIODS = Object.freeze([
  ['daily', 'Daily (UTC midnight)'],
  ['weekly', 'Weekly (UTC Monday)'],
  ['monthly', 'Monthly (UTC 1st)'],
  ['total', 'Lifetime (All-time)'],
]);

// Days in each reset window, used to scale the 7-day daily average into a limit suggestion.
const PERIOD_DAYS = Object.freeze({ daily: 1, weekly: 7, monthly: 30 });

// Preset limits (1.5x and 2x of the typical usage in one window) for the selected reset period.
// avgPerDay is the 7-day average from the velocity endpoint. Lifetime/none/unknown periods and
// a missing history give no presets: a daily average does not map onto them.
export function velocityPresets(avgPerDay, period) {
  const days = PERIOD_DAYS[period];
  if (!days || !(avgPerDay > 0)) return [];
  return [1.5, 2].map((factor) => ({
    factor,
    label: `${factor}x ${period}`,
    limit: Math.max(1, Math.ceil(avgPerDay * days * factor)),
  }));
}

// Status of a quota from its percent (uncapped) and the warning threshold.
export function quotaStatus(percent, warn, exhausted = percent >= 100) {
  if (exhausted) return 'exhausted';
  return percent >= warn ? 'near' : 'ok';
}

export function barTone(status) {
  return status === 'exhausted' ? 'err' : status === 'near' ? 'warn' : 'ok';
}

// "9router/claude-opus-4" -> "claude-opus-4"; "openrouter/anthropic/x" -> "x".
export function shortModel(id) {
  const s = String(id || '');
  const i = s.lastIndexOf('/');
  return i >= 0 ? s.slice(i + 1) : s;
}

// resetAt is an ISO string or null (lifetime quota never resets).
export function formatResetIn(resetAt, now = Date.now()) {
  if (!resetAt) return 'never';
  const ms = Date.parse(resetAt) - now;
  if (!Number.isFinite(ms)) return 'never';
  if (ms < 60000) return '<1m';
  const mins = Math.floor(ms / 60000);
  const d = Math.floor(mins / 1440);
  const hr = Math.floor((mins % 1440) / 60);
  const mn = mins % 60;
  if (d > 0) return `${d}d ${hr}h`;
  if (hr > 0) return `${hr}h ${mn}m`;
  return `${mn}m`;
}

const lc = (s) => String(s || '').toLowerCase();

// Port of keys.matchModelPattern.
export function matchModelPattern(allowed, model) {
  allowed = String(allowed || '').trim();
  if (allowed === '*' || allowed === '' || lc(allowed) === 'all') return true;
  if (lc(allowed) === lc(model)) return true;
  if (allowed.endsWith('/*')) {
    const prefix = allowed.slice(0, -2);
    if (lc(model).startsWith(lc(prefix) + '/')) return true;
  }
  if (allowed.startsWith('*.')) {
    if (lc(model).endsWith(lc(allowed.slice(1)))) return true;
  }
  if (model.includes('/') && !allowed.includes('/')) {
    if (lc(model.slice(model.indexOf('/') + 1)) === lc(allowed)) return true;
  }
  if (allowed.includes('/') && !model.includes('/')) {
    if (lc(allowed.slice(allowed.indexOf('/') + 1)) === lc(model)) return true;
  }
  return false;
}

// access = { mode: 'all'|'group'|'custom', groupIds: [], allowed: [] }; groups = GET /model-groups list.
export function effectiveEntries(access, groups = []) {
  if (access.mode === 'group') {
    const out = [];
    for (const g of groups) if ((access.groupIds || []).includes(g.id)) out.push(...(g.models || []));
    return out;
  }
  if (access.mode === 'custom') return access.allowed || [];
  return ['*'];
}

export function isModelAllowed(access, groups, id) {
  if (access.mode === 'all') return true;
  const entries = effectiveEntries(access, groups);
  if (access.mode === 'custom' && entries.length === 0) return true; // empty custom list = all (same as the server)
  if (entries.some((e) => e === '*' || lc(e) === 'all')) return true;
  return entries.some((e) => matchModelPattern(e, id));
}

// providers = GET /providers list (default provider first, as the server returns it).
export function isCanonicalModelId(providers, id) {
  const active = (providers || []).filter((p) => p.is_active);
  if (!active.length) return false;
  const target = lc(String(id || '').trim());
  if (!target) return false;
  // A provider whose "<prefix>/" starts the id wins (prefix match is case-sensitive on the server).
  for (const p of active) {
    if (p.prefix && String(id).startsWith(p.prefix + '/')) {
      return lc(p.prefix + '/' + String(id).slice(p.prefix.length + 1)) === target;
    }
  }
  const fallback = active.find((p) => p.is_default) || active[0];
  return !fallback.prefix; // bare id is canonical only when the fallback provider has no prefix
}

// models = GET /models list. taken = Set of lowercased ids that already have a row.
export function pickableModels(models, providers, access, groups, taken = new Set()) {
  return (models || [])
    .filter((m) => m && m.id && m.enabled && !m.removed)
    .map((m) => m.id)
    .filter((id) => !taken.has(lc(id)) && isCanonicalModelId(providers, id) && isModelAllowed(access, groups, id));
}

// Groups the pickable ids: each id appears once, under the first group that lists it.
export function pickerSections(pickable, groups = []) {
  const placed = new Set();
  const sections = [];
  for (const g of groups) {
    const ids = pickable.filter((id) => !placed.has(id) && (g.models || []).some((e) => matchModelPattern(e, id) && e !== '*' && lc(e) !== 'all'));
    ids.forEach((id) => placed.add(id));
    if (ids.length) sections.push({ title: g.name, models: ids });
  }
  const rest = pickable.filter((id) => !placed.has(id));
  if (rest.length) sections.push({ title: 'Other allowed models', models: rest });
  return sections;
}

// rows: [{model_id, quota_limit}] -> first problem as text, '' when fine.
export function validateModelQuotaRows(rows) {
  const seen = new Set();
  for (const r of rows) {
    const id = String(r.model_id || '').trim();
    if (!id) return 'A model quota has no model.';
    if (seen.has(lc(id))) return `Duplicate model quota for ${id}.`;
    seen.add(lc(id));
    const n = Number(r.quota_limit);
    if (!Number.isInteger(n) || n <= 0) return `Set a limit greater than 0 for ${id}.`;
  }
  return '';
}

// ── Quotas page filters (route params <-> state) ──
export const QUOTA_FILTER_DEFAULTS = Object.freeze({ q: '', mode: 'all', status: 'all', unlimited: '' });

const pick = (v, allowed, def) => (allowed.includes(v) ? v : def);

export function quotaFiltersFromParams(params = {}) {
  const d = QUOTA_FILTER_DEFAULTS;
  return {
    q: typeof params.q === 'string' ? params.q : d.q,
    mode: pick(params.mode, ['all', 'key', 'model'], d.mode),
    status: pick(params.status, ['all', 'near', 'exhausted', 'inactive'], d.status),
    unlimited: params.unlimited === '1' ? '1' : '',
  };
}

export function quotaParamsFromFilters(f) {
  const out = {};
  for (const [k, def] of Object.entries(QUOTA_FILTER_DEFAULTS)) if (f[k] !== def && f[k] !== '' && f[k] != null) out[k] = String(f[k]);
  return out;
}

const keyRows = (k) => [...(k.key_quota ? [k.key_quota] : []), ...(k.model_quotas || [])];

// keys = GET /quotas .keys
export function filterQuotaKeys(keys, f) {
  const q = lc(f.q).trim();
  return (keys || []).filter((k) => {
    if (f.mode !== 'all' && k.quota_mode !== f.mode) return false;
    const hasInactive = (k.model_quotas || []).some((r) => r.inactive);
    if (f.status === 'inactive') return hasInactive;
    if (k.status === 'unlimited' && !(k.model_quotas || []).length && f.unlimited !== '1') return false;
    if (f.status === 'near' && !keyRows(k).some((r) => !r.inactive && (r.status === 'near' || r.status === 'exhausted'))) return false;
    if (f.status === 'exhausted' && !keyRows(k).some((r) => !r.inactive && r.status === 'exhausted')) return false;
    if (q && !lc(k.name).includes(q) && !(k.model_quotas || []).some((r) => lc(r.model_id).includes(q))) return false;
    return true;
  });
}

// Rows at or above the warning threshold, exhausted first, then highest percent.
export function nearLimitRows(keys, max = 5) {
  const out = [];
  for (const k of keys || []) {
    if (k.key_quota && (k.key_quota.status === 'near' || k.key_quota.status === 'exhausted')) {
      out.push({ keyId: k.id, keyName: k.name, label: 'all models', percent: k.key_quota.percent, status: k.key_quota.status, resetAt: k.key_quota.reset_at });
    }
    for (const r of k.model_quotas || []) {
      if (!r.inactive && (r.status === 'near' || r.status === 'exhausted')) {
        out.push({ keyId: k.id, keyName: k.name, label: shortModel(r.model_id), percent: r.percent, status: r.status, resetAt: r.reset_at });
      }
    }
  }
  out.sort((a, b) => (a.status === 'exhausted') !== (b.status === 'exhausted') ? (a.status === 'exhausted' ? -1 : 1) : b.percent - a.percent);
  return out.slice(0, max);
}

// Collapsed head of a per-model key on the Quotas page: the sum over ACTIVE Model Quotas (inactive
// ones are not enforced). percent = total used / total limit; near/exhausted count the rows that
// crossed. period is 'mixed' when the active rows reset on different periods; resetAt is the soonest.
export function aggregateModelQuotas(rows, warn) {
  const active = (rows || []).filter((r) => !r.inactive && r.quota_limit > 0);
  let used = 0;
  let limit = 0;
  let near = 0;
  let exhausted = 0;
  let resetAt = null;
  const periods = new Set();
  for (const r of active) {
    used += Math.max(0, r.usage || 0);
    limit += r.quota_limit;
    periods.add(r.quota_period);
    if (r.status === 'exhausted') exhausted++;
    else if (r.status === 'near') near++;
    if (r.reset_at && (!resetAt || Date.parse(r.reset_at) < Date.parse(resetAt))) resetAt = r.reset_at;
  }
  const percent = limit > 0 ? Math.round((used / limit) * 100) : 0;
  return {
    active: active.length,
    used,
    limit,
    percent,
    status: quotaStatus(percent, warn),
    near,
    exhausted,
    period: periods.size === 1 ? [...periods][0] : (periods.size ? 'mixed' : ''),
    resetAt,
  };
}
