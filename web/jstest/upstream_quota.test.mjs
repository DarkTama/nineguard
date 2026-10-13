import test from 'node:test';
import assert from 'node:assert/strict';
import { upstreamBarTone } from '../static/js/views/upstream_quotas.js';

test('upstreamBarTone assigns correct status color tones', () => {
  // > 70% is ok (green)
  assert.equal(upstreamBarTone(100), 'ok');
  assert.equal(upstreamBarTone(75), 'ok');
  assert.equal(upstreamBarTone(71), 'ok');

  // 30 - 70% is warn (yellow)
  assert.equal(upstreamBarTone(70), 'warn');
  assert.equal(upstreamBarTone(50), 'warn');
  assert.equal(upstreamBarTone(30), 'warn');

  // < 30% is danger (red)
  assert.equal(upstreamBarTone(29), 'danger');
  assert.equal(upstreamBarTone(22), 'danger');
  assert.equal(upstreamBarTone(0), 'danger');
});
