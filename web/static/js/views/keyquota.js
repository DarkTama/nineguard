// Quota section of the API key modal: Key Quota (total) or Model Quotas (per Model), ADR 0007.
// endpoints.js passes in its existing Key Quota inputs as keyQuotaNode and reads the result
// through payload() / problem(). Rows are kept in memory and rendered from there.
import { api } from '../api.js';
import { h, icon } from '../ui.js';
import { formatTokensCompact } from '../quotahelpers.js';
import {
  MODEL_QUOTA_PERIODS, barTone, shortModel, isModelAllowed, pickableModels, pickerSections, validateModelQuotaRows,
} from '../modelquotas.js';

const REASON_LABEL = { removed: 'removed', disabled: 'disabled', no_access: 'no access' };

// ctx: { existingKey, isEdit, keyQuotaNode, getAccess(), groups, models, providers }
export function quotaSection(ctx) {
  const { existingKey, isEdit, keyQuotaNode, getAccess, groups = [], models = [], providers = [] } = ctx;
  let mode = existingKey && existingKey.quota_mode === 'model' ? 'model' : 'key';
  let loaded = !isEdit; // until the stored rows arrive, payload() omits model_quotas (= leave unchanged)
  const rows = [];

  const lc = (s) => String(s).toLowerCase();
  const allowedNow = (id) => isModelAllowed(getAccess(), groups, id);

  function makeRow(src, stored) {
    const limit = h('input', { class: 'input', type: 'number', min: '1', step: '1', placeholder: 'tokens', style: { width: '130px' } });
    limit.value = src.quota_limit ? String(src.quota_limit) : '';
    const period = h('select', { class: 'input qk-period', style: { width: '170px' } },
      MODEL_QUOTA_PERIODS.map(([v, l]) => h('option', { value: v, selected: v === (src.quota_period || 'daily') }, l)));
    const row = {
      modelId: src.model_id, stored, limit, period,
      usage: src.usage || 0, percent: src.percent || 0, status: src.status || 'ok', resetAt: src.reset_at || null,
      inactive: Boolean(src.inactive), reason: src.inactive_reason || '',
      notAllowed: false, velocity: h('div', { class: 'qrow-sub' }),
    };
    if (isEdit && stored) {
      let requested = false;
      limit.addEventListener('focus', () => {
        if (requested) return;
        requested = true;
        row.velocity.textContent = 'Loading usage history...';
        api.get(`/keys/${existingKey.id}/model-velocity`, { model: row.modelId }).then((v) => {
          const avg = v.tokens_7d_avg || 0;
          // Native replaceChildren prints null as text, so drop the absent buttons.
          row.velocity.replaceChildren(...[
            h('span', { class: 'muted' }, 'History (approx.): '),
            h('span', { class: 'badge' }, `24h: ${formatTokensCompact(v.tokens_24h || 0)}`), ' ',
            h('span', { class: 'badge' }, `7d avg: ${formatTokensCompact(avg)}/day`), ' ',
            h('span', { class: 'badge' }, `30d: ${formatTokensCompact(v.tokens_30d || 0)}`), ' ',
            avg > 0 ? h('button', { class: 'btn btn-sm', type: 'button', onclick: () => { limit.value = String(Math.round(avg * 1.5)); period.value = 'daily'; } }, 'Set 1.5x Daily') : null, ' ',
            avg > 0 ? h('button', { class: 'btn btn-sm', type: 'button', onclick: () => { limit.value = String(avg * 2); period.value = 'daily'; } }, 'Set 2x Daily') : null,
          ].filter(Boolean));
        }).catch(() => { row.velocity.textContent = ''; });
      });
    }
    return row;
  }

  function addRow(src, stored) {
    if (rows.some((r) => lc(r.modelId) === lc(src.model_id))) return;
    rows.push(makeRow(src, stored));
  }

  (existingKey && existingKey.model_quotas || []).forEach((q) => addRow(q, true));

  // The list response has no rows, so edit mode loads the full key once.
  if (isEdit) {
    api.get(`/keys/${existingKey.id}`).then((k) => {
      (k.model_quotas || []).forEach((q) => addRow(q, true));
      loaded = true;
      render();
    }).catch(() => { loaded = false; render(); });
  }

  // ── Mode switch ──
  const btnKey = h('button', { type: 'button', class: 'seg-btn', 'aria-pressed': 'false', onclick: () => setMode('key') }, h('span', { class: 'seg-title' }, 'Key Quota'), h('span', { class: 'seg-sub' }, 'one limit, all models'));
  const btnModel = h('button', { type: 'button', class: 'seg-btn', 'aria-pressed': 'false', onclick: () => setMode('model') }, h('span', { class: 'seg-title' }, 'Per Model'), h('span', { class: 'seg-sub' }, 'a limit for each model'));
  const modeBar = h('div', { class: 'seg-wrap' },
    h('div', { class: 'seg-label' }, 'Quota mode ', h('span', { class: 'muted' }, '(only one is enforced)')),
    h('div', { class: 'seg', role: 'group', 'aria-label': 'Quota mode' }, btnKey, btnModel));
  const savedHint = h('div', { class: 'muted', style: { fontSize: '11.5px' } });
  const keyBox = h('div', null, keyQuotaNode, savedHint);

  // ── Model rows ──
  const tbody = h('tbody');
  const table = h('table', { class: 'table qk-table' },
    h('thead', null, h('tr', null, h('th', null, 'Model'), h('th', null, 'Token limit'), h('th', null, 'Reset period'), h('th', null, 'Used (current window)'), h('th'))),
    tbody);
  const empty = h('p', { class: 'muted', style: { fontSize: '12px', margin: '6px 0' } }, 'No Model Quotas. All models are unlimited for this key.');
  const loadErr = h('p', { class: 'muted', style: { fontSize: '12px', margin: '6px 0' } }, 'Could not load the saved Model Quotas. Saving will leave them unchanged.');
  const addBtn = h('button', { class: 'btn btn-sm', type: 'button', onclick: () => togglePicker() }, icon('plus'), 'Add Model');
  const picker = h('div', { class: 'card', style: { display: 'none', padding: '10px', marginTop: '8px' } });
  const modelBox = h('div', null, h('div', { class: 'table-wrap' }, table), empty, loadErr,
    h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px', marginTop: '8px' } }, addBtn,
      h('span', { class: 'muted', style: { fontSize: '11.5px' } }, 'Models without a row are unlimited for this key.')),
    picker);

  function bar(r) {
    const pct = Math.min(100, r.percent);
    return h('span', null,
      h('span', { class: `qbar ${barTone(r.status)}` }, h('i', { style: { width: `${pct}%` } })),
      h('span', { class: `qpct ${barTone(r.status)}` }, `${r.percent}%`),
      h('span', { class: 'muted', style: { marginLeft: '6px', fontSize: '11px' } }, formatTokensCompact(r.usage)));
  }

  function badge(r) {
    if (r.inactive) return h('span', { class: 'badge removed', title: 'Kept, but not enforced' }, REASON_LABEL[r.reason] || 'inactive');
    if (r.notAllowed) return h('span', { class: 'badge err' }, 'not allowed');
    if (r.stored && !allowedNow(r.modelId)) return h('span', { class: 'badge removed' }, 'no access');
    return null;
  }

  function renderRows() {
    tbody.replaceChildren(...rows.flatMap((r) => [h('tr', { class: r.inactive ? 'qrow-inactive' : '' },
      h('td', null, h('code', { style: { fontSize: '12px' } }, r.modelId), ' ', badge(r)),
      h('td', null, r.limit),
      h('td', null, r.period),
      h('td', null, r.inactive ? h('span', { class: 'muted' }, 'not enforced') : (r.stored ? bar(r) : h('span', { class: 'muted' }, 'new'))),
      h('td', { style: { textAlign: 'right' } },
        h('button', { class: 'btn btn-sm', type: 'button', title: 'Remove', onclick: () => { rows.splice(rows.indexOf(r), 1); render(); } }, icon('x')))),
      // Usage history sits on its own full-width line so it never squeezes the model column.
      h('tr', { class: 'qrow-velocity' }, h('td', { colspan: '5', style: { padding: '0 8px', border: '0' } }, r.velocity))]));
    table.style.display = rows.length ? '' : 'none';
    empty.style.display = rows.length || !loaded ? 'none' : '';
    loadErr.style.display = !loaded && isEdit && !rows.length ? '' : 'none';
  }

  // ── Picker (inline panel) ──
  let pickerOpen = false;
  function togglePicker() {
    pickerOpen = !pickerOpen;
    picker.style.display = pickerOpen ? '' : 'none';
    if (pickerOpen) renderPicker('');
  }

  function renderPicker(query) {
    const taken = new Set(rows.map((r) => lc(r.modelId)));
    const q = lc(query).trim();
    const pickable = pickableModels(models, providers, getAccess(), groups, taken).filter((id) => !q || lc(id).includes(q));
    const checks = new Map();
    const search = h('input', { class: 'input', type: 'text', placeholder: 'Search models...', value: query, style: { width: '100%' } });
    search.addEventListener('input', () => renderPicker(search.value));
    const count = h('span', { class: 'muted', style: { fontSize: '11.5px' } }, '0 selected');
    const update = () => { count.textContent = `${[...checks.values()].filter((c) => c.checked).length} selected`; };

    const sections = pickerSections(pickable, groups).map((s) => h('div', null,
      h('div', { class: 'muted', style: { fontSize: '10.5px', fontWeight: '700', textTransform: 'uppercase', margin: '8px 4px 2px' } }, s.title),
      s.models.map((id) => {
        const cb = h('input', { type: 'checkbox' });
        cb.addEventListener('change', update);
        checks.set(id, cb);
        return h('label', { style: { display: 'flex', gap: '6px', alignItems: 'center', padding: '3px 4px', cursor: 'pointer', fontFamily: 'monospace', fontSize: '12px' } }, cb, id);
      })));

    picker.replaceChildren(search,
      h('div', { style: { maxHeight: '190px', overflowY: 'auto' } },
        sections.length ? sections : h('p', { class: 'muted', style: { fontSize: '12px', padding: '8px' } }, 'No more models to add. Only enabled models this key can use are listed.')),
      h('div', { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '8px' } }, count,
        h('button', { class: 'btn btn-sm btn-primary', type: 'button', onclick: () => {
          [...checks.entries()].filter(([, c]) => c.checked).forEach(([id]) => addRow({ model_id: id, quota_period: 'daily' }, false));
          pickerOpen = false;
          picker.style.display = 'none';
          render();
        } }, 'Add selected')));
    if (query) search.focus();
  }

  // ── Mode + render ──
  function setMode(next) {
    mode = next;
    render();
  }

  function render() {
    btnKey.classList.toggle('on', mode === 'key'); btnKey.setAttribute('aria-pressed', String(mode === 'key'));
    btnModel.classList.toggle('on', mode === 'model'); btnModel.setAttribute('aria-pressed', String(mode === 'model'));
    keyBox.style.display = mode === 'key' ? '' : 'none';
    modelBox.style.display = mode === 'model' ? '' : 'none';
    savedHint.textContent = mode === 'key' && rows.length
      ? `▸ ${rows.length} Model Quota${rows.length === 1 ? '' : 's'} saved (inactive while in Key Quota mode)` : '';
    refreshAccess();
    renderRows();
  }

  // Re-check unsaved rows whenever the access settings change.
  function refreshAccess() {
    rows.forEach((r) => { r.notAllowed = !r.stored && !allowedNow(r.modelId); });
    if (modelBox.isConnected) renderRows();
    if (pickerOpen) renderPicker('');
  }

  const node = h('div', { style: { padding: '12px 14px', background: 'var(--hover)', borderRadius: '8px', border: '1px solid var(--border)', display: 'flex', flexDirection: 'column', gap: '8px' } },
    modeBar, keyBox, modelBox,
    h('p', { class: 'muted', style: { fontSize: '11px', margin: 0, lineHeight: 1.4 } },
      '🛡️ Soft Post-Facto Gating: requests are blocked with HTTP 429 once consumed tokens reach the quota. Only one mode is enforced; the other mode\'s settings are kept.'));

  render();

  const rowData = () => rows.map((r) => ({ model_id: r.modelId, quota_limit: r.limit.value === '' ? 0 : Number(r.limit.value) }));

  return {
    node,
    refreshAccess,
    // First problem blocking Save, or '' when fine.
    problem() {
      const bad = validateModelQuotaRows(rowData());
      if (bad) return bad;
      const na = rows.find((r) => r.notAllowed);
      return na ? `${na.modelId} is not allowed by this key's access settings. Remove it or change the access.` : '';
    },
    // Fields merged into the save payload. model_quotas is left out until the stored rows
    // have loaded, so a fast save can never wipe them.
    payload() {
      const out = { quota_mode: mode };
      if (loaded) out.model_quotas = rows.map((r) => ({ model_id: r.modelId, quota_limit: Number(r.limit.value) || 0, quota_period: r.period.value }));
      return out;
    },
    shortModel,
  };
}
