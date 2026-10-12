import test from 'node:test';
import assert from 'node:assert/strict';
import {
  quotaStatus, barTone, shortModel, formatResetIn, matchModelPattern, isModelAllowed, isCanonicalModelId,
  pickableModels, pickerSections, validateModelQuotaRows, quotaFiltersFromParams, quotaParamsFromFilters,
  filterQuotaKeys, nearLimitRows,
} from '../static/js/modelquotas.js';

const providers = [
  { prefix: '9router', is_default: true, is_active: true },
  { prefix: 'openai', is_default: false, is_active: true },
  { prefix: 'dead', is_default: false, is_active: false },
];

test('quotaStatus and barTone', () => {
  assert.equal(quotaStatus(102, 80), 'exhausted');
  assert.equal(quotaStatus(86, 80), 'near');
  assert.equal(quotaStatus(79, 80), 'ok');
  assert.equal(quotaStatus(99, 80, true), 'exhausted');
  assert.deepEqual(['exhausted', 'near', 'ok'].map(barTone), ['err', 'warn', 'ok']);
});

test('shortModel and formatResetIn', () => {
  assert.equal(shortModel('9router/claude-opus-4'), 'claude-opus-4');
  assert.equal(shortModel('openrouter/anthropic/x'), 'x');
  assert.equal(shortModel('bare'), 'bare');
  const now = Date.parse('2026-10-12T18:00:00Z');
  assert.equal(formatResetIn(null, now), 'never');
  assert.equal(formatResetIn('2026-10-13T00:12:00Z', now), '6h 12m');
  assert.equal(formatResetIn('2026-10-15T00:00:00Z', now), '2d 6h');
  assert.equal(formatResetIn('2026-10-12T18:00:30Z', now), '<1m');
  assert.equal(formatResetIn('2026-10-12T18:25:00Z', now), '25m');
});

test('matchModelPattern mirrors the server rules', () => {
  assert.ok(matchModelPattern('gpt-4o', 'openai/gpt-4o'));
  assert.ok(matchModelPattern('openai/gpt-4o', 'gpt-4o'));
  assert.ok(matchModelPattern('openai/*', 'openai/x'));
  assert.ok(!matchModelPattern('openai/*', 'other/x'));
  assert.ok(matchModelPattern('*.flash', 'google/gemini.flash'));
  assert.ok(matchModelPattern('*', 'anything') && matchModelPattern('ALL', 'anything'));
  assert.ok(!matchModelPattern('gpt-4o', 'openai/gpt-4'));
});

test('isModelAllowed by access mode', () => {
  const groups = [{ id: 'g1', name: 'Claude', models: ['9router/claude-*', '9router/a'] }, { id: 'g2', name: 'All', models: ['*'] }];
  assert.ok(isModelAllowed({ mode: 'all' }, groups, 'x/y'));
  assert.ok(isModelAllowed({ mode: 'custom', allowed: [] }, groups, 'x/y')); // empty custom = all
  assert.ok(!isModelAllowed({ mode: 'custom', allowed: ['9router/a'] }, groups, '9router/b'));
  assert.ok(isModelAllowed({ mode: 'group', groupIds: ['g1'] }, groups, '9router/a'));
  assert.ok(!isModelAllowed({ mode: 'group', groupIds: ['g1'] }, groups, '9router/b'));
  assert.ok(isModelAllowed({ mode: 'group', groupIds: ['g2'] }, groups, 'whatever/z'));
  assert.ok(!isModelAllowed({ mode: 'group', groupIds: [] }, groups, '9router/a'));
});

test('isCanonicalModelId mirrors ResolveModelID', () => {
  assert.ok(isCanonicalModelId(providers, '9router/a'));
  assert.ok(isCanonicalModelId(providers, 'openai/GPT-4o'));
  assert.ok(!isCanonicalModelId(providers, 'a')); // bare, default provider has a prefix
  assert.ok(!isCanonicalModelId(providers, 'OPENAI/x')); // prefix is matched case-sensitively
  assert.ok(!isCanonicalModelId(providers, 'dead/x')); // inactive provider falls to the default
  assert.ok(isCanonicalModelId([{ prefix: '', is_default: true, is_active: true }], 'gpt-4o'));
  assert.ok(!isCanonicalModelId([], '9router/a'));
});

