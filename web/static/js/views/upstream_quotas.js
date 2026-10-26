// Upstream Quotas view: Live visibility into upstream provider capacity, rolling limits, and credit balances.
import { api } from '../api.js';
import { h, icon, toast, emptyState, fmtNum, confirmDialog } from '../ui.js';
import { formatResetIn } from '../modelquotas.js';
import { formatTokensCompact } from '../quotahelpers.js';
import { isPercentBucket, remainingLabel, observationLabel, telemetryStateLabel, freshPercent, remainingTone, sdkMeter } from '../upstreamtelemetry.js';

export const upstreamBarTone = remainingTone;

export function upstreamAlertRows(providers, max = 6) {
  if (!Array.isArray(providers)) return { rows: [], total: 0 };

  const errorProblemRows = [];
  const degradedProblemRows = [];
  const bucketRows = [];

  for (const p of providers) {
    if (!p) continue;
    if (!p.quota_type || p.quota_type === 'disabled') continue;
    if (p.status === 'unsupported') continue;

    const providerName = p.provider_name || p.provider_id || '';
    const connections = Array.isArray(p.connections) ? p.connections : [];

    if (connections.length === 0) {
      if (p.status === 'error') {
        errorProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel: '',
          status: 'error',
          errorMsg: p.error_msg || '',
          tone: 'err',
        });
      } else if (p.status === 'degraded') {
        degradedProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel: '',
          status: 'degraded',
          errorMsg: p.error_msg || '',
          tone: 'warn',
        });
      }
      continue;
    }

    for (const conn of connections) {
      if (!conn) continue;
      const accountLabel = conn.account_label || '';

      if (conn.status === 'error') {
        errorProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel,
          status: 'error',
          errorMsg: conn.error_msg || '',
          tone: 'err',
        });
      } else if (conn.status === 'degraded') {
        degradedProblemRows.push({
          kind: 'problem',
          providerName,
          accountLabel,
          status: 'degraded',
          errorMsg: conn.error_msg || '',
          tone: 'warn',
        });
      }

      const buckets = Array.isArray(conn.buckets) ? conn.buckets : [];
      for (const b of buckets) {
        if (!b) continue;
        if (isPercentBucket(b) && !freshPercent(b)) continue;
        if (b.warning === true) {
          const isCredit = Boolean(b.is_credit_balance);
          const remaining = typeof b.remaining === 'number' ? b.remaining : 0;
          const rawPct = isPercentBucket(b) ? (b.remaining_percent ?? 0) : (typeof b.remaining_percentage === 'number' ? b.remaining_percentage : 0);
          const percent = Math.max(0, Math.min(100, Math.round(rawPct)));
          let tone = upstreamBarTone(percent);
          if (isCredit && remaining <= 0) {
            tone = 'err';
          }
          bucketRows.push({
            kind: 'bucket',
            providerName,
            accountLabel,
            bucketName: b.name || '',
            percent,
            isCredit,
            remaining,
            resetAt: b.reset_at || null,
            tone,
          });
        }
      }
    }
  }

  bucketRows.sort((a, b) => {
    const effA = a.isCredit && a.remaining <= 0 ? 0 : a.percent;
    const effB = b.isCredit && b.remaining <= 0 ? 0 : b.percent;
    return effA - effB;
  });

  const allRows = [...errorProblemRows, ...bucketRows, ...degradedProblemRows];
  const total = allRows.length;
  const limit = typeof max === 'number' && max >= 0 ? max : 6;
  const rows = allRows.slice(0, limit);

  return { rows, total };
}

function timeAgo(dateStr) {
  if (!dateStr) return '';
  const diffSec = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (diffSec < 60) return 'just now';
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return `${Math.floor(diffSec / 86400)}d ago`;
}

// True when a provider needs a look: provider/connection errors or degradation, or a warning bucket.
export function upstreamNeedsAttention(p) {
  if (!p) return false;
  if (p.status === 'error' || p.status === 'degraded' || p.error_msg) return true;
  return (p.connections || []).some((c) => c && (c.status === 'error' || c.status === 'degraded'
    || (c.buckets || []).some((b) => b && b.warning === true)));
}

