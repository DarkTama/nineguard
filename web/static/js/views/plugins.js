// Plugins view: Manage token savers, prompt injectors, HTTP filters, pipeline order, and scope bindings.
import { api } from '../api.js';
import { h, icon, toast, formDialog, confirmDialog } from '../ui.js';
import { store } from '../state.js';
import { scopeLabel, scopeSubLabel, formatInheritText, sortPipeline, warningBannerText } from '../pluginhelpers.js';

export function mount(root) {
  let alive = true;
  let pluginsList = [];
  let groupsList = [];
  let keysList = [];
  let modelsList = [];
  let warningsList = [];

  const container = h('div', { class: 'page' });

  // ── Header ──
  const isAdmin = () => !store.authEnabled || store.role === 'admin';

  const registerBtn = h('button', {
    class: 'btn btn-primary',
    type: 'button',
    style: { display: isAdmin() ? 'inline-flex' : 'none' },
    onclick: () => openRegisterModal()
  }, icon('plus'), 'Register HTTP Plugin');

  const header = h('div', { class: 'page-head' },
    h('div', null,
      h('h1', null, 'Plugins & Token Savers'),
      h('p', null, 'Reduce token usage (Headroom, Ponytail, Caveman) or run custom HTTP filters. Key beats Group beats All keys, all models.')
    ),
    h('div', { class: 'page-actions' }, registerBtn)
  );

  // ── First-view Upgrade Banner ──
  function renderUpgradeBanner() {
    const ack = localStorage.getItem('ng.plugins.ack.banner');
    if (ack) return null;

    const banner = h('div', {
      class: 'card',
      style: {
        background: 'var(--hover)',
        borderColor: 'var(--accent)',
        marginBottom: '16px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '12px 16px'
      }
    },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px' } },
        icon('info', 'text-accent'),
        h('span', null,
          h('b', null, 'Plugins available'),
          ' — all token savers are off by default. Precedence: ',
          h('code', null, 'Key beats Group beats All keys, all models'), '.'
        )
      ),
      h('button', {
        class: 'btn btn-sm',
        onclick: () => {
          localStorage.setItem('ng.plugins.ack.banner', '1');
          banner.remove();
        }
      }, 'Dismiss')
    );
    return banner;
  }

  // ── Warnings Banner ──
  function renderWarningsBanner() {
    if (!warningsList.length) return null;
    return h('div', {
      class: 'card',
      style: {
        background: 'rgba(239, 68, 68, 0.08)',
        borderColor: 'var(--danger)',
        marginBottom: '16px',
        padding: '14px'
      }
    },
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', fontWeight: 'bold', color: 'var(--danger)', marginBottom: '8px' } },
        icon('alert'), warningBannerText(warningsList.length)
      ),
      h('ul', { style: { margin: 0, paddingLeft: '20px', fontSize: '12px' } },
        ...warningsList.slice(0, 5).map((w) =>
          h('li', { style: { marginBottom: '4px' } },
            h('b', null, w.key_name ? `${w.key_name}: ` : ''),
            w.message
          )
        )
      )
    );
  }

  // ── Plugin Cards List ──
  const listEl = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '12px', marginBottom: '24px' } });

  function renderList() {
    const sorted = sortPipeline(pluginsList);
    if (!sorted.length) {
      listEl.replaceChildren(h('div', { class: 'card', style: { padding: '24px', textAlign: 'center' } }, 'No plugins registered'));
      return;
    }

    listEl.replaceChildren(...sorted.map((p, idx) => {
      const globBinding = p.global_binding || { state: 'off' };
      const isGlobOn = globBinding.state === 'on';

      const catBadge = h('span', { class: 'badge', style: { textTransform: 'capitalize' } },
        p.category === 'input_compression' ? 'Input Compression' :
        p.category === 'output_style' ? 'Output Style' : 'Other'
      );

      const policyBadge = h('span', {
        class: `badge ${p.failure_policy === 'closed' ? 'badge-danger' : 'badge-neutral'}`
      }, p.failure_policy === 'closed' ? 'Fail Closed' : 'Fail Open');

      const bypassBadge = h('span', { class: 'badge' }, p.bypassable ? 'Bypassable' : 'Strict');

      const latencyLabel = h('span', { class: 'muted', style: { fontSize: '11px', marginLeft: '6px' } });
      const testBtn = h('button', {
        class: 'btn btn-sm',
        onclick: async () => {
          testBtn.disabled = true;
          latencyLabel.textContent = 'Testing...';
          try {
            const res = await api.post(`/plugins/${encodeURIComponent(p.id)}/test`);
            if (res.in_process) {
              latencyLabel.textContent = 'In-process (active)';
            } else if (res.status === 'error') {
              latencyLabel.textContent = `Error: ${res.error || 'Failed'} (${res.latency_ms || 0}ms)`;
            } else {
              latencyLabel.textContent = `${res.latency_ms || 0}ms (${res.status || 'ok'})`;
            }
          } catch (e) {
            latencyLabel.textContent = `Error: ${e.message}`;
          } finally {
            testBtn.disabled = false;
          }
        }
      }, icon('play'), 'Test');

      const toggleGlobBtn = h('button', {
        class: `btn btn-sm ${isGlobOn ? 'btn-primary' : ''}`,
        onclick: async () => {
          const nextState = isGlobOn ? 'off' : 'on';
          if (nextState === 'on' && (p.category === 'input_compression' || p.category === 'output_style')) {
            const ackKey = `ng.plugins.ack.${p.id}`;
            if (!localStorage.getItem(ackKey)) {
              confirmDialog({
                title: `Enable ${p.name} globally?`,
                message: `Check that your upstream providers do not already apply token saving (e.g. 9router RTK, another NineGuard). Stacked token savers can remove context and lower answer quality.\n\nTurn on for All keys, all models?`,
                confirmText: 'Enable',
                onConfirm: async () => {
                  await saveBinding(p.id, 'global', '', nextState, '{}');
                  reload();
                }
              });
              return;
            }
          }
          await saveBinding(p.id, 'global', '', nextState, '{}');
          reload();
        }
      }, isGlobOn ? 'All keys, all models: ON' : 'All keys, all models: OFF');

      const configBtn = h('button', {
        class: 'btn btn-sm',
        onclick: () => openDrawer(p)
      }, icon('pencil'), 'Configure & Scopes');

      const moveUpBtn = h('button', {
        class: 'btn btn-sm',
        disabled: idx === 0,
        title: 'Move up in pipeline',
        onclick: () => reorder(idx, idx - 1)
      }, icon('chevron-left'));

      const moveDownBtn = h('button', {
        class: 'btn btn-sm',
        disabled: idx === sorted.length - 1,
        title: 'Move down in pipeline',
        onclick: () => reorder(idx, idx + 1)
      }, icon('chevron-right'));

      const guidanceCard = p.guidance ? h('div', {
        class: 'muted',
        style: {
          fontSize: '11.5px',
          marginTop: '8px',
          background: 'var(--panel)',
          padding: '8px 12px',
          borderRadius: '6px'
        }
      },
        h('div', null, h('b', null, 'Summary: '), p.guidance.summary || ''),
        p.guidance.recommended_for ? h('div', { style: { marginTop: '2px' } }, h('b', null, 'Recommended for: '), p.guidance.recommended_for) : null,
        p.guidance.not_recommended_for ? h('div', { style: { marginTop: '2px', color: 'var(--warning, #eab308)' } }, h('b', null, 'Not recommended: '), p.guidance.not_recommended_for) : null
      ) : null;

      return h('div', { class: 'card', style: { padding: '14px' } },
        h('div', { style: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '8px' } },
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
            h('span', { class: 'badge', style: { fontWeight: 'bold' } }, `#${p.pipeline_order}`),
            h('h3', { style: { margin: 0, fontSize: '15px' } }, p.name),
            h('span', { class: 'badge' }, p.kind),
            catBadge,
            policyBadge,
            bypassBadge
          ),
          h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
            moveUpBtn, moveDownBtn,
            toggleGlobBtn,
            testBtn, latencyLabel,
            configBtn
          )
        ),
        p.description ? h('p', { class: 'muted', style: { fontSize: '12px', margin: '6px 0 0' } }, p.description) : null,
        guidanceCard
      );
    }));
  }

  // ── Reorder pipeline ──
  async function reorder(fromIdx, toIdx) {
    const sorted = sortPipeline(pluginsList);
    const item = sorted.splice(fromIdx, 1)[0];
    sorted.splice(toIdx, 0, item);
    const orderedIDs = sorted.map((p) => p.id);
    try {
      await api.put('/plugins/order', { order: orderedIDs });
      toast('Pipeline order updated');
      reload();
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  // ── Save Binding ──
  async function saveBinding(pluginID, scopeType, scopeID, state, settings = '{}') {
    try {
      const res = await api.put(`/plugins/${encodeURIComponent(pluginID)}/bindings`, {
        scope_type: scopeType, scope_id: scopeID, state, settings
      });
      toast(`Binding updated: ${state}`);
      if (res.warnings && res.warnings.length) {
        toast(`Notice: ${res.warnings[0].message}`, 'warning');
      }
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  // ── Configure Drawer ──
  async function openDrawer(p) {
    let bindings = [];
    try {
      const bRes = await api.get(`/plugins/${encodeURIComponent(p.id)}/bindings`);
      bindings = bRes.bindings || [];
    } catch { /* ignore */ }

    const bindingMap = new Map();
    for (const b of bindings) {
      bindingMap.set(`${b.scope_type}:${b.scope_id}`, b);
    }

    // Bindings table elements
    const rows = [];

    // 1. Global Row
    const globKey = 'global:';
    const globB = bindingMap.get(globKey) || { state: 'off', settings: '{}' };
    const globSelect = h('select', { class: 'input' },
      h('option', { value: 'off', selected: globB.state === 'off' }, 'Off'),
      h('option', { value: 'on', selected: globB.state === 'on' }, 'On')
    );
    rows.push(h('tr', null,
      h('td', null,
        h('b', null, scopeLabel('global')),
        h('div', { class: 'muted', style: { fontSize: '11px' } }, scopeSubLabel('global'))
      ),
      h('td', null, globSelect),
      h('td', null,
        h('button', {
          class: 'btn btn-sm btn-primary',
          onclick: async () => {
            await saveBinding(p.id, 'global', '', globSelect.value, globB.settings);
            reload();
          }
        }, 'Save')
      )
    ));

    // 2. Groups Rows
    for (const g of groupsList) {
      const gKey = `group:${g.id}`;
      const gb = bindingMap.get(gKey) || { state: 'inherit', settings: '{}' };
      const gSelect = h('select', { class: 'input' },
        h('option', { value: 'inherit', selected: gb.state === 'inherit' }, formatInheritText('group')),
        h('option', { value: 'off', selected: gb.state === 'off' }, 'Off'),
        h('option', { value: 'on', selected: gb.state === 'on' }, 'On')
      );
      rows.push(h('tr', null,
        h('td', null,
          h('b', null, scopeLabel('group', g.name)),
          h('div', { class: 'muted', style: { fontSize: '11px' } }, scopeSubLabel('group', { modelsCount: g.models_count, keysCount: g.keys_count }))
        ),
        h('td', null, gSelect),
        h('td', null,
          h('button', {
            class: 'btn btn-sm',
            onclick: async () => {
              await saveBinding(p.id, 'group', g.id, gSelect.value, gb.settings);
              reload();
            }
          }, 'Save')
        )
      ));
    }

    // 3. Keys Rows
    for (const k of keysList) {
      const kKey = `key:${k.id}`;
      const kb = bindingMap.get(kKey) || { state: 'inherit', settings: '{}' };
      const kSelect = h('select', { class: 'input' },
        h('option', { value: 'inherit', selected: kb.state === 'inherit' }, formatInheritText('key')),
        h('option', { value: 'off', selected: kb.state === 'off' }, 'Off'),
        h('option', { value: 'on', selected: kb.state === 'on' }, 'On')
      );
      rows.push(h('tr', null,
        h('td', null,
          h('b', null, scopeLabel('key', k.name)),
          h('div', { class: 'muted', style: { fontSize: '11px' } }, scopeSubLabel('key'))
        ),
        h('td', null, kSelect),
        h('td', null,
          h('button', {
            class: 'btn btn-sm',
            onclick: async () => {
              await saveBinding(p.id, 'key', k.id, kSelect.value, kb.settings);
              reload();
            }
          }, 'Save')
        )
      ));
    }

    const bindingsTable = h('table', { class: 'table', style: { width: '100%', fontSize: '12px' } },
      h('thead', null,
        h('tr', null,
          h('th', null, 'Scope'),
          h('th', { style: { width: '180px' } }, 'Effective Rule'),
          h('th', { style: { width: '80px' } }, 'Action')
        )
      ),
      h('tbody', null, ...rows)
    );

    // Prompt reset for Caveman / Ponytail
    let promptSection = null;
    if (p.id === 'caveman' || p.id === 'ponytail') {
      promptSection = h('div', { style: { marginBottom: '16px', padding: '12px', background: 'var(--hover)', borderRadius: '6px' } },
        h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
          h('b', null, 'Prompt Customization'),
          h('button', {
            class: 'btn btn-sm',
            onclick: async () => {
              try {
                await api.post(`/plugins/${encodeURIComponent(p.id)}/reset-prompt`);
                toast('Prompt override reset to default');
                reload();
              } catch (e) {
                toast(e.message, 'error');
              }
            }
          }, 'Reset to Default')
        ),
        h('p', { class: 'muted', style: { fontSize: '11px', margin: 0 } },
          'Custom prompt text can be defined in Settings overrides or reset to bundled default prompt text.'
        )
      );
    }

    formDialog({
      title: `Configure ${p.name}`,
      wide: true,
      body: h('div', null,
        promptSection,
        h('h4', { style: { margin: '0 0 8px' } }, 'Scope Precedence & Bindings'),
        h('p', { class: 'muted', style: { fontSize: '11px', margin: '0 0 12px' } },
          'Precedence: Key overrides Group overrides All keys, all models. Inherit defers to the broader scope.'
        ),
        bindingsTable
      ),
      submitText: 'Done',
      cancel: false,
      onSubmit: async () => true
    });
  }

  // ── Register HTTP Plugin Modal ──
  function openRegisterModal() {
    let nameVal = '';
    let urlVal = '';
    let catVal = 'other';
    let timeoutVal = '3000';
    let policyVal = 'open';
    let bypassVal = true;
    let summaryVal = '';

    formDialog({
      title: 'Register HTTP Plugin',
      fields: [
        { label: 'Name', name: 'name', placeholder: 'e.g. PII Filter', required: true },
        { label: 'Endpoint URL', name: 'url', placeholder: 'http://127.0.0.1:9090/transform', required: true },
        {
          label: 'Category', name: 'category', type: 'select',
          options: [
            { value: 'other', label: 'Other (Policy, PII, Guardrails)' },
            { value: 'input_compression', label: 'Input Compression' },
            { value: 'output_style', label: 'Output Style' },
          ]
        },
        { label: 'Timeout (ms)', name: 'timeout_ms', type: 'number', value: '3000' },
        {
          label: 'Failure Policy', name: 'failure_policy', type: 'select',
          options: [
            { value: 'open', label: 'Fail Open (skip on error)' },
            { value: 'closed', label: 'Fail Closed (block request on error)' },
          ]
        },
        { label: 'Summary', name: 'summary', placeholder: 'Optional description of what this plugin transforms' },
      ],
      onSubmit: async (data) => {
        try {
          const res = await api.post('/plugins', {
            name: data.name,
            url: data.url,
            category: data.category || 'other',
            timeout_ms: parseInt(data.timeout_ms || '3000', 10),
            failure_policy: data.failure_policy || 'open',
            bypassable: true,
            summary: data.summary || '',
          });
          toast('Plugin registered');
          reload();

          // Show secret modal once
          if (res.secret) {
            formDialog({
              title: 'Plugin Shared Secret',
              body: h('div', null,
                h('p', null, 'Copy this secret now. It will not be shown again:'),
                h('input', { class: 'input', value: res.secret, readOnly: true, style: { width: '100%', fontFamily: 'monospace' } })
              ),
              submitText: 'I have copied the secret',
              cancel: false,
              onSubmit: async () => true
            });
          }
          return true;
        } catch (e) {
          toast(e.message, 'error');
          return false;
        }
      }
    });
  }

  // ── Live Plugin & Endpoint Probe ──
  const probeCard = h('div', { class: 'card', style: { padding: '16px' } });

  function renderProbe() {
    const activeKeys = keysList.filter((k) => k.is_active !== false);
    const keySelect = h('select', { class: 'input', style: { minWidth: '220px' } },
      ...activeKeys.map((k) => h('option', { value: k.raw_key || k.key, 'data-id': k.id }, `${k.name} (${k.key})`))
    );

    const activeModels = modelsList.filter((m) => m.enabled !== false);
    const datalistId = 'plugin-probe-models';
    const datalist = h('datalist', { id: datalistId },
      ...activeModels.map((m) => h('option', { value: m.id }))
    );

    const defaultModel = activeModels[0] ? activeModels[0].id : '9router/anthropic/claude-3.5-sonnet';
    const modelInput = h('input', {
      class: 'input',
      list: datalistId,
      placeholder: 'Select or type model...',
      value: defaultModel,
      style: { minWidth: '260px' }
    });

    const promptInput = h('input', {
      class: 'input',
      type: 'text',
      placeholder: 'Test prompt...',
      value: 'Hi NineGuard! Give me a 1-sentence response.',
      style: { flex: '1', minWidth: '260px' }
    });

    const resultBox = h('div', {
      style: { display: 'none', marginTop: '14px', padding: '12px', background: 'var(--hover)', borderRadius: '6px' }
    });

    // 1. Live Ping Button
    const pingBtn = h('button', {
      class: 'btn btn-primary',
      type: 'button',
      onclick: async () => {
        const model = modelInput.value.trim();
        const key = keySelect.value;
        const prompt = promptInput.value.trim();

        if (!model) return toast('No model specified', 'warn');
        if (!key) return toast('No API key selected', 'warn');

        pingBtn.disabled = true;
        pingBtn.textContent = 'Sending...';
        resultBox.style.display = 'block';
        resultBox.replaceChildren(h('span', { class: 'muted' }, 'Relaying through NineGuard proxy /v1/chat/completions...'));

        const startTime = performance.now();
        try {
          const resp = await fetch('/v1/chat/completions', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${key}`,
            },
            body: JSON.stringify({
              model,
              messages: [{ role: 'user', content: prompt || 'ping' }],
              stream: false,
            })
          });

          const duration = Math.round(performance.now() - startTime);
          const pluginsApplied = resp.headers.get('X-NineGuard-Plugins-Applied') || '';
          const tokensSaved = resp.headers.get('X-NineGuard-Tokens-Saved') || '';
          const data = await resp.json().catch(() => null);

          if (resp.ok) {
            const content = data?.choices?.[0]?.message?.content || '(Empty content)';
            const totalTok = data?.usage?.total_tokens || 0;

            const pluginBadges = [];
            if (pluginsApplied) {
              pluginBadges.push(h('span', {
                class: 'badge',
                style: { background: 'rgba(249, 115, 22, 0.15)', color: 'var(--accent)', border: '1px solid rgba(249, 115, 22, 0.4)' }
              }, '🧩 ' + pluginsApplied));
            }
            if (tokensSaved && parseInt(tokensSaved, 10) > 0) {
              pluginBadges.push(h('span', { class: 'badge ok' }, `-${tokensSaved} tokens saved`));
            }

            resultBox.replaceChildren(
              h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px', marginBottom: '8px' } },
                h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } },
                  h('span', { class: 'badge ok' }, `HTTP ${resp.status} OK`),
                  ...pluginBadges
                ),
                h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms · Tokens: ${totalTok}`)
              ),
              h('div', { style: { fontSize: '13px', whiteSpace: 'pre-wrap', lineHeight: '1.5', fontFamily: 'monospace', padding: '8px', background: 'var(--panel)', borderRadius: '4px' } }, content)
            );
            toast('Live request succeeded!', 'ok');
          } else {
            const errText = data?.error?.message || `HTTP ${resp.status} ${resp.statusText}`;
            resultBox.replaceChildren(
              h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' } },
                h('span', { class: 'badge err' }, `HTTP ${resp.status} Failed`),
                h('span', { class: 'muted', style: { fontSize: '12px' } }, `Latency: ${duration}ms`)
              ),
              h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, errText)
            );
            toast(`Request failed: ${errText}`, 'error');
          }
        } catch (err) {
          const duration = Math.round(performance.now() - startTime);
          resultBox.replaceChildren(
            h('div', { style: { color: 'var(--danger)', fontSize: '13px' } }, `Network error: ${err.message} (${duration}ms)`)
          );
          toast(`Network error: ${err.message}`, 'error');
        } finally {
          pingBtn.disabled = false;
          pingBtn.replaceChildren(icon('sparkles'), 'Test Live Ping');
        }
      }
    }, icon('sparkles'), 'Test Live Ping');

    // 2. Scope Preview Button
    const previewBtn = h('button', {
      class: 'btn btn-secondary',
      type: 'button',
      onclick: async () => {
        const selectedOpt = keySelect.selectedOptions?.[0];
        const keyID = selectedOpt?.getAttribute('data-id') || '';
        const model = modelInput.value.trim();

        previewBtn.disabled = true;
        try {
          const q = new URLSearchParams();
          if (keyID) q.set('key_id', keyID);
          if (model) q.set('model', model);
          const res = await api.get(`/plugins/resolve?${q.toString()}`);

          resultBox.style.display = 'block';
          resultBox.replaceChildren(
            h('div', { style: { marginBottom: '8px', fontWeight: 'bold', fontSize: '12.5px' } },
              `Scope Precedence for ${model || '(any model)'}:`
            ),
            h('table', { class: 'table', style: { width: '100%', fontSize: '12px' } },
              h('thead', null,
                h('tr', null,
                  h('th', null, 'Plugin'),
                  h('th', null, 'Effective State'),
                  h('th', null, 'Decided By'),
                  h('th', null, 'Overridden Candidates')
                )
              ),
              h('tbody', null,
                ...(res.plugins || []).map((rp) => {
                  const runs = rp.effective_state === 'on';
                  const overriddenText = (rp.overridden || []).map((o) => o.label).join(', ') || '—';
                  return h('tr', null,
                    h('td', null, h('b', null, rp.plugin.name)),
                    h('td', null, h('span', { class: `badge ${runs ? 'badge-primary' : ''}` }, runs ? 'ON' : 'OFF')),
                    h('td', null, rp.decided_by?.label || '—'),
                    h('td', { class: 'muted' }, overriddenText)
                  );
                })
              )
            )
          );
        } catch (e) {
          resultBox.style.display = 'block';
          resultBox.replaceChildren(h('p', { class: 'text-danger' }, e.message));
        } finally {
          previewBtn.disabled = false;
        }
      }
    }, icon('table'), 'Check Scope Preview');

    probeCard.replaceChildren(
      h('div', { class: 'card-head' },
        h('div', null,
          h('h2', null, 'Live Plugin & Endpoint Probe'),
          h('p', { class: 'card-sub' }, 'Test the full pipeline (Agent → NineGuard Plugins → Upstream) and inspect transformed response style in real time.')
        )
      ),
      h('div', { style: { padding: '16px' } },
        h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' } },
          keySelect,
          modelInput,
          datalist,
          promptInput,
          pingBtn,
          previewBtn
        ),
        resultBox
      )
    );
  }

  // ── Reload data ──
  async function reload() {
    try {
      const [pRes, gRes, kRes, wRes, mRes] = await Promise.all([
        api.get('/plugins'),
        api.get('/model-groups'),
        api.get('/keys'),
        api.get('/plugins/warnings'),
        api.get('/models').catch(() => []),
      ]);
      pluginsList = pRes.plugins || [];
      groupsList = gRes.groups || [];
      keysList = kRes.keys || [];
      warningsList = wRes.warnings || [];
      modelsList = Array.isArray(mRes) ? mRes : (mRes?.models || []);

      if (!alive) return;
      render();
    } catch (e) {
      if (alive) toast(e.message, 'error');
    }
  }

  function render() {
    const banner = renderUpgradeBanner();
    const warnings = renderWarningsBanner();
    renderList();
    renderProbe();

    container.replaceChildren(
      header,
      banner || '',
      warnings || '',
      listEl,
      probeCard
    );
  }

  root.replaceChildren(container);
  reload();

  return {
    destroy() {
      alive = false;
    },
    update() {
      reload();
    }
  };
}
