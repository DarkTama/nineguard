// Quotas: every API key with its Key Quota or Model Quotas, usage and reset (ADR 0007).
import { api } from '../api.js';
import { h, icon, emptyState, fmtNum, debounce, skeletonRows } from '../ui.js';
import { patchRoute, getRoute } from '../state.js';
import { formatTokensCompact } from '../quotahelpers.js';
import { barTone, formatResetIn, filterQuotaKeys, quotaFiltersFromParams, aggregateModelQuotas } from '../modelquotas.js';
import { renderUpstreamQuotas } from './upstream_quotas.js';
import { openGlobalScheduleDialog } from './globalschedule.js';
import { nextResetText } from '../resetschedule.js';

const PERIOD_LABEL = { daily: 'daily', weekly: 'weekly', monthly: 'monthly', total: 'lifetime' };
const REASON_LABEL = { removed: 'removed', disabled: 'disabled', no_access: 'no access' };

export function mount(root, initialParams) {
  let alive = true;
  let data = null;
  let error = '';
  const initParams = initialParams || (typeof getRoute === 'function' ? getRoute()?.params : {}) || {};
  let filters = quotaFiltersFromParams(initParams);
  const toggled = new Map(); // key id -> expanded (user choice beats the default)

  const cards = h('div', { class: 'grid grid-4 quota-summary' });
  const search = h('input', {
    type: 'search', placeholder: 'Search key or model...', autocomplete: 'off', spellcheck: 'false', 'aria-label': 'Search keys or models',
    onkeydown: (e) => { if (e.key === 'Escape' && search.value) { search.value = ''; patchRoute({ q: '' }, { replace: true }); } },
  });
  const searchBox = h('label', { class: 'tool-search' }, icon('search'), search);
  const modeSel = h('select', { class: 'select', 'aria-label': 'Filter by quota mode' },
    [['all', 'All modes'], ['key', 'Key quota'], ['model', 'Per model']].map(([v, l]) => h('option', { value: v }, l)));
  const STATUS_FILTERS = [
    { id: 'all', label: 'All' },
    { id: 'near', label: 'Near limit' },
    { id: 'exhausted', label: 'Exhausted' },
    { id: 'inactive', label: 'Inactive' },
  ];
  const statusCounts = new Map();
  const statusButtons = new Map();
  const statusSegmented = h('div', { class: 'segmented', role: 'group', 'aria-label': 'Filter by quota status' },
    ...STATUS_FILTERS.map((s) => {
      const count = h('span', { class: 'seg-count' }, '0');
      statusCounts.set(s.id, count);
      const btn = h('button', {
        type: 'button', 'aria-pressed': 'false',
        onclick: () => patchRoute({ status: s.id === 'all' ? '' : s.id }),
      }, s.label, count);
      statusButtons.set(s.id, btn);
      return btn;
    })
  );
  const unlimited = h('input', { type: 'checkbox' });
  const clearFiltersBtn = h('button', {
    class: 'btn btn-sm', type: 'button', title: 'Reset search, mode, status and unlimited filters', hidden: true,
    onclick: () => { search.value = ''; patchRoute({ q: '', mode: '', status: '', unlimited: '' }); },
  }, icon('x'), 'Clear');
  const matchInfo = h('span', { class: 'muted', style: { fontSize: '11.5px', whiteSpace: 'nowrap' } });
  const toolbar = h('div', { class: 'toolbar card-toolbar' },
    searchBox, modeSel, statusSegmented,
    h('label', { class: 'muted quota-unlimited-toggle' }, unlimited, 'Show unlimited'),
    clearFiltersBtn, h('span', { class: 'spacer' }), matchInfo);
  // The toolbar stays put while only tableBody re-renders, so the search box keeps focus.
  const tableBody = h('div');
  const body = h('div', { class: 'card table-card mt' }, toolbar, tableBody);

  search.addEventListener('input', debounce(() => patchRoute({ q: search.value }, { replace: true }), 250));
  modeSel.addEventListener('change', () => patchRoute({ mode: modeSel.value === 'all' ? '' : modeSel.value }));
  unlimited.addEventListener('change', () => patchRoute({ unlimited: unlimited.checked ? '1' : '' }));

  let activeTab = 'client';
  let upstreamCleanup = null;
  const clientTabBtn = h('button', {
    class: 'tab active',
    type: 'button',
    role: 'tab',
    onclick: () => { patchRoute({ tab: '' }); if (activeTab !== 'client') switchTab('client'); },
  }, icon('users'), 'Client quotas');
  const upstreamTabBtn = h('button', {
    class: 'tab',
    type: 'button',
    role: 'tab',
    onclick: () => { patchRoute({ tab: 'upstream' }); if (activeTab !== 'upstream') switchTab('upstream'); },
  }, icon('server'), 'Upstream quotas');
  const tabSwitcher = h('div', { class: 'tabs', role: 'tablist' }, clientTabBtn, upstreamTabBtn);

  const clientContainer = h('div', null, cards, body);
  const upstreamContainer = h('div', { style: { display: 'none' } });
  // Global Reset Schedule (ADR 0010): applies to client key quotas only, so it hides with the Upstream tab.
  const scheduleBtn = h('button', {
    class: 'btn btn-sm', type: 'button', title: 'When client key quotas reset (all keys without their own schedule)',
    onclick: () => openGlobalScheduleDialog({ onChanged: () => load() }),
  }, icon('clock'), 'Reset schedule');

  function stopUpstream() {
    if (upstreamCleanup) upstreamCleanup();
    upstreamCleanup = null;
  }

  function switchTab(tab) {
    activeTab = tab;
    const up = tab === 'upstream';
    clientTabBtn.classList.toggle('active', !up);
    upstreamTabBtn.classList.toggle('active', up);
    clientTabBtn.setAttribute('aria-selected', String(!up));
    upstreamTabBtn.setAttribute('aria-selected', String(up));
    clientContainer.style.display = up ? 'none' : 'block';
    scheduleBtn.style.display = up ? 'none' : '';
    upstreamContainer.style.display = up ? 'block' : 'none';
    stopUpstream();
    if (up) upstreamCleanup = renderUpstreamQuotas(upstreamContainer);
  }

  root.append(h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', null,
        h('h1', null, 'Quotas & limits'),
        h('p', null, 'Token limits per client key/model and live rolling capacity from upstream providers.')
      ),
      h('div', { class: 'page-actions' }, scheduleBtn)
    ),
    tabSwitcher,
    clientContainer,
    upstreamContainer
  ));

  function card(label, value, tone) {
    return h('div', { class: 'card' }, h('small', { class: 'muted' }, label),
      h('div', { style: { fontSize: '22px', fontWeight: '700', color: tone ? `var(--${tone})` : '' } }, fmtNum(value)));
  }

  function bar(status, percent) {
    return h('span', null, h('span', { class: `qbar ${barTone(status)}` }, h('i', { style: { width: `${Math.min(100, percent)}%` } })),
      h('span', { class: `qpct ${barTone(status)}` }, `${percent}%`));
  }

  // tz: the timezone of the key's Effective Reset Schedule (UTC for the default). The relative
  // text stays; the tooltip shows the exact reset in that zone.
  function resetCell(r, tz = 'UTC') {
    if (!r.reset_at) return h('td', { class: 'muted' }, 'never');
    return h('td', { class: 'muted', title: nextResetText(r.reset_at, tz) }, `in ${formatResetIn(r.reset_at)}`);
  }

  function modelRow(r, tz) {
    const inactive = r.inactive;
    return h('tr', { class: inactive ? 'qrow-inactive' : '' },
      h('td', { style: { paddingLeft: '30px' } }, h('code', { style: { fontSize: '12px' } }, r.model_id), ' ',
        inactive ? h('span', { class: 'badge removed' }, REASON_LABEL[r.inactive_reason] || 'inactive')
          : r.status === 'exhausted' ? h('span', { class: 'badge err' }, 'exhausted') : null),
      h('td'),
      h('td', null, inactive ? '' : `${formatTokensCompact(r.usage)} / ${formatTokensCompact(r.quota_limit)}`),
      h('td', null, inactive ? h('span', { class: 'muted' }, 'not enforced') : bar(r.status, r.percent)),
      h('td', { class: 'muted' }, PERIOD_LABEL[r.quota_period] || r.quota_period),
      inactive ? h('td') : resetCell(r, tz),
      h('td'));
  }

  const keyTz = (k) => (k.effective_quota_reset_schedule ? k.effective_quota_reset_schedule.timezone : 'UTC');

  function editKeyBtn(k) {
    const stop = (e) => e.stopPropagation();
    return h('button', {
      class: 'icon-btn sm', type: 'button', title: 'Edit key', 'aria-label': `Edit key ${k.name}`,
      onclick: (e) => { e.stopPropagation(); location.hash = `#/endpoints?edit=${encodeURIComponent(k.id)}`; },
      onkeydown: stop,
    }, icon('pencil'));
  }

  function toggleKey(id, open) {
    const hadFocus = body.contains(document.activeElement);
    toggled.set(id, !open);
    render();
    const row = hadFocus && body.querySelector(`tr[data-qkey="${CSS.escape(String(id))}"]`);
    if (row) row.focus({ preventScroll: true });
  }

  function keyRows(k) {
    const open = toggled.has(k.id) ? toggled.get(k.id) : (k.status === 'near' || k.status === 'exhausted');
    const name = h('span', { class: 'strong' }, k.name);
    const disabled = k.is_active ? null : h('span', { class: 'badge', style: { marginLeft: '6px' } }, 'disabled');
    const actions = h('td', { class: 'qrow-actions' }, editKeyBtn(k));

    if (k.quota_mode === 'model') {
      const rows = k.model_quotas || [];
      const agg = aggregateModelQuotas(rows, data.warn_percent);
      const inactiveCount = rows.length - agg.active;
      const countText = `· ${agg.active} active` + (inactiveCount ? `, ${inactiveCount} off` : '');
      // The head is the key's combined allowance; per-model bars live in the breakdown. A model
      // that crossed its own line is named here, since the combined bar can look healthy.
      const flag = agg.exhausted
        ? h('span', { class: 'badge err', style: { marginLeft: '6px' } }, `${agg.exhausted} exhausted`)
        : agg.near ? h('span', { class: 'badge warn', style: { marginLeft: '6px' } }, `${agg.near} near limit`) : null;
      const label = open ? 'Hide per-model limits' : 'Show per-model limits';
      // The chevron's click bubbles to the row, which owns the toggle.
      const chevron = h('button', { class: 'icon-btn sm qrow-chevron', type: 'button', 'aria-expanded': String(open), 'aria-label': label, title: label },
        icon(open ? 'chevron-down' : 'chevron-right'));
      const head = h('tr', {
        class: 'qrow-head qrow-toggle', tabindex: '0', 'aria-expanded': String(open), 'data-qkey': String(k.id),
        onclick: () => toggleKey(k.id, open),
        onkeydown: (e) => {
          if (e.target !== e.currentTarget || (e.key !== 'Enter' && e.key !== ' ')) return;
          e.preventDefault();
          toggleKey(k.id, open);
        },
      },
        h('td', null, chevron, name, disabled, h('span', { class: 'muted', style: { marginLeft: '6px' } }, countText), flag),
        h('td', null, h('span', { class: 'badge' }, 'per model')),
        agg.active
          ? h('td', { title: 'Sum of the active per-model limits' }, `${formatTokensCompact(agg.used)} / ${formatTokensCompact(agg.limit)}`)
          : h('td', { class: 'muted' }, 'nothing enforced'),
        h('td', null, agg.active ? bar(agg.status, agg.percent) : ''),
        h('td', { class: 'muted' }, agg.period === 'mixed' ? 'mixed' : (PERIOD_LABEL[agg.period] || agg.period)),
        agg.resetAt ? resetCell({ reset_at: agg.resetAt }, keyTz(k)) : h('td'),
        actions);
      return [head, ...(open ? rows.map((r) => modelRow(r, keyTz(k))) : [])];
    }
    const q = k.key_quota;
    return [h('tr', null,
      h('td', null, name, disabled),
      h('td', null, h('span', { class: 'badge' }, 'key')),
      h('td', null, q ? `${formatTokensCompact(q.usage)} / ${formatTokensCompact(q.quota_limit)}` : h('span', { class: 'muted' }, 'unlimited')),
      h('td', null, q ? bar(q.status, q.percent) : ''),
      h('td', { class: 'muted' }, q ? (PERIOD_LABEL[q.quota_period] || q.quota_period) : ''),
      q ? resetCell(q, keyTz(k)) : h('td'),
      actions)];
  }

  function render() {
    if (!alive) return;
    if (error) { tableBody.replaceChildren(emptyState('alert', 'Could not load quotas', error)); cards.replaceChildren(); return; }
    if (!data) return;
    const s = data.summary;
    cards.replaceChildren(card('Keys with quotas', s.keys_with_quota), card('Model quotas', s.model_quotas),
      card(`Near limit (>=${data.warn_percent}%)`, s.near_limit, s.near_limit ? 'lvt-warn' : ''), card('Exhausted', s.exhausted, s.exhausted ? 'danger' : ''));

    // Counts honour every filter except status, so each segment shows what picking it would list.
    const total = filterQuotaKeys(data.keys, { ...filters, q: '', mode: 'all', status: 'all' }).length;
    for (const st of STATUS_FILTERS) {
      statusCounts.get(st.id).textContent = fmtNum(filterQuotaKeys(data.keys, { ...filters, status: st.id }).length);
    }
    const list = filterQuotaKeys(data.keys, filters);
    const isFiltered = !!(filters.q || filters.mode !== 'all' || filters.status !== 'all' || filters.unlimited === '1');
    clearFiltersBtn.hidden = !isFiltered;
    matchInfo.textContent = filters.q || filters.mode !== 'all' || filters.status !== 'all'
      ? `${fmtNum(list.length)} of ${fmtNum(total)} ${total === 1 ? 'key' : 'keys'}`
      : `${fmtNum(total)} ${total === 1 ? 'key' : 'keys'}`;
    if (!list.length) {
      tableBody.replaceChildren(emptyState('search', 'No keys match', filters.status !== 'all' || filters.q || filters.mode !== 'all'
        ? 'Clear the search or filters.' : 'No API key has a quota yet. Set one in Endpoints & keys.'));
      return;
    }
    tableBody.replaceChildren(h('div', { class: 'table-wrap' }, h('table', { class: 'table quota-table' },
      h('thead', null, h('tr', null, ['API key / model', 'Mode', 'Used / limit', 'Usage', 'Period', 'Resets', ''].map((t) => h('th', null, t)))),
      h('tbody', null, list.flatMap(keyRows)))));
  }

  function renderLoading() {
    cards.replaceChildren(...Array.from({ length: 4 }, () => h('div', { class: 'card' }, h('div', { class: 'skel', style: { height: '44px' } }))));
    tableBody.replaceChildren(...skeletonRows(6));
  }

  async function load() {
    try { data = await api.get('/quotas'); error = ''; } catch (e) { data = null; error = e.message; }
    render();
  }

  function applyFilters() {
    if (document.activeElement !== search) search.value = filters.q;
    modeSel.value = filters.mode;
    for (const st of STATUS_FILTERS) {
      const btn = statusButtons.get(st.id);
      btn.classList.toggle('active', st.id === filters.status);
      btn.setAttribute('aria-pressed', String(st.id === filters.status));
    }
    unlimited.checked = filters.unlimited === '1';
  }

  renderLoading();
  load();
  applyFilters();
  if (initParams.tab === 'upstream') {
    switchTab('upstream');
  }
  return {
    update(params) {
      if (params?.tab === 'upstream') {
        if (activeTab !== 'upstream') switchTab('upstream');
      } else {
        if (activeTab !== 'client') switchTab('client');
      }
      const next = quotaFiltersFromParams(params);
      if (JSON.stringify(next) === JSON.stringify(filters) && data) return;
      filters = next;
      applyFilters();
      render();
    },
    refresh: load,
    destroy() { alive = false; stopUpstream(); },
  };
}
