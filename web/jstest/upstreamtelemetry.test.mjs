import test from 'node:test';
import assert from 'node:assert/strict';
import { remainingLabel, observationLabel, freshPercent } from '../static/js/upstreamtelemetry.js';

test('SDK remaining distinguishes measured zero from absent values', () => {
  assert.equal(remainingLabel({ unit: 'percent', remaining_percent: 0 }), '0% left');
  assert.equal(remainingLabel({ unit: 'percent' }), 'Unknown');
  assert.equal(remainingLabel({ unit: 'percent', limit_reached: true }), 'Limit reached');
  assert.equal(remainingLabel({ unit: 'percent', remaining_percent: 47 }), '47% left');
});
test('SDK fresh snapshot excludes stale, partial and past reset measurements', () => {
  const now = Date.parse('2026-10-26T12:00:00Z');
  const b = { unit: 'percent', observation_state: 'observed', observed_at: '2026-10-26T11:59:00Z', remaining_percent: 5 };
  assert.equal(freshPercent(b, now), true);
  assert.equal(freshPercent({ ...b, incomplete: true }, now), false);
  assert.equal(freshPercent({ ...b, reset_at: '2026-10-26T11:59:50Z' }, now), false);
  assert.equal(freshPercent({ ...b, observed_at: '2026-10-26T11:00:00Z' }, now), false);
  assert.equal(observationLabel(null, 'waiting'), 'Waiting for upstream telemetry');
  assert.equal(observationLabel('invalid', 'waiting'), 'Waiting for upstream telemetry');
});
