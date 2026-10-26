// Endpoints & Agent Setup: Upstream provider configuration, NineGuard API key generation, and IDE guides.
import { api } from '../api.js';
import { h, icon, toast, copy, fmtNum, fmtCompact, fmtAgo, fmtDateTime, tzLabel, debounce, emptyState, formDialog, confirmDialog, searchableSelect, menu, skeletonRows } from '../ui.js';
import { setRoute, getRoute } from '../state.js';
import {
  stateFromParams, paramsFromState, apiQuery, nextSort, withFilter,
  totalPages, rangeLabel, pageItems, pageAfterReload, PAGE_SIZES,
} from '../keylist.js';
import { quotaPercent, formatQuotaUsage, isQuotaExhausted, quotaPeriodLabel, formatTokensCompact } from '../quotahelpers.js';
import { quotaStatus, barTone, shortModel } from '../modelquotas.js';
import { quotaSection, presetButtons } from './keyquota.js';
import { nextResetText } from '../resetschedule.js';
import { openKeyUsageModal } from './keyusage.js';

export function mount(root) {
  let alive = true;
  let activeTab = 'cursor';
  let modelsList = [];
  let keysList = []; // all keys (legacy list) for guides and the endpoint tester
  let keyState = stateFromParams(getRoute().params); // paged key table state (mirrors URL)
  let keyPage = { keys: [], total: 0, page: 1, limit: keyState.limit };
  let keyLoadSeq = 0;
  let modelGroupsList = [];
  let providersList = [];
  let quotaWarn = 80;
  let upstreamInfo = { configured: false, masked_key: '', key: '', router_target: '' };

  const origin = location.origin || `${location.protocol}//${location.host}` || 'http://localhost:8080';
  const baseUrl = `${origin}/v1`;

  const upstreamCard = h('div', { class: 'card mt' });
  const keysCard = h('div', { class: 'card table-card' });
  const guidesWrap = h('div', { class: 'card-body p-0' });
  const testerCard = h('div', { class: 'card mt' });

  const generateBtn = h('button', { class: 'btn btn-primary', type: 'button', onclick: () => openKeyModal(null, generateBtn) }, icon('plus'), 'Generate key');
  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Endpoints & Agent Setup'),
      h('p', null, 'Manage upstream provider connection, generate NineGuard keys for your agents, and configure tools')
    ),
    h('div', { class: 'page-actions' }, generateBtn)
  );

  // ── Copy Helper ──
  function copyText(text, label = 'Copied to clipboard!') {
    return copy(text, label);
  }

  function copyField(label, val) {
    return h('div', { class: 'copy-box', style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', background: 'var(--hover)', borderRadius: '6px', marginBottom: '8px' } },
      h('div', { style: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } },
        h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase', letterSpacing: '0.05em' } }, label),
        h('code', { style: { fontSize: '13px', fontWeight: '600', color: 'var(--accent)' } }, val)
      ),
      h('button', {
        class: 'btn btn-sm',
        type: 'button',
        title: 'Copy to clipboard',
        onclick: () => copyText(val, `${label} copied!`)
      }, icon('copy'), 'Copy')
    );
  }

  // ── Flow Architecture Card ──
  function renderFlowCard() {
    return h('div', { class: 'card mt' },
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Architecture: NineGuard as Master Proxy for Agents'),
          h('p', { class: 'card-sub' }, 'Agents connect to NineGuard using their own keys; NineGuard authorizes calls and forwards to upstream providers')
        )
      ),
      h('div', { class: 'flow-wrap', style: { display: 'flex', flexDirection: 'column', gap: '12px' } },
        h('div', { class: 'flow-steps', style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '12px' } },
          // Step 1: Agent
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' } },
              icon('user'), h('b', null, '1. AI Agent (Client)')
            ),
            h('p', { class: 'muted', style: { fontSize: '12px', margin: 0 } },
              'Cursor, Cline, Continue use Base URL ', h('code', null, ':8080/v1'), ' with NineGuard key (', h('code', null, 'sk-ng-...'), ').'
            )
          ),
          // Step 2: NineGuard
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1.5px solid var(--accent)', background: 'var(--hover)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px', color: 'var(--accent)' } },
              icon('shield'), h('b', null, '2. NineGuard (:8080)')
            ),
            h('p', { style: { fontSize: '12px', margin: 0 } },
              'Validates agent key, enforces model firewall (403), tracks tokens, and injects Upstream Master Key.'
            )
          ),
          // Step 3: Upstream Provider
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' } },
              icon('server'), h('b', null, '3. Upstream Provider')
            ),
            h('p', { class: 'muted', style: { fontSize: '12px', margin: 0 } },
              'Receives request authenticated with Upstream Master Key and handles model provider load balancing.'
            )
          ),
          // Step 4: Upstream LLMs
          h('div', { class: 'flow-box', style: { padding: '14px', borderRadius: '8px', border: '1px solid var(--border)', background: 'var(--panel)' } },
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' } },
              icon('sparkles'), h('b', null, '4. AI Model Providers')
            ),
            h('p', { class: 'muted', style: { fontSize: '12px', margin: 0 } },
              'Claude, OpenAI, Gemini, DeepSeek, or local Ollama LLMs.'
            )
          )
        )
      )
    );
  }

  // ── Upstream Provider Setup Card ──
  function renderUpstreamCard() {
    const isConfigured = upstreamInfo && upstreamInfo.configured && upstreamInfo.key;
    const targetUrl = upstreamInfo?.router_target || '';

    const statusBadge = isConfigured
      ? h('span', { class: 'badge ok', style: { padding: '4px 8px' } }, icon('check'), ' Upstream Key Configured')
      : h('span', { class: 'badge muted', style: { padding: '4px 8px' } }, icon('server'), ' Multi-Provider Routing');

    upstreamCard.replaceChildren(
      h('div', { class: 'card-head', style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } },
        h('div', null,
          h('h2', null, 'Upstream AI Providers'),
          h('p', { class: 'card-sub' }, 'NineGuard relays all authorized agent requests to configured OpenAI-compatible upstream providers')
        ),
        statusBadge
      ),
      h('div', { style: { padding: '14px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '10px', background: 'var(--hover)', borderRadius: '6px' } },
        h('div', null,
          h('span', { class: 'muted', style: { fontSize: '11px', display: 'block', textTransform: 'uppercase' } }, 'Default Upstream Route'),
          targetUrl
            ? h('code', { style: { fontWeight: 'bold' } }, targetUrl)
            : h('span', { class: 'muted', style: { fontStyle: 'italic', fontSize: '12px' } }, 'None configured (Use Providers menu to register upstream endpoints)'),
          isConfigured
            ? h('span', { class: 'muted', style: { marginLeft: '12px', fontSize: '12px' } }, 'Master Key: ', h('code', null, upstreamInfo.masked_key))
            : null
        ),
        h('button', {
          class: 'btn btn-sm btn-primary',
          type: 'button',
          onclick: () => setRoute('providers')
        }, icon('server'), 'Manage Providers')
      )
    );
  }

  // ── NineGuard Client API Keys Card ──
  // ── Paged Key Table (state mirrors the URL hash) ──
  // Writes the table state to the URL; the router calls update(), which loads it.
  function setKeyState(next) {
    setRoute('endpoints', paramsFromState(next));
  }

  // Fetches the page for keyState. If the page became empty (e.g. after a
  // delete) and is not the first, steps back one page via the URL.
  async function reloadKeyPage() {
    const seq = ++keyLoadSeq;
    try {
      const res = await api.get('/keys', apiQuery(keyState));
      if (!alive || seq !== keyLoadSeq) return;
      keyPage = res || { keys: [], total: 0, page: keyState.page, limit: keyState.limit };
      const back = pageAfterReload(keyState.page, (keyPage.keys || []).length);
      if (back !== keyState.page) {
        setKeyState({ ...keyState, page: back });
        return;
      }
    } catch (e) {
      if (!alive || seq !== keyLoadSeq) return;
      keyPage = { keys: [], total: 0, page: keyState.page, limit: keyState.limit, error: e.message };
    }
    renderKeysCard();
  }

  const keySearch = h('input', {
    type: 'search',
    placeholder: 'Search keys...',
    autocomplete: 'off',
    spellcheck: 'false',
    value: keyState.q,
    'aria-label': 'Search keys by name',
    oninput: debounce((e) => setKeyState(withFilter(keyState, { q: e.target.value.trim() })), 300),
    onkeydown: (e) => {
      if (e.key === 'Escape' && keySearch.value) {
        keySearch.value = '';
        setKeyState(withFilter(keyState, { q: '' }));
      }
    },
  });

  function selectEl(options, value, onChange, title) {
    const el = h('select', { class: 'select', title, 'aria-label': title, onchange: (e) => onChange(e.target.value) },
      options.map(([v, l]) => h('option', { value: v }, l)));
    el.value = String(value);
    return el;
  }

  function sortHeader(label, field, extra = {}) {
    const active = keyState.sort === field;
    const arrow = active ? (keyState.order === 'desc' ? ' \u2193' : ' \u2191') : '';
    return h('th', {
      ...extra,
      class: `sortable${active ? ' sorted' : ''}${extra.class ? ' ' + extra.class : ''}`,
      title: `Sort by ${label}`,
      'aria-sort': active ? (keyState.order === 'desc' ? 'descending' : 'ascending') : 'none',
      onclick: () => setKeyState(nextSort(keyState, field)),
    }, label + arrow);
  }

  function lastActiveCell(ts) {
    const ms = ts ? Date.parse(ts) : NaN;
    if (isNaN(ms)) return h('td', { class: 'muted' }, 'Never');
    return h('td', { title: `${fmtDateTime(ms)} (${tzLabel()})` }, fmtAgo(ms));
  }

  function allowedModelsCell(k) {
    const keyMode = k.model_access_mode || (
      (!k.allowed_models || k.allowed_models.length === 0 || k.allowed_models.includes('*') || k.allowed_models.includes('all'))
        ? 'all'
        : 'custom'
    );
    if (keyMode === 'all') {
      return h('td', null,
        h('span', { class: 'badge ok', style: { fontSize: '11px', display: 'inline-flex', alignItems: 'center', gap: '4px' } },
          icon('check'), 'All Models (*)'
        )
      );
    }
    if (keyMode === 'group') {
      const gids = Array.isArray(k.model_group_ids) ? k.model_group_ids : [];
      const groupBadges = gids.map(gid => {
        const grp = modelGroupsList.find(g => g.id === gid);
        const label = grp ? grp.name : gid;
        const modelPreview = grp && grp.models ? grp.models.join(', ') : '';
        return h('span', {
          class: 'badge',
          style: { background: 'var(--accent)', color: '#fff', fontSize: '11px', marginRight: '4px', cursor: 'help', display: 'inline-flex', alignItems: 'center', gap: '4px' },
          title: modelPreview ? `Models in ${label}:\n${grp.models.join('\n')}` : label
        }, icon('sparkles'), label);
      });
      if (groupBadges.length === 0) {
        groupBadges.push(h('span', { class: 'badge err', style: { fontSize: '11px' } }, 'No groups linked'));
      }
      return h('td', null, h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...groupBadges));
    }
    const allowedList = Array.isArray(k.allowed_models) ? k.allowed_models : [];
    const count = allowedList.length;
    const badges = allowedList.slice(0, 2).map(m =>
      h('span', { class: 'badge', style: { background: 'var(--hover)', fontSize: '11px', fontFamily: 'monospace', marginRight: '4px' } }, m)
    );
    if (count > 2) {
      badges.push(h('span', { class: 'badge muted', style: { fontSize: '11px', cursor: 'help' }, title: allowedList.join('\n') }, `+${count - 2} more`));
    }
    return h('td', null, h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '2px', alignItems: 'center' } }, ...badges));
  }

  // Key in Per Model mode: its highest active Model Quota plus how many limits it has.
  function modelQuotaCell(k) {
    const s = k.model_quota_summary;
    if (!s || !s.count) return h('td', { class: 'muted', style: { fontSize: '12px' } }, 'Unlimited');
    if (!s.active) return h('td', { class: 'muted', style: { fontSize: '12px' } }, `Per model \u00b7 ${s.count} inactive`);
    const status = quotaStatus(s.top_percent, quotaWarn);
    const tone = barTone(status);
    return h('td', { style: { verticalAlign: 'middle' }, title: `${s.top_model_id}: ${s.top_percent}% \u00b7 ${s.active} of ${s.count} limits active` },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '4px', fontSize: '11px' } },
        h('span', { class: `qbar ${tone}`, style: { width: '70px' } }, h('i', { style: { width: `${Math.min(100, s.top_percent)}%` } })),
        h('span', { class: `qpct ${tone}` }, `${s.top_percent}%`)),
      h('div', { class: 'muted', style: { fontSize: '11px' } }, `${shortModel(s.top_model_id)} \u00b7 ${s.count} limit${s.count === 1 ? '' : 's'}`));
  }

  // When the Key Quota resets, in the key's own timezone (UTC for the default), from the absolute
  // instant the server returns (ADR 0010). A lifetime quota never resets.
  function resetTooltip(k) {
    if (k.quota_period === 'total') return 'lifetime, never resets';
    if (!k.quota_reset_at) return 'reset time unknown';
    const eff = k.effective_quota_reset_schedule;
    return `resets ${nextResetText(k.quota_reset_at, eff ? eff.timezone : 'UTC')}`;
  }

  function quotaCell(k) {
    if (k.quota_mode === 'model') return modelQuotaCell(k);
    if (!k.quota_limit || k.quota_limit <= 0 || !k.quota_period || k.quota_period === 'none') {
      return h('td', { class: 'muted', style: { fontSize: '12px' } }, 'Unlimited');
    }
    const currentUsage = (typeof k.quota_usage === 'number') ? k.quota_usage : (k.total_tokens || 0);
    const pct = quotaPercent(currentUsage, k.quota_limit);
    const exhausted = isQuotaExhausted(currentUsage, k.quota_limit, k.quota_period);
    const label = `${formatTokensCompact(currentUsage)} / ${formatTokensCompact(k.quota_limit)} ${k.quota_period}`;

    const bar = h('div', {
      style: {
        width: '90px',
        height: '5px',
        background: 'var(--border)',
        borderRadius: '3px',
        overflow: 'hidden',
        marginTop: '3px'
      }
    },
      h('div', {
        style: {
          width: `${pct}%`,
          height: '100%',
          background: exhausted ? 'var(--err, #ef4444)' : 'var(--accent, #6366f1)',
          borderRadius: '3px'
        }
      })
    );

    const tooltip = `${fmtNum(currentUsage)} / ${fmtNum(k.quota_limit)} tokens used in active ${k.quota_period} window (${resetTooltip(k)}) • All-time: ${fmtNum(k.total_tokens || 0)}`;

    return h('td', { style: { verticalAlign: 'middle' }, title: tooltip },
      h('div', null,
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '4px', fontSize: '11px' } },
          h('span', { class: exhausted ? 'badge err' : '' }, label),
          h('span', { class: 'muted' }, `(${pct}%)`)
        ),
        bar
      )
    );
  }

  function keyRow(k) {
    const raw = k.raw_key || k.key;
    const statusBtn = h('button', {
      class: `badge ${k.is_active ? 'ok' : 'muted'} badge-btn`,
      type: 'button',
      onclick: async () => {
        try {
          await api.post(`/keys/${k.id}/toggle`, { active: !k.is_active });
          toast(`Key ${k.is_active ? 'deactivated' : 'activated'}`, 'ok');
          await load();
        } catch (e) {
          toast(`Failed: ${e.message}`, 'error');
        }
      }
    }, k.is_active ? 'Active' : 'Disabled');

    const created = k.created_at ? Date.parse(k.created_at) : NaN;
    const editBtn = h('button', { class: 'btn btn-sm', type: 'button', title: 'Edit key and model access', onclick: () => openKeyModal(k, editBtn) }, icon('pencil'), 'Edit');
    const moreBtn = h('button', {
      class: 'icon-btn sm', type: 'button', title: 'More actions', 'aria-label': `More actions for ${k.name}`, 'aria-haspopup': 'menu',
      onclick: () => menu(moreBtn, [
        { icon: 'activity', text: 'Usage', onClick: () => openKeyUsageModal(k) },
        { icon: 'puzzle', text: 'Plugins', onClick: () => setRoute('plugins') },
        { icon: 'copy', text: 'Copy key', onClick: () => copyText(raw, `Key "${k.name}" copied`) },
        { icon: 'trash', text: 'Delete', onClick: () => confirmDeleteKey(k) },
      ]),
    }, icon('more'));
    return h('tr', null,
      h('td', { class: 'strong' },
        h('span', { class: 'badge', style: { background: 'var(--hover)', marginRight: '8px' } }, icon('key')),
        k.name
      ),
      h('td', null, h('code', { class: 'muted', style: { fontSize: '12px' } }, k.key)),
      allowedModelsCell(k),
      h('td', { class: 'num' }, fmtNum(k.total_requests || 0)),
      h('td', { class: 'num' }, fmtCompact(k.total_tokens || 0)),
      quotaCell(k),
      lastActiveCell(k.last_used_at),
      h('td', null, statusBtn),
      h('td', { class: 'muted', title: isNaN(created) ? '' : `${fmtDateTime(created)} (${tzLabel()})` }, isNaN(created) ? '-' : fmtDateTime(created).slice(0, 10)),
      h('td', { style: { textAlign: 'right' } },
        h('div', { style: { display: 'inline-flex', gap: '6px', alignItems: 'center' } },
          editBtn,
          moreBtn
        )
      )
    );
  }

  function renderPager() {
    const pages = totalPages(keyPage.total, keyState.limit);
    const go = (n) => setKeyState({ ...keyState, page: n });
    const btn = (label, n, disabled, current = false, num = false) => h('button', {
      class: `btn btn-sm${num ? ' page-num' : ''}${current ? ' btn-primary' : ''}`,
      type: 'button',
      disabled,
      'aria-current': current ? 'page' : null,
      onclick: () => go(n),
    }, label);
    return h('div', { class: 'table-foot table-pager' },
      h('span', null, rangeLabel(keyState.page, keyState.limit, keyPage.total, (keyPage.keys || []).length)),
      h('div', { class: 'pager-pages' },
        keyLimitSel,
        btn('Previous', keyState.page - 1, keyState.page <= 1),
        ...pageItems(keyState.page, pages).map((it) => it === '\u2026'
          ? h('span', { class: 'muted pager-gap' }, '\u2026')
          : btn(String(it), it, false, it === keyState.page, true)),
        btn('Next', keyState.page + 1, keyState.page >= pages))
    );
  }

  // Card chrome is built once so the search box keeps focus while typing;
  // renderKeysCard() only refreshes control values and the body.
  const KEY_STATUS_FILTERS = [
    { id: 'all', label: 'All' },
    { id: 'active', label: 'Active' },
    { id: 'disabled', label: 'Disabled' },
  ];
  const keyStatusCounts = new Map();
  const keyStatusButtons = new Map();
  const keyStatusSegmented = h('div', { class: 'segmented', role: 'group', 'aria-label': 'Filter by key status' },
    ...KEY_STATUS_FILTERS.map((s) => {
      const count = h('span', { class: 'seg-count' }, '0');
      keyStatusCounts.set(s.id, count);
      const btn = h('button', {
        type: 'button',
        'aria-pressed': String(s.id === keyState.status),
        onclick: () => setKeyState(withFilter(keyState, { status: s.id })),
      }, s.label, count);
      keyStatusButtons.set(s.id, btn);
      return btn;
    })
  );
  const keyModeSel = selectEl([['any', 'All access modes'], ['all', 'All models'], ['group', 'Model groups'], ['custom', 'Custom list']], keyState.mode,
    (v) => setKeyState(withFilter(keyState, { mode: v })), 'Filter by access mode');
  const keyLimitSel = selectEl(PAGE_SIZES.map((n) => [String(n), `${n} / page`]), keyState.limit,
    (v) => setKeyState(withFilter(keyState, { limit: Number(v) })), 'Page size');
  const keyClearBtn = h('button', {
    class: 'btn btn-sm',
    type: 'button',
    title: 'Reset search, status and access mode filters',
    onclick: () => {
      keySearch.value = '';
      setKeyState(withFilter(keyState, { q: '', status: 'all', mode: 'any' }));
    },
  }, icon('x'), 'Clear');
  const keyMatchInfo = h('span', { class: 'muted', style: { fontSize: '11.5px', whiteSpace: 'nowrap' } });
  const keysBody = h('div', null, skeletonRows(5));
  keysCard.append(
    h('div', { class: 'toolbar card-toolbar' },
      h('label', { class: 'tool-search' }, icon('search'), keySearch),
      keyStatusSegmented, keyModeSel, keyClearBtn,
      h('span', { class: 'spacer' }), keyMatchInfo),
    keysBody
  );

  // Status counts come from the full key list (keysList) narrowed by the same
  // name search and access mode the server applies, so they match the table.
  function keyAccessMode(k) {
    return k.model_access_mode || 'all';
  }

  function renderKeyToolbar() {
    const q = keyState.q.toLowerCase();
    const base = keysList.filter((k) =>
      (!q || String(k.name || '').toLowerCase().includes(q)) &&
      (keyState.mode === 'any' || keyAccessMode(k) === keyState.mode));
    for (const s of KEY_STATUS_FILTERS) {
      const n = s.id === 'all' ? base.length : base.filter((k) => (s.id === 'active') === Boolean(k.is_active)).length;
      keyStatusCounts.get(s.id).textContent = fmtNum(n);
      const btn = keyStatusButtons.get(s.id);
      btn.classList.toggle('active', s.id === keyState.status);
      btn.setAttribute('aria-pressed', String(s.id === keyState.status));
    }
    keyModeSel.classList.toggle('has-value', keyState.mode !== 'any');
    const filtered = Boolean(keyState.q || keyState.status !== 'all' || keyState.mode !== 'any');
    keyClearBtn.hidden = !filtered;
    const total = keyPage.total || 0;
    keyMatchInfo.textContent = filtered
      ? `${fmtNum(total)} of ${fmtNum(keysList.length)} ${keysList.length === 1 ? 'key' : 'keys'}`
      : `${fmtNum(total)} ${total === 1 ? 'key' : 'keys'}`;
  }

  function renderKeysCard() {
    if (document.activeElement !== keySearch) keySearch.value = keyState.q;
    keyModeSel.value = keyState.mode;
    keyLimitSel.value = String(keyState.limit);
    renderKeyToolbar();

    const filtered = keyState.q || keyState.status !== 'all' || keyState.mode !== 'any';
    if (keyPage.error) {
      keysBody.replaceChildren(emptyState('alert', 'Could not load API keys', keyPage.error));
      return;
    }
    if (!keyPage.total) {
      keysBody.replaceChildren(filtered
        ? emptyState('search', 'No keys match these filters', 'Clear the search or filters to see all keys.')
        : emptyState('key', 'No NineGuard API keys generated yet', 'Use "Generate key" to issue your first API key for an agent.'));
      return;
    }

    const table = h('table', { class: 'table' },
      h('thead', null,
        h('tr', null,
          sortHeader('Agent / key name', 'name'),
          h('th', null, 'Key token'),
          h('th', null, 'Allowed models'),
          sortHeader('Requests', 'requests', { class: 'num' }),
          sortHeader('Tokens', 'tokens', { class: 'num' }),
          h('th', null, 'Quota'),
          sortHeader('Last active', 'last_active'),
          sortHeader('Status', 'status'),
          sortHeader('Created', 'created'),
          h('th', { style: { textAlign: 'right' } }, 'Actions')
        )
      ),
      h('tbody', null, ...(keyPage.keys || []).map(keyRow))
    );

    keysBody.replaceChildren(h('div', { class: 'table-wrap' }, table), renderPager());
  }

  async function openKeyModal(existingKey = null, trigger = null) {
    const isEdit = Boolean(existingKey);
    const existingModels = (existingKey && Array.isArray(existingKey.allowed_models)) ? existingKey.allowed_models : [];
    const existingGroupIDs = new Set((existingKey && Array.isArray(existingKey.model_group_ids)) ? existingKey.model_group_ids : []);

    // The prerequisite requests can take a moment; the triggering button shows it is working.
    const triggerContent = trigger ? [...trigger.childNodes] : null;
    if (trigger) {
      if (trigger.disabled) return;
      trigger.disabled = true;
      trigger.replaceChildren('Loading\u2026');
    }
    let pluginsList = [];
    let keyBindings = new Map();
    let globalSchedule = null; // Global Reset Schedule (ADR 0010), null when unset
    try {
      const [pRes, bRes, gRes] = await Promise.all([
        api.get('/plugins').catch(() => ({ plugins: [] })),
        existingKey ? api.get('/plugins/bindings', { scope_type: 'key', scope_id: existingKey.id }).catch(() => ({ bindings: [] })) : Promise.resolve({ bindings: [] }),
        api.get('/settings/quota-reset-schedule').catch(() => ({ quota_reset_schedule: null }))
      ]);
      globalSchedule = (gRes && gRes.quota_reset_schedule) || null;
      pluginsList = pRes.plugins || [];
      (bRes.bindings || []).forEach(b => keyBindings.set(b.plugin_id, b.state));
    } catch { /* ignore */ } finally {
      if (trigger) {
        trigger.disabled = false;
        trigger.replaceChildren(...triggerContent);
      }
    }
    if (!alive) return;

    const pluginSelectMap = new Map();
    const pluginRows = pluginsList.map(p => {
      const curState = keyBindings.get(p.id) || 'inherit';
      const sel = h('select', { class: 'select', 'aria-label': `${p.name} for this key`, style: { width: '180px' } },
        h('option', { value: 'inherit', selected: curState === 'inherit' }, 'Inherit (default)'),
        h('option', { value: 'on', selected: curState === 'on' }, 'On'),
        h('option', { value: 'off', selected: curState === 'off' }, 'Off')
      );
      pluginSelectMap.set(p.id, sel);

      const catBadge = h('span', { class: 'badge', style: { fontSize: '10px' } },
        p.category === 'input_compression' ? 'Compress' : (p.category === 'output_style' ? 'Style' : 'Plugin')
      );

      return h('div', { class: 'key-plugin-row' },
        h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
          h('b', { style: { fontSize: '12px' } }, p.name),
          catBadge
        ),
        sel
      );
    });

    const pluginsWrap = h('div', { class: 'key-panel' },
      pluginRows.length ? pluginRows : h('span', { class: 'muted' }, 'No plugins registered')
    );

    let initialMode = 'all';
    if (existingKey) {
      if (existingKey.model_access_mode) {
        initialMode = existingKey.model_access_mode;
      } else if (existingModels.length > 0 && !existingModels.includes('*') && !existingModels.includes('all')) {
        initialMode = 'custom';
      }
    }

    const nameInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. Cursor on laptop',
      value: existingKey ? existingKey.name : ''
    });

    const quotaLimitInput = h('input', {
      class: 'input',
      type: 'number',
      min: '0',
      step: '1000',
      placeholder: '0 (unlimited)',
      value: (existingKey && existingKey.quota_limit) ? String(existingKey.quota_limit) : '0'
    });

    const quotaPeriodSelect = h('select', { class: 'select wide' },
      h('option', { value: 'none', selected: !existingKey || !existingKey.quota_period || existingKey.quota_period === 'none' }, 'None (unlimited)'),
      h('option', { value: 'daily', selected: existingKey && existingKey.quota_period === 'daily' }, 'Daily'),
      h('option', { value: 'weekly', selected: existingKey && existingKey.quota_period === 'weekly' }, 'Weekly'),
      h('option', { value: 'monthly', selected: existingKey && existingKey.quota_period === 'monthly' }, 'Monthly'),
      h('option', { value: 'total', selected: existingKey && existingKey.quota_period === 'total' }, 'Lifetime (all-time)')
    );

    const velocityBox = h('div', {
      style: {
        display: isEdit ? 'flex' : 'none',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: '6px',
        padding: '6px 8px',
        background: 'var(--panel)',
        borderRadius: '6px',
        fontSize: '11px'
      }
    }, h('span', { class: 'muted' }, 'Loading usage history\u2026'));

    if (isEdit) {
      api.get(`/keys/${existingKey.id}/velocity`).then(vel => {
        if (!vel) return;
        const p24 = formatTokensCompact(vel.tokens_24h || 0);
        const p7d = formatTokensCompact(vel.tokens_7d_avg || 0);
        const p30 = formatTokensCompact(vel.tokens_30d || 0);

        const pill24 = h('span', { class: 'badge', title: `${vel.tokens_24h || 0} tokens in past 24h` }, `24h: ${p24}`);
        const pill7d = h('span', { class: 'badge', title: `${vel.tokens_7d_avg || 0} tokens/day average over past 7d` }, `7d avg: ${p7d}/day`);
        const pill30 = h('span', { class: 'badge', title: `${vel.tokens_30d || 0} tokens in past 30d` }, `30d: ${p30}`);

        velocityBox.replaceChildren(
          h('b', { style: { color: 'var(--text-2)' } }, 'Usage history:'),
          pill24, pill7d, pill30,
          presetButtons(vel.tokens_7d_avg, quotaLimitInput, quotaPeriodSelect, true)
        );
      }).catch(() => {
        velocityBox.style.display = 'none';
      });
    }

    let mode = initialMode;

    // ── Access mode switch (same .seg control as the quota mode) ──
    const segBtn = (value, title, sub) => h('button', {
      type: 'button', class: 'seg-btn', 'aria-pressed': 'false', onclick: () => switchAccessMode(value)
    }, h('span', { class: 'seg-title' }, title), h('span', { class: 'seg-sub' }, sub));
    const btnAll = segBtn('all', 'All models', 'follows global settings');
    const btnGroup = segBtn('group', 'Model groups', 'models from chosen groups');
    const btnCustom = segBtn('custom', 'Custom', 'pick models by hand');

    const modeSelector = h('div', { class: 'seg-wrap' },
      h('div', { class: 'seg-label' }, 'Access mode'),
      h('div', { class: 'seg', role: 'group', 'aria-label': 'Access mode' }, btnAll, btnGroup, btnCustom)
    );

    // ── Panel 1: All Models ──
    const panelAll = h('div', { class: 'key-panel', style: { display: mode === 'all' ? '' : 'none' } },
      h('div', { class: 'key-panel-title' }, icon('check'), 'Inherit global firewall settings'),
      h('p', { class: 'muted' },
        'Grants access to every model enabled in global settings. Models added later become available automatically, without editing this key.'
      )
    );

    // ── Panel 2: Model Groups ──
    const groupCheckboxes = new Map();
    const groupListItems = [];

    const previewContainer = h('div', {
      style: {
        marginTop: '10px',
        padding: '10px 12px',
        background: 'var(--panel)',
        borderRadius: '6px',
        border: '1px solid var(--border)'
      }
    });

    function updateGroupPreview() {
      const selectedGroups = [];
      const unionModels = new Set();
      modelGroupsList.forEach(g => {
        const cb = groupCheckboxes.get(g.id);
        if (cb && cb.checked) {
          selectedGroups.push(g);
          (g.models || []).forEach(m => unionModels.add(m));
        }
      });

      if (selectedGroups.length === 0) {
        previewContainer.replaceChildren(
          h('div', { class: 'muted' },
            'No group selected yet. Select at least one model group above.'
          )
        );
        return;
      }

      const badges = Array.from(unionModels).map(m =>
        h('span', {
          class: 'badge',
          style: { background: 'var(--hover)', fontSize: '11px', fontFamily: 'monospace', margin: '2px' }
        }, m)
      );

      previewContainer.replaceChildren(
        h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' } },
          h('b', { style: { fontSize: '12px' } }, `Allowed models (${unionModels.size})`),
          h('span', { class: 'badge ok', style: { fontSize: '10px' } }, icon('sparkles'), 'Synced')
        ),
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '4px', maxHeight: '100px', overflowY: 'auto' } }, ...badges),
        h('p', { class: 'muted', style: { margin: '8px 0 0' } },
          'Models added to these groups later are allowed for this key automatically.'
        )
      );
    }

    if (modelGroupsList.length === 0) {
      groupListItems.push(
        h('div', { class: 'muted', style: { padding: '12px', textAlign: 'center' } },
          'No model groups yet. Create one under Models > Model groups.'
        )
      );
    } else {
      modelGroupsList.forEach(g => {
        const cb = h('input', {
          type: 'checkbox',
          checked: existingGroupIDs.has(g.id)
        });
        groupCheckboxes.set(g.id, cb);
        cb.onchange = updateGroupPreview;

        const count = Array.isArray(g.models) ? g.models.length : 0;
        const item = h('label', { class: 'key-group-option' },
          cb,
          h('div', { style: { flex: 1, minWidth: 0 } },
            h('div', { style: { fontWeight: '600', display: 'flex', alignItems: 'center', gap: '6px' } },
              icon('sparkles'),
              g.name
            ),
            g.description ? h('div', { class: 'muted', style: { fontSize: '11px', marginTop: '1px' } }, g.description) : null
          ),
          h('span', { class: 'badge', style: { fontSize: '11px', whiteSpace: 'nowrap' } }, `${count} model${count === 1 ? '' : 's'}`)
        );
        groupListItems.push(item);
      });
    }

    const panelGroup = h('div', { class: 'key-panel', style: { display: mode === 'group' ? '' : 'none' } },
      h('div', { class: 'key-panel-title' }, 'Model groups'),
      h('div', { style: { display: 'flex', flexDirection: 'column', gap: '6px', maxHeight: '170px', overflowY: 'auto' } }, ...groupListItems),
      previewContainer
    );
    updateGroupPreview();

    // ── Panel 3: Custom Selection ──
    const filterInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'Filter available models...',
      style: { fontSize: '12px', padding: '6px 10px' }
    });

    const availableModelIDs = new Set();
    modelsList.forEach(m => {
      if (m && m.id) availableModelIDs.add(m.id);
    });

    const initialSelected = new Set();
    const leftoverCustom = [];
    if (initialMode === 'custom') {
      existingModels.forEach(m => {
        if (m === '*' || m === 'all') return;
        if (availableModelIDs.has(m)) {
          initialSelected.add(m);
        } else {
          leftoverCustom.push(m);
        }
      });
    }

    const selectedCount = h('span', { class: 'muted', style: { fontSize: '11.5px', marginLeft: 'auto' } });

    const checkboxes = new Map();
    const listItems = [];

    modelsList.forEach(m => {
      const cb = h('input', {
        type: 'checkbox',
        checked: initialSelected.has(m.id)
      });
      checkboxes.set(m.id, cb);
      cb.onchange = updateCount;

      const item = h('label', { class: 'key-model-option' },
        cb,
        h('span', { style: { flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } }, m.id),
        m.provider_id ? h('span', { class: 'badge', style: { fontSize: '10px' } }, m.provider_id) : null
      );

      listItems.push({ id: m.id.toLowerCase(), node: item });
    });

    function updateCount() {
      let count = 0;
      checkboxes.forEach(cb => { if (cb.checked) count++; });
      selectedCount.textContent = `${count} model${count === 1 ? '' : 's'} selected`;
    }
    updateCount();

    filterInput.oninput = (e) => {
      const q = e.target.value.toLowerCase().trim();
      listItems.forEach(({ id, node }) => {
        node.style.display = (!q || id.includes(q)) ? 'flex' : 'none';
      });
    };

    const selectAllBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      onclick: () => {
        checkboxes.forEach(cb => { cb.checked = true; });
        updateCount();
      }
    }, 'Select all');

    const clearAllBtn = h('button', {
      class: 'btn btn-sm',
      type: 'button',
      onclick: () => {
        checkboxes.forEach(cb => { cb.checked = false; });
        updateCount();
      }
    }, 'Clear selection');

    // Detour helper: Load from Group dropdown
    const detourSelect = searchableSelect({
      placeholder: 'Add models from a group',
      searchPlaceholder: 'Search model group...',
      clearable: false,
      compact: true,
      style: { fontSize: '11.5px', height: '28px', maxWidth: '210px' },
      options: [
        { value: '', label: 'Add models from a group' },
        ...modelGroupsList.map(g => ({
          value: g.id,
          label: `${g.name} (${(g.models || []).length})`,
          badge: `${(g.models || []).length} models`
        }))
      ],
      onChange: (gid) => {
        if (!gid) return;
        const grp = modelGroupsList.find(g => g.id === gid);
        if (grp && Array.isArray(grp.models)) {
          let added = 0;
          grp.models.forEach(m => {
            const cb = checkboxes.get(m);
            if (cb) {
              cb.checked = true;
              added++;
            }
          });
          updateCount();
          toast(`Checked ${added} models from group "${grp.name}"`, 'ok');
        }
        detourSelect.setValue('');
      }
    });

    const actionsRow = h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' } },
      detourSelect,
      selectAllBtn,
      clearAllBtn,
      selectedCount
    );

    const scrollList = h('div', {
      style: {
        maxHeight: '160px',
        overflowY: 'auto',
        border: '1px solid var(--border)',
        borderRadius: '6px',
        padding: '4px',
        background: 'var(--panel)'
      }
    },
      listItems.length > 0
        ? listItems.map(i => i.node)
        : h('div', { class: 'muted', style: { padding: '8px', fontSize: '12px', textAlign: 'center' } }, 'No models synced yet in NineGuard')
    );

    const customInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'e.g. openrouter/*, gpt-4o, claude-3-5-sonnet',
      value: leftoverCustom.join(', '),
      style: { fontSize: '12px' }
    });

    const panelCustom = h('div', { class: 'key-panel', style: { display: mode === 'custom' ? '' : 'none' } },
      actionsRow,
      filterInput,
      scrollList,
      h('label', { class: 'field' },
        h('span', null, 'Extra model IDs or wildcards'),
        customInput,
        h('small', { class: 'muted' }, 'Comma-separated.')
      )
    );

    // What the access panels currently select (also used to check unsaved Model Quota rows).
    function currentAccess() {
      const groupIds = [];
      groupCheckboxes.forEach((cb, gid) => { if (cb.checked) groupIds.push(gid); });
      const allowed = [];
      checkboxes.forEach((cb, id) => { if (cb.checked) allowed.push(id); });
      customInput.value.split(',').map((s) => s.trim()).filter(Boolean).forEach((m) => { if (!allowed.includes(m)) allowed.push(m); });
      return { mode, groupIds, allowed };
    }

    // Key Quota block (limit, period, velocity). The quota section shows it in Key Quota mode.
    const keyQuotaNode = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px' } },
      h('div', { class: 'key-grid-2' },
        h('label', { class: 'field' },
          h('span', null, 'Key quota limit'),
          quotaLimitInput,
          h('small', { class: 'muted' }, 'Max tokens across all models. 0 = unlimited.')
        ),
        h('label', { class: 'field' },
          h('span', null, 'Reset period'),
          quotaPeriodSelect,
          h('small', { class: 'muted' }, 'When it resets is set in Reset schedule below.')
        )
      ),
      velocityBox
    );

    const quotaUI = quotaSection({
      existingKey, isEdit, keyQuotaNode, keyPeriodSelect: quotaPeriodSelect, getAccess: currentAccess,
      groups: modelGroupsList, models: modelsList, providers: providersList, globalSchedule,
    });
    quotaPeriodSelect.addEventListener('change', () => quotaUI.refreshSchedule());

    // Group/model checkboxes and the extra-models input all live in these panels, so one
    // delegated listener re-checks unsaved Model Quota rows whenever the access changes.
    [panelGroup, panelCustom].forEach((panel) => {
      panel.addEventListener('change', () => quotaUI.refreshAccess());
      panel.addEventListener('input', () => quotaUI.refreshAccess());
    });

    function switchAccessMode(newMode) {
      mode = newMode;
      queueMicrotask(() => quotaUI.refreshAccess());
      [[btnAll, 'all'], [btnGroup, 'group'], [btnCustom, 'custom']].forEach(([b, v]) => {
        b.classList.toggle('on', mode === v);
        b.setAttribute('aria-pressed', String(mode === v));
      });

      panelAll.style.display = mode === 'all' ? '' : 'none';
      panelGroup.style.display = mode === 'group' ? '' : 'none';
      panelCustom.style.display = mode === 'custom' ? '' : 'none';
    }
    switchAccessMode(mode);

    const costVisSelect = h('select', { class: 'select wide' },
      h('option', { value: '', selected: !existingKey || !existingKey.cost_visibility }, 'Follow global setting'),
      h('option', { value: 'show', selected: existingKey && existingKey.cost_visibility === 'show' }, 'Always show to key holder'),
      h('option', { value: 'hide', selected: existingKey && existingKey.cost_visibility === 'hide' }, 'Always hide from key holder')
    );

    const section = (title, ...children) => h('section', { class: 'key-section' },
      h('h4', { class: 'key-section-title' }, title), ...children);

    formDialog({
      title: isEdit ? `Edit key: ${existingKey.name}` : 'Generate API key',
      submitText: isEdit ? 'Save changes' : 'Generate key',
      extraWide: true,
      fields: [
        {
          node: section('Name and access',
            h('label', { class: 'field' },
              h('span', null, 'Name'),
              nameInput,
              h('small', { class: 'muted' }, 'Shown in traffic logs and usage reports.')
            ),
            h('div', { class: 'field' },
              h('span', null, 'Allowed models'),
              modeSelector,
              panelAll,
              panelGroup,
              panelCustom,
              h('small', { class: 'muted' }, 'Calls to models outside this list are blocked with HTTP 403.')
            )
          )
        },
        {
          node: section('Quota', quotaUI.node)
        },
        {
          node: section('Plugins and cost',
            h('label', { class: 'field' },
              h('span', null, 'Estimated cost visibility'),
              costVisSelect,
              h('small', { class: 'muted' }, 'Whether this key can see estimated cost and pricing in /usage and /v1/usage.')
            ),
            h('div', { class: 'field' },
              h('span', null, 'Plugin overrides (optional)'),
              pluginsWrap,
              h('small', { class: 'muted' }, 'Overrides for this key take priority over model group and global plugin settings.')
            )
          )
        }
      ],
      onSubmit: async () => {
        const name = nameInput.value.trim() || (isEdit ? existingKey.name : '');
        if (!name) {
          nameInput.focus();
          throw new Error('Enter a name for this key.');
        }
        let model_access_mode = mode;
        let model_group_ids = [];
        let allowed_models = [];

        if (mode === 'group') {
          groupCheckboxes.forEach((cb, gid) => {
            if (cb.checked) model_group_ids.push(gid);
          });
          if (model_group_ids.length === 0) {
            throw new Error('Select at least one model group, or switch to All models.');
          }
        } else if (mode === 'custom') {
          const selected = [];
          checkboxes.forEach((cb, id) => {
            if (cb.checked) selected.push(id);
          });

          const extra = customInput.value.split(',').map(s => s.trim()).filter(Boolean);
          extra.forEach(m => {
            if (!selected.includes(m)) selected.push(m);
          });

          if (selected.length === 0) {
            throw new Error('Select at least one model, or switch to All models.');
          }
          allowed_models = selected;
        }

        const quotaProblem = quotaUI.problem();
        if (quotaProblem) throw new Error(quotaProblem);

        const quota_limit = parseInt(quotaLimitInput.value, 10) || 0;
        const quota_period = quotaPeriodSelect.value;
        const payload = {
          name,
          model_access_mode,
          model_group_ids,
          allowed_models,
          quota_limit,
          quota_period,
          cost_visibility: costVisSelect.value || null,
          ...quotaUI.payload()
        };

        try {
          let targetKeyID = '';
          if (isEdit) {
            await api.put(`/keys/${existingKey.id}`, payload);
            targetKeyID = existingKey.id;
            toast(`Key "${name}" updated!`, 'ok');
            await load();
          } else {
            const newKey = await api.post('/keys', payload);
            targetKeyID = newKey.id;
            toast(`Key "${name}" created!`, 'ok');
            await load();
            showGeneratedKeyModal(newKey);
          }

          if (targetKeyID) {
            for (const [pluginID, sel] of pluginSelectMap.entries()) {
              const nextState = sel.value;
              const prevState = keyBindings.get(pluginID) || 'inherit';
              if (nextState !== prevState) {
                await api.put(`/plugins/${encodeURIComponent(pluginID)}/bindings`, {
                  scope_type: 'key',
                  scope_id: targetKeyID,
                  state: nextState,
                  settings: '{}'
                }).catch(() => {});
              }
            }
          }
        } catch (e) {
          throw new Error(e.message || 'Operation failed');
        }
      }
    });
  }

  function showGeneratedKeyModal(newKey) {
    const raw = newKey.raw_key || newKey.key;
    formDialog({
      title: 'NineGuard API Key Generated',
      submitText: 'Done',
      cancel: false,
      fields: [
        { node: h('div', { class: 'note ok mt-sm' }, icon('check'), h('b', null, 'Your API key is ready:')) },
        { node: h('div', { style: { padding: '14px', background: 'var(--hover)', borderRadius: '6px', margin: '10px 0' } },
          h('div', { class: 'muted', style: { fontSize: '11px', marginBottom: '4px' } }, `Name: ${newKey.name}`),
          h('code', { style: { fontSize: '14px', fontWeight: 'bold', color: 'var(--accent)', wordBreak: 'break-all' } }, raw)
        )},
        { node: h('button', {
          class: 'btn btn-primary btn-block',
          type: 'button',
          onclick: () => copyText(raw, 'API Key copied!')
        }, icon('copy'), 'Copy API Key') }
      ]
    });
  }

  async function confirmDeleteKey(k) {
    const ok = await confirmDialog({
      title: `Delete Key "${k.name}"?`,
      body: `Agents using this key (${k.key}) will immediately receive HTTP 401 Unauthorized. Existing traffic logs will be preserved.`,
      confirmText: 'Delete Key',
      danger: true,
    });
    if (!ok) return;

    try {
      await api.del(`/keys/${k.id}`);
      toast(`Key "${k.name}" deleted`, 'ok');
      await load();
    } catch (e) {
      toast(`Failed to delete key: ${e.message}`, 'error');
    }
  }

  // ── Integration Guides Card ──
  function renderGuides() {
    const activeKeyObj = keysList.find(k => k.is_active) || keysList[0];
    const defaultKey = activeKeyObj ? (activeKeyObj.raw_key || activeKeyObj.key) : 'sk-ng-YOUR_NINEGUARD_KEY';
    const sampleModel = modelsList.length > 0 ? modelsList[0].id : 'gpt-4o';

    const guides = {
      cursor: {
        title: 'Cursor IDE',
        desc: 'Configure custom OpenAI endpoint in Cursor Settings',
        steps: [
          'Open Cursor Settings → Models → OpenAI API Key.',
          'Toggle Override OpenAI Base URL to ON.',
          `Base URL: ${baseUrl}`,
          `API Key: ${defaultKey}`,
          'Add your models (e.g. gpt-4o, claude-3-5-sonnet).'
        ],
        code: `# Cursor IDE Configuration
OpenAI Base URL: ${baseUrl}
API Key: ${defaultKey}
Model: ${sampleModel}`
      },
      cline: {
        title: 'Cline / Roo Code (VS Code)',
        desc: 'Autonomous coding agent extension for VS Code',
        steps: [
          'Click the Cline extension icon in the VS Code sidebar.',
          'Open Settings (gear icon) → Select API Provider: "OpenAI Compatible".',
          `Base URL: ${baseUrl}`,
          `API Key: ${defaultKey}`,
          `Model ID: ${sampleModel}`
        ],
        code: `// Cline Provider Configuration
Provider: OpenAI Compatible
Base URL: ${baseUrl}
API Key: ${defaultKey}
Model ID: ${sampleModel}`
      },
      continue: {
        title: 'Continue.dev',
        desc: 'Open-source AI coding assistant for VS Code and JetBrains',
        steps: [
          'Open your ~/.continue/config.json file.',
          'Add a new model entry under "models" array as shown below.'
        ],
        code: `// ~/.continue/config.json
{
  "models": [
    {
      "title": "NineGuard (${sampleModel})",
      "provider": "openai",
      "model": "${sampleModel}",
      "apiBase": "${baseUrl}",
      "apiKey": "${defaultKey}"
    }
  ]
}`
      },
      pi: {
        title: 'Pi Agent Harness',
        desc: 'High-performance coding agent harness CLI',
        steps: [
          'Configure your environment variable or pass inline when launching.',
          'NineGuard will intercept, filter, and track token usage automatically.'
        ],
        code: `# Set environment variables for Pi Agent
export OPENAI_BASE_URL="${baseUrl}"
export OPENAI_API_KEY="${defaultKey}"

# Or execute Pi directly:
OPENAI_BASE_URL="${baseUrl}" OPENAI_API_KEY="${defaultKey}" pi`
      },
      python: {
        title: 'Python (OpenAI SDK)',
        desc: 'Official OpenAI Python client library',
        steps: [
          'Install package: pip install openai',
          'Instantiate OpenAI client pointing to NineGuard proxy.'
        ],
        code: `from openai import OpenAI

client = OpenAI(
    base_url="${baseUrl}",
    api_key="${defaultKey}"
)

response = client.chat.completions.create(
    model="${sampleModel}",
    messages=[{"role": "user", "content": "Hello from NineGuard!"}]
)
print(response.choices[0].message.content)`
      },
      node: {
        title: 'Node.js (OpenAI SDK)',
        desc: 'Official OpenAI Node.js / TypeScript SDK',
        steps: [
          'Install package: npm install openai',
          'Set baseURL to NineGuard proxy.'
        ],
        code: `import OpenAI from 'openai';

const openai = new OpenAI({
  baseURL: '${baseUrl}',
  apiKey: '${defaultKey}',
});

const response = await openai.chat.completions.create({
  model: '${sampleModel}',
  messages: [{ role: 'user', content: 'Hello from NineGuard!' }],
});
console.log(response.choices[0].message.content);`
      },
      curl: {
        title: 'cURL / Shell',
        desc: 'Direct HTTP terminal request test',
        steps: [
          'Run the command in your terminal to test chat completions.'
        ],
        code: `curl -X POST "${baseUrl}/chat/completions" \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${defaultKey}" \\
  -d '{
    "model": "${sampleModel}",
    "messages": [{"role": "user", "content": "Ping NineGuard"}]
  }'`
      }
    };

    const tabKeys = Object.keys(guides);
    const tabBtns = tabKeys.map(k => h('button', {
      class: `tab-btn${activeTab === k ? ' active' : ''}`,
      type: 'button',
      onclick: () => {
        activeTab = k;
        renderGuides();
      }
    }, guides[k].title));

    const g = guides[activeTab] || guides.cursor;

    guidesWrap.replaceChildren(
      h('div', { class: 'tab-nav' }, ...tabBtns),
      h('div', { style: { paddingTop: '14px' } },
        h('h3', { style: { margin: '0 0 6px 0' } }, g.title),
        h('p', { class: 'muted', style: { fontSize: '13px', marginBottom: '14px' } }, g.desc),
        h('ol', { style: { paddingLeft: '18px', margin: '0 0 16px 0', fontSize: '13px', lineHeight: '1.6' } },
          ...g.steps.map(s => h('li', null, s))
        ),
        h('div', { style: { position: 'relative' } },
          h('pre', { class: 'code-block', style: { padding: '14px', borderRadius: '6px', background: 'var(--hover)', overflowX: 'auto', fontSize: '12px' } }, g.code),
          h('button', {
            class: 'btn btn-sm',
            style: { position: 'absolute', top: '8px', right: '8px' },
            type: 'button',
            onclick: () => copyText(g.code, `${g.title} snippet copied!`)
          }, icon('copy'), 'Copy Snippet')
        )
      )
    );
  }

  // ── Live Endpoint Tester ──
  function renderTester() {
    const enabledModels = modelsList.filter(m => m.enabled);
    const activeKeys = keysList.filter(k => k.is_active);

    const modelSelect = searchableSelect({
      placeholder: enabledModels.length ? 'Select model...' : 'No models available',
      searchPlaceholder: 'Search models...',
      ariaLabel: 'Target Model',
      wide: true,
      compact: true,
      style: { flex: '1', minWidth: '170px' },
      options: enabledModels.map(m => {
        const prov = m.provider_id || (m.id.includes('/') ? m.id.split('/')[0] : '');
        return {
          value: m.id,
          label: m.id,
          badge: prov || ''
        };
      }),
      value: enabledModels[0]?.id || ''
    });

    const keySelect = searchableSelect({
      placeholder: activeKeys.length ? 'Select API key...' : 'No keys available',
      searchPlaceholder: 'Search keys...',
      ariaLabel: 'NineGuard API Key',
      wide: true,
      compact: true,
      style: { flex: '1', minWidth: '170px' },
      options: activeKeys.map(k => ({
        value: k.raw_key || k.key,
        label: `${k.name} (${k.key})`,
        badge: k.is_active ? 'active' : ''
      })),
      value: activeKeys[0] ? (activeKeys[0].raw_key || activeKeys[0].key) : ''
    });

    const promptInput = h('input', {
      class: 'input',
      type: 'text',
      value: 'Hi NineGuard! Give me a 1-sentence response.',
      style: { flex: '2' }
    });

    const testBtn = h('button', { class: 'btn btn-primary', type: 'button' }, icon('sparkles'), 'Test Ping');
    const resultBox = h('div', { style: { display: 'none', marginTop: '14px', padding: '12px', background: 'var(--hover)', borderRadius: '6px' } });

    testBtn.onclick = async () => {
      const model = modelSelect.value;
      const key = keySelect.value;
      const prompt = promptInput.value.trim();

      if (!model) return toast('No model selected', 'warn');
      if (!key) return toast('No NineGuard API key selected', 'warn');

      testBtn.disabled = true;
      testBtn.textContent = 'Sending...';
      resultBox.style.display = 'block';
      resultBox.replaceChildren(h('span', { class: 'muted' }, 'Connecting to proxy /v1/chat/completions...'));

      const startTime = performance.now();
      try {
        const resp = await fetch('/v1/chat/completions', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${key}`,
          },
          body: JSON.stringify({
            model: model,
            messages: [{ role: 'user', content: prompt || 'ping' }],
            stream: false,
          })
        });

        const duration = Math.round(performance.now() - startTime);
        const data = await resp.json().catch(() => null);

        if (resp.ok) {
          const content = data?.choices?.[0]?.message?.content || '(Empty content)';
          const totalTok = data?.usage?.total_tokens || 0;

          resultBox.replaceChildren(
            h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
              h('span', { class: 'badge ok' }, `HTTP ${resp.status} OK`),
              h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms · Tokens: ${totalTok}`)
            ),
            h('div', { style: { fontSize: '13px', whiteSpace: 'pre-wrap', lineHeight: '1.5' } }, content)
          );
          toast('Proxy test succeeded!', 'ok');
        } else {
          const errText = data?.error?.message || `HTTP ${resp.status} ${resp.statusText}`;
          resultBox.replaceChildren(
            h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
              h('span', { class: 'badge err' }, `HTTP ${resp.status} Failed`),
              h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms`)
            ),
            h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, errText)
          );
          toast(`Test failed: ${errText}`, 'error');
        }
      } catch (err) {
        const duration = Math.round(performance.now() - startTime);
        resultBox.replaceChildren(
          h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, `Network error: ${err.message} (${duration}ms)`)
        );
        toast(`Network error: ${err.message}`, 'error');
      } finally {
        testBtn.disabled = false;
        testBtn.replaceChildren(icon('sparkles'), 'Test Ping');
      }
    };

    testerCard.replaceChildren(
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Live Endpoint Probe (Connection Test)'),
          h('p', { class: 'card-sub' }, 'Test the full pipeline Agent → NineGuard → Upstream Provider directly from your browser')
        )
      ),
      h('div', null,
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' } },
          modelSelect,
          keySelect,
          promptInput,
          testBtn
        ),
        resultBox
      )
    );
  }

  // ── Endpoints URL Summary Box ──
  function renderUrlCard() {
    return h('div', { class: 'card mt' },
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'OpenAI-Compatible URLs'),
          h('p', { class: 'card-sub' }, 'Configure these endpoints in your developer tools and coding agents')
        )
      ),
      h('div', null,
        copyField('OpenAI Base URL (Recommended for agents)', baseUrl),
        copyField('Chat Completions Endpoint', `${baseUrl}/chat/completions`),
        copyField('Models List Endpoint', `${baseUrl}/models`),
        copyField('Health Probe Endpoint', `${origin}/healthz`)
      )
    );
  }

  // ── Main Load ──
  async function load() {
    try {
      const [u, m, k, g, pv, ts] = await Promise.all([
        api.get('/settings/upstream').catch(() => ({})),
        api.get('/models').catch(() => []),
        api.get('/keys').catch(() => ({ keys: [] })),
        api.get('/model-groups').catch(() => ({ groups: [] })),
        api.get('/providers').catch(() => ({ providers: [] })),
        api.get('/settings/traffic').catch(() => ({}))
      ]);
      providersList = (pv && Array.isArray(pv.providers)) ? pv.providers : [];
      quotaWarn = (ts && ts.quota_warn_percent) || 80;
      upstreamInfo = u || {};
      modelsList = Array.isArray(m) ? m : [];
      keysList = (k && k.keys) ? k.keys : [];
      modelGroupsList = (g && Array.isArray(g.groups)) ? g.groups : [];
    } catch {
      // ignore
    }

    if (!alive) return;

    renderUpstreamCard();
    await reloadKeyPage();
    renderGuides();
    renderTester();
  }

  const guidesCard = h('div', { class: 'card mt' },
    h('div', { class: 'card-head' },
      h('div', null,
        h('h2', null, 'Agent & Client Configuration'),
        h('p', { class: 'card-sub' }, 'Select your tool to view setup instructions and copy configuration snippets with your NineGuard key')
      )
    ),
    guidesWrap
  );

  root.append(h('div', { class: 'page' },
    header,
    keysCard,
    renderUrlCard(),
    upstreamCard,
    renderFlowCard(),
    guidesCard,
    testerCard
  ));

  // #/endpoints?edit=<id> (from the Quotas page): open that key's edit modal, then drop the param.
  let editHandled = '';
  async function openEditFromRoute(params) {
    const id = params && params.edit;
    if (!id || id === editHandled) return;
    editHandled = id;
    try {
      const key = await api.get('/keys/' + encodeURIComponent(id));
      if (alive) openKeyModal(key);
    } catch (e) {
      toast(`Could not open key: ${e.message}`, 'error');
    }
    const { edit, ...rest } = getRoute().params;
    setRoute('endpoints', rest, { replace: true });
    editHandled = '';
  }

  load().then(() => openEditFromRoute(getRoute().params));

  return {
    // Router calls update() on hash changes (sort, page, filters, back/forward).
    update(params) {
      if (params.edit) { openEditFromRoute(params); return; }
      const next = stateFromParams(params);
      if (JSON.stringify(next) === JSON.stringify(keyState)) return;
      keyState = next;
      reloadKeyPage();
    },
    refresh: load,
    destroy() { alive = false; }
  };
}
