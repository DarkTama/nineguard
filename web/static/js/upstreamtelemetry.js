// Display only measured SDK percentages; unknown is not empty capacity.
export const isPercentBucket = b => b?.unit === 'percent';
export function remainingLabel(b) {
  if (!isPercentBucket(b)) return `${b?.remaining_percentage ?? 0}% left`;
  if (b.remaining_percent == null || !Number.isFinite(b.remaining_percent)) return b.limit_reached ? 'Limit reached' : 'Unknown';
  return `${Math.round(b.remaining_percent * 10) / 10}% left`;
}
export function telemetryStateLabel(state) {
  return ({ waiting: 'Waiting for upstream telemetry', observed: 'Observed', stale: 'Stale snapshot', needs_login: 'Login required', disabled: 'Disabled' })[state] || state || 'Waiting for upstream telemetry';
}
export function observationLabel(observedAt, state) {
  const time = observedAt && Date.parse(observedAt);
  return time && Number.isFinite(time) ? `Last observed ${new Date(time).toLocaleString()}${state === 'stale' ? ' — stale' : ''}` : telemetryStateLabel(state);
}
export function freshPercent(b, now = Date.now()) {
  const observed = Date.parse(b?.observed_at);
  return isPercentBucket(b) && !b.incomplete && b.observation_state === 'observed' && Number.isFinite(observed) && now - observed <= 300000 && (!b.reset_at || Date.parse(b.reset_at) > now);
}
// Remaining-capacity tone shared by dashboard and portal: >70% ok, 30-70% warn, <30% err.
export function remainingTone(pct) {
  if (pct > 70) return 'ok';
  if (pct >= 30) return 'warn';
  return 'err';
}
// Meter model for an SDK percent bucket. Unknown, stale or incomplete values get no fill or color,
// so a missing observation never looks like empty (or full) capacity.
export function sdkMeter(b, now = Date.now()) {
  const pct = b?.remaining_percent;
  if (pct == null || !Number.isFinite(pct)) {
    return { known: false, pct: 0, tone: b?.limit_reached ? 'err' : 'muted', dim: true };
  }
  const left = Math.max(0, Math.min(100, pct));
  return { known: true, pct: left, tone: remainingTone(left), dim: !freshPercent(b, now) };
}
