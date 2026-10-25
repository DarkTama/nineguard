// Client Portal helpers for key validation, masking, and storage

const STORAGE_KEY = 'nineguard_client_key';

export function normalizeKey(key) {
  if (typeof key !== 'string') return '';
  let k = key.trim();
  if (k.toLowerCase().startsWith('bearer ')) {
    k = k.slice(7).trim();
  }
  return k;
}

export function isValidKeyFormat(key) {
  const k = normalizeKey(key);
  if (!k) return false;
  return k.startsWith('sk-');
}

export function maskKey(key) {
  const k = normalizeKey(key);
  if (!k) return '-';
  if (k.length <= 10) {
    return 'sk-...' + k.slice(-2);
  }
  // If key starts with sk-ng-, keep prefix
  if (k.startsWith('sk-ng-')) {
    return 'sk-ng-...' + k.slice(-4);
  }
  return k.slice(0, 6) + '...' + k.slice(-4);
}

export function buildHeaders(key) {
  const k = normalizeKey(key);
  return {
    'Authorization': 'Bearer ' + k
  };
}

export function getStoredPortalKey(storage = (typeof localStorage !== 'undefined' ? localStorage : null)) {
  if (!storage) return '';
  try {
    return normalizeKey(storage.getItem(STORAGE_KEY) || '');
  } catch {
    return '';
  }
}

export function setStoredPortalKey(key, storage = (typeof localStorage !== 'undefined' ? localStorage : null)) {
  if (!storage) return;
  try {
    const k = normalizeKey(key);
    if (k) {
      storage.setItem(STORAGE_KEY, k);
    } else {
      storage.removeItem(STORAGE_KEY);
    }
  } catch { /* storage unavailable */ }
}

export function clearStoredPortalKey(storage = (typeof localStorage !== 'undefined' ? localStorage : null)) {
  if (!storage) return;
  try {
    storage.removeItem(STORAGE_KEY);
  } catch { /* storage unavailable */ }
}

// Status of a used/limit pair: exhausted at 100%, near from 80% (the server's warning line).
function budgetTone(pct) {
  return pct >= 100 ? 'err' : pct >= 80 ? 'warn' : 'ok';
}

// What the portal's status line says about the key's own token budget (GET /v1/quota).
// kind: 'unknown' (no data), 'unlimited', 'key' (one budget) or 'models' (per-model budgets).
// For 'models', total sums the ACTIVE budgets (each row's used capped at its limit, so one
// overrun cannot hide room elsewhere); its tone is the worst row's, since one used-up model is
// already blocked. inactive lists budgets that are kept but not enforced.
export function budgetSummary(quota) {
  if (!quota) return { kind: 'unknown' };
  const all = Array.isArray(quota.model_quotas) ? quota.model_quotas.filter((m) => m && m.limit > 0) : [];
  const toRow = (m) => {
    const used = Math.max(0, m.used || 0);
    const pct = Math.min(100, (used / m.limit) * 100);
    return { modelId: m.model_id, used, limit: m.limit, left: Math.max(0, m.limit - used), pct, period: m.period || '', resetAt: m.reset_at || null, tone: budgetTone(pct), reason: m.inactive_reason || '' };
  };
  const rows = all.filter((m) => !m.inactive).map(toRow);
  const inactive = all.filter((m) => m.inactive).map(toRow);
  if (rows.length) {
    rows.sort((a, b) => b.pct - a.pct);
    const used = rows.reduce((n, r) => n + Math.min(r.used, r.limit), 0);
    const limit = rows.reduce((n, r) => n + r.limit, 0);
    const periods = new Set(rows.map((r) => r.period));
    const resetAt = rows.map((r) => r.resetAt).filter(Boolean).sort((a, b) => Date.parse(a) - Date.parse(b))[0] || null;
    const total = {
      used, limit, left: limit - used, pct: (used / limit) * 100,
      tone: rows[0].tone, period: periods.size === 1 ? rows[0].period : 'mixed', resetAt,
    };
    return { kind: 'models', rows, total, inactive, tightest: rows[0], exhausted: rows.filter((r) => r.pct >= 100).length };
  }
  if (inactive.length) return { kind: 'unlimited', inactive };
  const kq = quota.key_quota;
  if (kq && kq.limit > 0) {
    const used = Math.max(0, kq.used || 0);
    const pct = Math.min(100, (used / kq.limit) * 100);
    return { kind: 'key', used, limit: kq.limit, left: Math.max(0, kq.limit - used), pct, period: kq.period || '', resetAt: kq.reset_at || null, tone: budgetTone(pct) };
  }
  return { kind: 'unlimited', inactive: [] };
}

// "daily" -> "daily", "total" -> "lifetime"; used in "of your 1M daily budget".
export function periodWord(period) {
  return period === 'total' ? 'lifetime' : (period || '');
}
