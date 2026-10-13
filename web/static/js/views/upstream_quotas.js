// Upstream Quotas view: Live visibility into upstream provider capacity, rolling limits, and credit balances.
import { api } from '../api.js';
import { h, icon, toast, emptyState, fmtNum } from '../ui.js';
import { formatResetIn } from '../modelquotas.js';
import { formatTokensCompact } from '../quotahelpers.js';

export function upstreamBarTone(pct) {
  if (pct > 70) return 'ok';
  if (pct >= 30) return 'warn';
  return 'danger';
}

export function renderUpstreamQuotas(container) {
  let alive = true;
  let loading = false;
  let providersData = [];

  const content = h('div', { class: 'mt' },
    h('div', { class: 'skel', style: { height: '140px', borderRadius: '8px' } })
  );
  container.replaceChildren(content);

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
        b.warning ? h('span', { title: 'Depletion warning (<25%)', style: { marginRight: '6px' } }, '⚠️') : null,
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

  function renderConnectionCard(conn) {
    const hasError = conn.status === 'error';

    return h('div', {
      style: {
        background: 'var(--panel)',
        border: '1px solid var(--border)',
        borderRadius: '8px',
        padding: '12px 14px',
        marginBottom: '10px'
      }
    },
      h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
          icon('user'),
          h('span', { style: { fontWeight: '600', fontSize: '13px' } }, conn.account_label || conn.connection_id),
          conn.plan ? h('span', { class: 'badge' }, conn.plan) : null
        ),
        h('span', { class: `badge ${conn.status === 'ok' ? 'ok' : 'err'}` }, conn.status)
      ),
      hasError && conn.error_msg
        ? h('div', { class: 'banner err', style: { padding: '6px 10px', fontSize: '11.5px', marginBottom: '8px' } }, conn.error_msg)
        : null,
      conn.buckets && conn.buckets.length > 0
        ? h('table', { class: 'table', style: { width: '100%', fontSize: '12px', margin: 0 } },
            h('thead', null,
              h('tr', null,
                h('th', { style: { width: '35%' } }, 'Window / Bucket'),
                h('th', { style: { width: '25%' } }, 'Used / Total'),
                h('th', { style: { width: '25%' } }, 'Remaining'),
                h('th', { style: { width: '15%' } }, 'Reset In')
              )
            ),
            h('tbody', null, ...conn.buckets.map(renderBucketRow))
          )
        : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, 'No active quota buckets reported.')
    );
  }

  function render() {
    const activeTrackers = providersData.filter((p) => p.quota_type && p.quota_type !== 'disabled');

    if (!providersData.length || !activeTrackers.length) {
      content.replaceChildren(
        emptyState(
          'data_usage',
          'No Upstream Quota Trackers Configured',
          'Configure a provider with 9router API or Cascaded NineGuard protocol in the Providers settings to track live upstream limits.',
          h('button', {
            class: 'btn btn-primary',
            type: 'button',
            style: { marginTop: '12px' },
            onclick: () => location.hash = '#/providers'
          }, icon('server'), 'Manage Providers')
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
        title: 'Force refresh live quotas from upstream',
        onclick: async () => {
          refreshBtn.disabled = true;
          await load(true);
          toast(`Refreshed quotas for ${p.provider_name}`, 'ok');
        }
      }, icon('refresh'), 'Refresh');

      const protocolLabel = p.quota_type === '9router'
        ? '9router API'
        : p.quota_type === 'nineguard'
        ? 'NineGuard Cascaded'
        : 'Direct LLM (No Tracker)';

      return h('div', { class: 'card', style: { marginBottom: '16px' } },
        h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
            icon('server'),
            h('h2', { style: { margin: 0, fontSize: '15px' } }, p.provider_name),
            h('span', { class: 'badge', style: { background: 'var(--hover)' } }, protocolLabel),
            h('span', { class: `badge ${p.status === 'ok' ? 'ok' : p.status === 'degraded' ? 'warn' : 'muted'}` }, p.status)
          ),
          refreshBtn
        ),
        p.error_msg
          ? h('div', { class: 'banner err', style: { margin: '8px 14px' } }, icon('alert'), ' ', p.error_msg)
          : null,
        h('div', { style: { padding: '12px 14px' } },
          isUnsupported
            ? h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } },
                'This provider connects directly to an LLM without upstream limit telemetry. Switch protocol in Providers menu to enable.'
              )
            : p.connections && p.connections.length > 0
            ? h('div', null, ...p.connections.map(renderConnectionCard))
            : h('p', { class: 'muted', style: { margin: 0, fontSize: '12px' } }, 'No account connections found on upstream.')
        )
      );
    });

    content.replaceChildren(...providerCards);
  }

  load(false);

  return () => {
    alive = false;
  };
}