// One-line digest for a collapsed provider card: connections, lowest remaining %, soonest reset.
export function upstreamProviderSummary(p, now = Date.now()) {
  const conns = (p?.connections || []).filter(Boolean);
  let low = null;
  let reset = null;
  for (const c of conns) {
    for (const b of c.buckets || []) {
      if (!b) continue;
      let pct = null;
      if (isPercentBucket(b)) { if (freshPercent(b, now) && Number.isFinite(b.remaining_percent)) pct = b.remaining_percent; }
      else if (!b.is_credit_balance && b.total > 0 && Number.isFinite(b.remaining_percentage)) pct = b.remaining_percentage;
      if (pct != null) low = low == null ? pct : Math.min(low, pct);
      const t = b.reset_at ? Date.parse(b.reset_at) : NaN;
      if (Number.isFinite(t) && t > now && (reset == null || t < reset)) reset = t;
    }
  }
  const parts = [`${conns.length} ${conns.length === 1 ? 'connection' : 'connections'}`];
  if (low != null) parts.push(`lowest ${Math.max(0, Math.min(100, Math.round(low)))}% left`);
  if (reset != null) parts.push(`next reset in ${formatResetIn(new Date(reset).toISOString())}`);
  return parts.join(' · ');
}

// User choices per provider id (true = collapsed); unset providers use the default.
const COLLAPSE_KEY = 'ng.upstream.collapsed';
function loadCollapsed() {
  try { return JSON.parse(localStorage.getItem(COLLAPSE_KEY) || '{}') || {}; } catch { return {}; }
}
const providerKey = (p) => String(p.provider_id || p.provider_name || '');
function isProviderOpen(p, count) {
  const saved = loadCollapsed()[providerKey(p)];
  if (typeof saved === 'boolean') return !saved;
  return count <= 2 || upstreamNeedsAttention(p);
}
function setProviderOpen(p, open) {
  const all = loadCollapsed();
  all[providerKey(p)] = !open;
  try { localStorage.setItem(COLLAPSE_KEY, JSON.stringify(all)); } catch { /* storage unavailable */ }
}

