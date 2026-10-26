package providerengine

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"nineguard/internal/providers"
	"nineguard/internal/upstream"
)

func observationState(observed time.Time, reset *time.Time, now time.Time) string {
	if observed.IsZero() {
		return "waiting"
	}
	if now.Sub(observed) > 5*time.Minute || (reset != nil && !now.Before(*reset)) {
		return "stale"
	}
	return "observed"
}
func parseQuotaSignals(family string, signals map[string]string, observed, now time.Time, scope string) []upstream.QuotaBucket {
	rows := []upstream.QuotaBucket{}
	add := func(id, name, usedRaw, resetRaw, afterRaw, status string, multiplier float64, minutes int64) {
		var used, left *float64
		if v, err := strconv.ParseFloat(usedRaw, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			v = math.Max(0, math.Min(100, v*multiplier))
			remaining := 100 - v
			used, left = &v, &remaining
		}
		reached := status == "rejected"
		var reset *time.Time
		if seconds, err := strconv.ParseInt(resetRaw, 10, 64); err == nil && seconds > 0 && seconds <= 253402300799 {
			t := time.Unix(seconds, 0).UTC()
			reset = &t
		} else if seconds, err := strconv.ParseInt(afterRaw, 10, 64); err == nil && seconds >= 0 && !observed.IsZero() && seconds <= 31536000 {
			t := observed.Add(time.Duration(seconds) * time.Second)
			reset = &t
		}
		if used == nil && !reached && reset == nil {
			return
		}
		state := observationState(observed, reset, now)
		var stamp *time.Time
		if !observed.IsZero() {
			t := observed
			stamp = &t
		}
		rows = append(rows, upstream.QuotaBucket{Name: name, Unit: "percent", UsedPercent: used, RemainingPercent: left, Scope: scope, WindowMinutes: minutes, ObservedAt: stamp, ObservationState: state, LimitReached: reached, ResetAt: reset, Warning: state == "observed" && left != nil && *left < 25})
	}
	if family == "claude" {
		for _, window := range []struct {
			id, name string
			minutes  int64
		}{{"5h", "5-hour window", 300}, {"7d", "7-day window", 10080}} {
			prefix := "Anthropic-Ratelimit-Unified-" + window.id + "-"
			add(window.id, window.name, signals[prefix+"Utilization"], signals[prefix+"Reset"], "", signals[prefix+"Status"], 100, window.minutes)
		}
	} else if family == "codex" {
		prefixes := map[string]bool{"X-Codex-Primary-": true, "X-Codex-Secondary-": true}
		for k := range signals {
			if strings.HasPrefix(k, "X-Codex-") && strings.HasSuffix(k, "Used-Percent") {
				prefixes[strings.TrimSuffix(k, "Used-Percent")] = true
			}
		}
		keys := []string{}
		for k := range prefixes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, prefix := range keys {
			minutes, _ := strconv.ParseInt(signals[prefix+"Window-Minutes"], 10, 64)
			name := strings.TrimSuffix(strings.TrimPrefix(prefix, "X-Codex-"), "-") + " window"
			if minutes > 0 {
				name += " (" + strconv.FormatInt(minutes, 10) + " min)"
			}
			add(prefix, name, signals[prefix+"Used-Percent"], signals[prefix+"Reset-At"], signals[prefix+"Reset-After-Seconds"], "", 1, minutes)
		}
	}
	return rows
}

// SDKQuotas reads passive observations only. It never invokes inference, OAuth refresh, or another gateway.
func (e *Engine) SDKQuotas(ctx context.Context, p providers.Provider, now time.Time) (*upstream.ProviderQuotaResult, error) {
	result := &upstream.ProviderQuotaResult{ProviderID: p.ID, ProviderName: p.Name, QuotaType: "sdk", Source: "sdk", Status: "ok", ObservationState: "waiting", Connections: []upstream.ConnectionQuota{}}
	if p.Backend != "cliproxy" || (p.Family != "claude" && p.Family != "codex") {
		result.Status = "unsupported"
		result.ErrorMsg = "This SDK connector has no verified quota telemetry schema."
		return result, nil
	}
	accounts, err := e.Accounts(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if account.Status == "disabled" {
			continue
		}
		a, ok := e.manager.GetByID(account.ID)
		if !ok || a.Disabled {
			continue
		}
		connection := upstream.ConnectionQuota{ConnectionID: account.ID, Provider: p.Family, AccountLabel: account.Label, Status: account.Status, Source: "sdk", ObservationState: "waiting", Buckets: []upstream.QuotaBucket{}}
		if account.Status == "connected" {
			connection.Status = "ok"
		} else {
			connection.ErrorMsg = account.LastError
			result.Status = "degraded"
		}
		if plan, ok := a.Metadata["plan_type"].(string); ok {
			connection.Plan = plan
		}
		if account.Status != "needs_login" {
			connection.Buckets = parseQuotaSignals(p.Family, a.Quota.Signals, a.Quota.ObservedAt, now, "account")
			for model, state := range a.ModelStates {
				if state != nil && state.Quota.ObservedAt.After(a.Quota.ObservedAt) {
					connection.Buckets = append(connection.Buckets, parseQuotaSignals(p.Family, state.Quota.Signals, state.Quota.ObservedAt, now, model)...)
				}
			}
		}
		sort.Slice(connection.Buckets, func(i, j int) bool {
			a, b := connection.Buckets[i], connection.Buckets[j]
			if a.Scope != b.Scope {
				return a.Scope < b.Scope
			}
			return a.Name < b.Name
		})
		for _, b := range connection.Buckets {
			if b.ObservedAt != nil && (connection.ObservedAt == nil || b.ObservedAt.After(*connection.ObservedAt)) {
				connection.ObservedAt = b.ObservedAt
				connection.LastUpdated = *b.ObservedAt
			}
			if b.ObservationState == "stale" {
				connection.ObservationState = "stale"
			} else if connection.ObservationState == "waiting" {
				connection.ObservationState = b.ObservationState
			}
		}
		if connection.ObservedAt != nil {
			if result.ObservedAt == nil || connection.ObservedAt.After(*result.ObservedAt) {
				result.ObservedAt = connection.ObservedAt
				result.LastUpdated = *connection.ObservedAt
			}
			if connection.ObservationState == "stale" {
				result.ObservationState = "stale"
			} else if result.ObservationState == "waiting" {
				result.ObservationState = "observed"
			}
		}
		result.Connections = append(result.Connections, connection)
	}
	return result, nil
}
