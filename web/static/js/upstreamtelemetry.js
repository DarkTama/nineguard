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