export function renderUpstreamQuotas(container) {
  let alive = true;
  let loading = false;
  let providersData = [];

  const content = h('div', { class: 'mt' },
    h('div', { class: 'skel', style: { height: '140px', borderRadius: '8px' } })
  );

  // Auto-sync interval setting (global, applies to all providers)
  const intervalSel = h('select', { class: 'select', disabled: true });
  const intervalBar = h('div', {
    style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', marginBottom: '4px' }
  },
    icon('refresh'),
    h('span', { style: { fontSize: '12.5px', fontWeight: '600' } }, 'Auto-sync every'),
    intervalSel,
    h('span', { class: 'muted', style: { fontSize: '11.5px' } }, 'Applies to the dashboard and the agent /quota command.')
  );
  container.replaceChildren(intervalBar, content);

  async function loadSyncSettings() {
    try {
      const s = await api.get('/settings/upstream-sync');
      if (!alive) return;
      intervalSel.replaceChildren(...(s.allowed || []).map((v) =>
        h('option', { value: v, selected: v === s.interval }, v)
      ));
      intervalSel.value = s.interval;
      intervalSel.disabled = false;
    } catch (err) {
      if (!alive) return;
      intervalBar.replaceChildren(
        h('span', { class: 'muted', style: { fontSize: '12px' } }, `Sync interval unavailable: ${err.message}`)
      );
    }
  }

  intervalSel.addEventListener('change', async () => {
    intervalSel.disabled = true;
    try {
      const s = await api.post('/settings/upstream-sync', { interval: intervalSel.value });
      toast(`Auto-sync interval set to ${s.interval}`, 'ok');
      intervalSel.value = s.interval;
    } catch (err) {
      toast(`Failed to save interval: ${err.message}`, 'error');
      await loadSyncSettings();
    } finally {
      intervalSel.disabled = false;
    }
  });

  async function load(force = false) {
    if (loading) return;
    loading = true;
    try {
      const url = force ? '/upstream-quotas?force=1' : '/upstream-quotas';
      const res = await api.get(url);
      if (!alive) return;
      providersData = res.providers || [];
      render();
    } catch (err) {
      if (!alive) return;
      content.replaceChildren(
        h('div', { class: 'banner err' }, icon('alert'), ` Failed to load upstream quotas: ${err.message}`)
      );
    } finally {
      loading = false;
    }
  }

  function renderBucketRow(b) {
    if (isPercentBucket(b)) {
      const state = `${telemetryStateLabel(b.observation_state)}${b.incomplete ? ' · incomplete' : ''}`;
      const m = sdkMeter(b);
      return h('tr', null,
        h('td', null, b.name, b.scope && b.scope !== 'account' ? h('div', { class: 'muted' }, b.scope) : null),
        h('td', { class: 'muted' }, b.used_percent == null ? '—' : `${b.used_percent.toFixed(1)}% used`),
        h('td', null,
          h('div', { class: `uq-remaining${m.dim ? ' is-dim' : ''}` },
            h('span', { class: `qbar ${m.tone}`, role: 'img', 'aria-label': `${remainingLabel(b)}` }, h('i', { style: { width: `${m.pct}%` } })),
            h('strong', { class: `qpct ${m.tone}` }, remainingLabel(b))),
          h('div', { class: 'muted uq-state' }, state)),
        h('td', { class: 'muted' }, b.reset_at ? `in ${formatResetIn(b.reset_at)}` : '—')
      );
    }
    const tone = upstreamBarTone(b.remaining_percentage);
    const pct = Math.max(0, Math.min(100, b.remaining_percentage || 0));

    let usedTotalStr = `${formatTokensCompact(b.used)} / ${formatTokensCompact(b.total)}`;
    if (b.is_credit_balance) {
      usedTotalStr = `$${(b.remaining / 100).toFixed(2)}`;
    } else if (b.total <= 0) {
      usedTotalStr = '-';
    }

    const resetStr = b.reset_at ? `in ${formatResetIn(b.reset_at)}` : '-';

    return h('tr', null,
      h('td', { style: { fontWeight: '500' } },
        b.warning ? h('span', { class: 'uq-warn', title: 'Depletion warning (<25%)' }, icon('alert')) : null,
        b.name
      ),
      h('td', { class: 'muted', style: { fontSize: '12px' } }, usedTotalStr),
      h('td', null,
        b.is_credit_balance
          ? h('span', { class: 'badge ok' }, 'Active')
          : h('span', null,
              h('span', { class: `qbar ${tone}`, style: { width: '80px', display: 'inline-block' } },
                h('i', { style: { width: `${pct}%` } })
              ),
              h('span', { class: `qpct ${tone}`, style: { marginLeft: '8px', fontSize: '11px', fontWeight: 'bold' } }, `${pct}%`)
            )
      ),
      h('td', { class: 'muted', style: { fontSize: '12px' } }, resetStr)
    );
  }

  function renderConnectionCard(conn, provider) {
    const hasError = conn.status === 'error';
    const isKeyAllowance = (conn.connection_id || '').includes('allowance');

    const resetBtn = conn.reset_credits > 0 ? h('button', {
      class: 'badge badge-btn err',
      style: { display: 'inline-flex', alignItems: 'center', gap: '4px', cursor: 'pointer', fontWeight: 'bold' },
      type: 'button',
      title: `Use 1 reset now (${conn.reset_credits} available)`,
      onclick: async () => {
        const ok = await confirmDialog({
          title: 'Spend upstream quota reset?',
          body: `Spend 1 irreversible reset grant for "${conn.account_label || conn.connection_id}"? This will refill session and weekly limits on ${provider.provider_name}. (${conn.reset_credits} resets available)`,
          danger: true,
          confirmText: 'Spend reset'
        });
        if (!ok) return;
        try {
          resetBtn.disabled = true;
          resetBtn.textContent = 'Resetting...';
          const res = await api.post(`/upstream-quotas/${provider.provider_id}/connections/${conn.connection_id}/reset`, {
            provider: conn.provider,
            grant_id: conn.next_grant_id || ''
          });
          toast(res.message || 'Reset applied successfully', 'ok');
          await load(true);
        } catch (e) {
          toast(`Reset failed: ${e.message}`, 'error');
        } finally {
          resetBtn.disabled = false;
        }
      }
    }, icon('refresh'), ` ${conn.reset_credits} Reset`) : null;

    const isSharedPool = provider.quota_type === 'nineguard' || conn.plan === 'Shared Upstream Pool';
    const tierBadge = isKeyAllowance
      ? h('span', { class: 'badge ok', style: { fontWeight: 'bold', border: '1px solid var(--ok)' } }, icon('user'), 'Key allowance (admin limit)')
      : (isSharedPool
          ? h('span', { class: 'badge', style: { background: 'var(--hover)', fontWeight: 'bold', border: '1px solid var(--border)' } }, icon('globe'), 'Shared upstream pool')
          : null);

    const planBadge = (conn.plan && conn.plan !== 'Shared Upstream Pool')
      ? h('span', { class: 'badge' }, conn.plan)
      : null;

    return h('section', { class: `uq-conn${isKeyAllowance ? ' uq-conn-allowance' : ''}` },
      h('div', { class: 'uq-conn-head' },
        h('div', { class: 'uq-conn-title' },
          icon(isKeyAllowance ? 'shield' : 'user'),
          h('h3', null, conn.account_label || conn.connection_id),
          tierBadge,
          planBadge,
          resetBtn
        ),
        h('span', { class: `badge ${conn.source === 'sdk' ? 'muted' : conn.status === 'ok' ? 'ok' : 'err'}` }, conn.source === 'sdk' && conn.status === 'ok' ? telemetryStateLabel(conn.observation_state) : conn.status)
      ),
      hasError && conn.error_msg
        ? h('div', { class: 'banner err', style: { padding: '6px 10px', fontSize: '11.5px', marginBottom: '8px' } }, conn.error_msg)
        : null,
      conn.buckets && conn.buckets.length > 0
        ? h('div', { class: 'table-wrap' }, h('table', { class: 'table uq-table' },
            h('thead', null,
              h('tr', null,
                h('th', null, isKeyAllowance ? 'Model / allowance window' : 'Window / bucket'),
                h('th', null, provider.source === 'sdk' ? 'Usage' : isKeyAllowance ? 'Tokens consumed / limit' : 'Used / total'),
                h('th', null, 'Remaining'),
                h('th', null, 'Resets in')
              )
            ),
            h('tbody', null, ...conn.buckets.map(renderBucketRow))
          ))
        : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, conn.source === 'sdk' ? telemetryStateLabel(conn.observation_state) : 'No active quota buckets reported.'),
    );
  }

  function render() {
    const activeTrackers = providersData.filter((p) => p.quota_type && p.quota_type !== 'disabled');

    if (!providersData.length || !activeTrackers.length) {
      content.replaceChildren(
        emptyState(
          'inbox',
          'No upstream quota trackers configured',
          'Configure a provider with the 9router API or cascaded NineGuard protocol in Providers to track live upstream limits.',
          h('button', {
            class: 'btn btn-primary',
            type: 'button',
            style: { marginTop: '12px' },
            onclick: () => location.hash = '#/providers'
          }, icon('server'), 'Manage providers')
        )
      );
      return;
    }

    const providerCards = providersData.map((p) => {
      const isUnsupported = p.quota_type === 'disabled';
      const refreshBtn = h('button', {
        class: 'btn btn-sm',
        type: 'button',
        disabled: isUnsupported || loading,
        title: p.source === 'sdk' ? 'Read latest SDK snapshot; no inference request is sent' : 'Force refresh live quotas from upstream',
        onclick: async () => {
          refreshBtn.disabled = true;
          await load(true);
          toast(`Refreshed quotas for ${p.provider_name}`, 'ok');

          // 30-second cooldown timer
          let cooldown = 30;
          refreshBtn.disabled = true;
          refreshBtn.replaceChildren(icon('refresh'), ` Cooldown (${cooldown}s)`);
          const timer = setInterval(() => {
            cooldown--;
            if (cooldown <= 0 || !alive) {
              clearInterval(timer);
              refreshBtn.disabled = false;
              refreshBtn.replaceChildren(icon('refresh'), 'Refresh');
            } else {
              refreshBtn.replaceChildren(icon('refresh'), ` Cooldown (${cooldown}s)`);
            }
          }, 1000);
        }
      }, icon('refresh'), 'Refresh');

      const protocolLabel = p.quota_type === '9router'
        ? 'Router Protocol'
        : p.quota_type === 'nineguard'
        ? 'NineGuard Cascaded'
        : p.quota_type === 'sdk' ? 'SDK account telemetry' : 'Direct LLM (No Tracker)';

      const lastUpdatedLabel = p.source === 'sdk'
        ? h('span', { class: 'muted', style: { fontSize: '11px' } }, observationLabel(p.observed_at, p.observation_state))
        : p.last_updated ? h('span', { class: 'muted', style: { fontSize: '11.5px', marginLeft: '6px' } }, `Synced ${timeAgo(p.last_updated)}`) : null;

      const open = isProviderOpen(p, providersData.length);
      const bodyId = `uq-body-${String(p.provider_id || p.provider_name || '').replace(/[^\w-]/g, '_')}`;
      const chevron = h('button', {
        class: 'icon-btn sm', type: 'button', 'aria-expanded': String(open), 'aria-controls': bodyId,
        'aria-label': `${open ? 'Collapse' : 'Expand'} ${p.provider_name}`,
      }, icon(open ? 'chevron-down' : 'chevron-right'));
      const summary = h('span', { class: 'muted uq-summary', hidden: open }, upstreamProviderSummary(p));
      const cardBody = h('div', { id: bodyId, class: 'uq-body', hidden: !open },
        isUnsupported
          ? h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } },
              'This provider connects directly to an LLM without upstream limit telemetry. Switch protocol in Providers menu to enable.'
            )
          : p.connections && p.connections.length > 0
          ? h('div', null, ...p.connections.map((c) => renderConnectionCard(c, p)))
          : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, p.source === 'sdk' ? 'No connected accounts. Connect an account in Providers.' : 'No account connections found on upstream.')
      );
      const setOpen = (next) => {
        setProviderOpen(p, next);
        cardBody.hidden = !next;
        summary.hidden = next;
        chevron.setAttribute('aria-expanded', String(next));
        chevron.setAttribute('aria-label', `${next ? 'Collapse' : 'Expand'} ${p.provider_name}`);
        chevron.replaceChildren(icon(next ? 'chevron-down' : 'chevron-right'));
      };
      // Clicks anywhere on the header (including the chevron) bubble here; Refresh stops them.
      refreshBtn.addEventListener('click', (e) => e.stopPropagation());

      return h('div', { class: 'card uq-card' },
        h('div', { class: 'card-head uq-head', onclick: () => setOpen(cardBody.hidden) },
          h('div', { class: 'uq-head-main' },
            chevron,
            icon('server'),
            h('h2', { style: { margin: 0, fontSize: '15px' } }, p.provider_name),
            h('span', { class: 'badge', style: { background: 'var(--hover)' } }, protocolLabel),
            h('span', { class: `badge ${p.source === 'sdk' ? 'muted' : p.status === 'ok' ? 'ok' : p.status === 'degraded' ? 'warn' : 'muted'}` }, p.source === 'sdk' && p.status === 'ok' ? telemetryStateLabel(p.observation_state) : p.status),
            lastUpdatedLabel,
            summary
          ),
          refreshBtn
        ),
        p.error_msg
          ? h('div', { class: 'banner err', style: { margin: '8px 14px' } }, icon('alert'), ' ', p.error_msg)
          : null,
        cardBody
      );
    });

    content.replaceChildren(...providerCards);
  }

  loadSyncSettings();
  load(false);

  return () => {
    alive = false;
  };
}
