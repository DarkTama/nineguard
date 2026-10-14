package upstream

import (
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultSyncInterval is how often the background worker refreshes upstream quotas.
	DefaultSyncInterval = 10 * time.Minute
	// MinSyncInterval is the lowest allowed sync interval.
	MinSyncInterval = 5 * time.Minute
	// CacheTTLBuffer is added on top of the sync interval to get the cache TTL,
	// so one slow or missed sync does not make readers fall back to a cold fetch.
	CacheTTLBuffer = 5 * time.Minute
)

// AllowedSyncIntervals lists the selectable sync intervals.
var AllowedSyncIntervals = []time.Duration{
	5 * time.Minute,
	10 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
}

// ParseSyncInterval parses and validates a sync interval string (e.g. "10m").
// Values below MinSyncInterval clamp up to MinSyncInterval. Values that are not
// one of AllowedSyncIntervals (after clamping) are rejected.
func ParseSyncInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q: use one of 5m, 10m, 15m, 30m", s)
	}
	if d < MinSyncInterval {
		d = MinSyncInterval
	}
	for _, a := range AllowedSyncIntervals {
		if d == a {
			return d, nil
		}
	}
	return 0, fmt.Errorf("invalid interval %q: use one of 5m, 10m, 15m, 30m", s)
}

// CacheTTLForInterval returns the cache TTL matching a sync interval (interval + 5m).
func CacheTTLForInterval(interval time.Duration) time.Duration {
	return interval + CacheTTLBuffer
}

// FormatInterval renders an interval like "10m".
func FormatInterval(d time.Duration) string {
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
