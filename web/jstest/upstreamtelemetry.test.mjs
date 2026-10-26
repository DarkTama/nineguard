import test from 'node:test';
import assert from 'node:assert/strict';
import { remainingLabel, observationLabel, freshPercent, sdkMeter } from '../static/js/upstreamtelemetry.js';

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
test('SDK meter colors measured remaining capacity and never paints unknown as empty', () => {
  const now = Date.parse('2026-10-26T12:00:00Z');
  const fresh = { unit: 'percent', observation_state: 'observed', observed_at: '2026-10-26T11:59:00Z' };
  assert.deepEqual(sdkMeter({ ...fresh, remaining_percent: 85 }, now), { known: true, pct: 85, tone: 'ok', dim: false });
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 70 }, now).tone, 'warn');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 30 }, now).tone, 'warn');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 29.9 }, now).tone, 'err');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 0 }, now).known, true);
  assert.deepEqual(sdkMeter({ ...fresh }, now), { known: false, pct: 0, tone: 'muted', dim: true });
  assert.equal(sdkMeter({ ...fresh, limit_reached: true }, now).tone, 'err');
  assert.equal(sdkMeter({ ...fresh, remaining_percent: 60, observation_state: 'stale' }, now).dim, true);
});
