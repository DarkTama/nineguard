import test from 'node:test';
import assert from 'node:assert/strict';

import {
  isValidKeyFormat,
  maskKey,
  buildHeaders,
  getStoredPortalKey,
  setStoredPortalKey,
  clearStoredPortalKey,
  budgetSummary
} from '../static/js/portalhelpers.js';

test('isValidKeyFormat validates key structure', () => {
  assert.equal(isValidKeyFormat(''), false);
  assert.equal(isValidKeyFormat('   '), false);
  assert.equal(isValidKeyFormat(null), false);
  assert.equal(isValidKeyFormat(undefined), false);
  assert.equal(isValidKeyFormat('invalid-key'), false);
  assert.equal(isValidKeyFormat('sk-ng-abc1234567890'), true);
  assert.equal(isValidKeyFormat('sk-proj-xyz123'), true);
  assert.equal(isValidKeyFormat('Bearer sk-ng-abc1234'), true);
});

test('maskKey masks token safely', () => {
  assert.equal(maskKey(''), '-');
  assert.equal(maskKey(null), '-');
  assert.equal(maskKey('sk-1234'), 'sk-...34');
  assert.equal(maskKey('sk-ng-1234567890abcdef'), 'sk-ng-...cdef');
  assert.equal(maskKey('Bearer sk-ng-1234567890abcdef'), 'sk-ng-...cdef');
});

test('buildHeaders constructs authorization header', () => {
  assert.deepEqual(buildHeaders('sk-ng-secret123'), {
    'Authorization': 'Bearer sk-ng-secret123'
  });

  // Strips leading 'Bearer ' if user input already includes it
  assert.deepEqual(buildHeaders('Bearer sk-ng-secret123'), {
    'Authorization': 'Bearer sk-ng-secret123'
  });
});

test('localStorage key management', () => {
  const mockStorage = new Map();
  const fakeLocalStorage = {
    getItem: (k) => mockStorage.get(k) || null,
    setItem: (k, v) => mockStorage.set(k, String(v)),
    removeItem: (k) => mockStorage.delete(k)
  };

  assert.equal(getStoredPortalKey(fakeLocalStorage), '');

  setStoredPortalKey('  sk-ng-test123  ', fakeLocalStorage);
  assert.equal(getStoredPortalKey(fakeLocalStorage), 'sk-ng-test123');

  clearStoredPortalKey(fakeLocalStorage);
  assert.equal(getStoredPortalKey(fakeLocalStorage), '');
});

test('budgetSummary: per-model budgets win over the key budget, total first, tightest row first', () => {
  const s = budgetSummary({
    key_quota: { limit: 1000, used: 10, period: 'daily' },
    model_quotas: [
      { model_id: 'a', limit: 100, used: 20, period: 'daily' },
      { model_id: 'b', limit: 100, used: 150, period: 'weekly' },
      { model_id: 'c', limit: 0, used: 5 },
      { model_id: 'd', limit: 1000, used: 999, period: 'daily', inactive: true },
    ],
  });
  assert.equal(s.kind, 'models');
  assert.deepEqual(s.rows.map((r) => r.modelId), ['b', 'a']);
  assert.equal(s.tightest.modelId, 'b');
  assert.equal(s.tightest.pct, 100);
  assert.equal(s.tightest.left, 0);
  assert.equal(s.tightest.tone, 'err');
  assert.equal(s.exhausted, 1);
  // Total over active budgets: used is capped per row (b counts 100, not 150); tone is the worst row's.
  assert.deepEqual([s.total.used, s.total.limit, s.total.left, s.total.pct, s.total.tone], [120, 200, 80, 60, 'err']);
  assert.deepEqual(s.inactive.map((r) => r.modelId), ['d']);
});

test('budgetSummary: key budget tone crosses warn at 80%', () => {
  assert.equal(budgetSummary({ key_quota: { limit: 1000, used: 799, period: 'daily' } }).tone, 'ok');
  const near = budgetSummary({ key_quota: { limit: 1000, used: 800, period: 'daily' } });
  assert.equal(near.kind, 'key');
  assert.equal(near.tone, 'warn');
  assert.equal(near.left, 200);
});

test('budgetSummary: no limits is unlimited, missing data is unknown', () => {
  assert.equal(budgetSummary({ upstream_pools: [] }).kind, 'unlimited');
  assert.equal(budgetSummary({ key_quota: { limit: 0, used: 5 } }).kind, 'unlimited');
  assert.equal(budgetSummary(null).kind, 'unknown');
});