test('pickableModels and pickerSections', () => {
  const models = [
    { id: '9router/a', enabled: true }, { id: '9router/b', enabled: true }, { id: '9router/off', enabled: false },
    { id: '9router/gone', enabled: true, removed: true }, { id: 'a', enabled: true }, { id: 'openai/x', enabled: true },
  ];
  const groups = [{ id: 'g1', name: 'Router', models: ['9router/*'] }];
  const access = { mode: 'all' };
  const p = pickableModels(models, providers, access, groups, new Set(['9router/b']));
  assert.deepEqual(p, ['9router/a', 'openai/x']);
  assert.deepEqual(pickerSections(p, groups), [
    { title: 'Router', models: ['9router/a'] },
    { title: 'Other allowed models', models: ['openai/x'] },
  ]);
});

test('validateModelQuotaRows', () => {
  assert.equal(validateModelQuotaRows([{ model_id: 'a/b', quota_limit: 5 }]), '');
  assert.match(validateModelQuotaRows([{ model_id: 'a/b', quota_limit: 0 }]), /greater than 0/);
  assert.match(validateModelQuotaRows([{ model_id: 'a/b', quota_limit: '1.5' }]), /greater than 0/);
  assert.match(validateModelQuotaRows([{ model_id: 'a/b', quota_limit: 1 }, { model_id: 'A/B', quota_limit: 2 }]), /Duplicate/);
  assert.match(validateModelQuotaRows([{ model_id: ' ', quota_limit: 1 }]), /no model/);
});

const keys = [
  { id: '1', name: 'ari', quota_mode: 'model', status: 'exhausted', key_quota: null, model_quotas: [
    { model_id: 'p/a', percent: 102, status: 'exhausted', reset_at: 'x', inactive: false },
    { model_id: 'p/b', percent: 86, status: 'near', inactive: false },
    { model_id: 'p/gone', percent: 0, status: 'inactive', inactive: true }] },
  { id: '2', name: 'bot', quota_mode: 'key', status: 'near', key_quota: { percent: 82, status: 'near', reset_at: 'y' }, model_quotas: [] },
  { id: '3', name: 'free', quota_mode: 'key', status: 'unlimited', key_quota: null, model_quotas: [] },
  { id: '4', name: 'quiet', quota_mode: 'model', status: 'ok', key_quota: null, model_quotas: [{ model_id: 'p/z', percent: 5, status: 'ok', inactive: false }] },
];

test('filter state round-trips through route params', () => {
  assert.deepEqual(quotaFiltersFromParams({}), { q: '', mode: 'all', status: 'all', unlimited: '' });
  assert.deepEqual(quotaFiltersFromParams({ mode: 'bogus', status: 'near', unlimited: '1', q: 'x' }), { q: 'x', mode: 'all', status: 'near', unlimited: '1' });
  assert.deepEqual(quotaParamsFromFilters({ q: '', mode: 'all', status: 'near', unlimited: '' }), { status: 'near' });
});

test('filterQuotaKeys', () => {
  const f = (o) => filterQuotaKeys(keys, { q: '', mode: 'all', status: 'all', unlimited: '', ...o }).map((k) => k.name);
  assert.deepEqual(f({}), ['ari', 'bot', 'quiet']); // unlimited key hidden by default
  assert.deepEqual(f({ unlimited: '1' }), ['ari', 'bot', 'free', 'quiet']);
  assert.deepEqual(f({ mode: 'key' }), ['bot']);
  assert.deepEqual(f({ status: 'near' }), ['ari', 'bot']); // near includes exhausted
  assert.deepEqual(f({ status: 'exhausted' }), ['ari']);
  assert.deepEqual(f({ status: 'inactive' }), ['ari']);
  assert.deepEqual(f({ q: 'p/z' }), ['quiet']); // search matches model ids
  assert.deepEqual(f({ q: 'BOT' }), ['bot']);
});

test('nearLimitRows orders exhausted first then by percent', () => {
  const rows = nearLimitRows(keys, 5);
  assert.deepEqual(rows.map((r) => [r.keyName, r.label, r.status]), [
    ['ari', 'a', 'exhausted'], ['ari', 'b', 'near'].map((x) => x), ['bot', 'all models', 'near'],
  ].map((x) => x));
  assert.equal(nearLimitRows(keys, 1).length, 1);
});
