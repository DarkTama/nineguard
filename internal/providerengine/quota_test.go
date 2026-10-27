package providerengine

import (
	"testing"
	"time"
)

func TestSDKQuotaSignals(t *testing.T) {
	observed := time.Unix(1800000000, 0).UTC()
	claude := map[string]string{"Anthropic-Ratelimit-Unified-5h-Utilization": "0.53", "Anthropic-Ratelimit-Unified-5h-Reset": "1800018000", "Anthropic-Ratelimit-Unified-7d-Utilization": "0"}
	rows := parseQuotaSignals("claude", claude, observed, observed, "account")
	if len(rows) != 2 || rows[0].UsedPercent == nil || *rows[0].UsedPercent != 53 || *rows[0].RemainingPercent != 47 || *rows[1].RemainingPercent != 100 || rows[0].Total != 0 || rows[0].Unit != "percent" {
		t.Fatalf("fraction parsing: %+v", rows)
	}
	if rows[0].ResetAt == nil || rows[0].ResetAt.Unix() != 1800018000 {
		t.Fatal("reset lost")
	}
	stale := parseQuotaSignals("claude", claude, observed, observed.Add(5*time.Minute+time.Nanosecond), "account")
	if stale[0].ObservationState != "stale" || !stale[0].ObservedAt.Equal(observed) {
		t.Fatal("stale observation was redated")
	}
	for _, bad := range []string{"NaN", "Inf", "broken", ""} {
		r := parseQuotaSignals("claude", map[string]string{"Anthropic-Ratelimit-Unified-5h-Utilization": bad}, observed, observed, "account")
		if len(r) != 0 {
			t.Fatalf("invalid value accepted: %s", bad)
		}
	}
	rejected := parseQuotaSignals("claude", map[string]string{"Anthropic-Ratelimit-Unified-5h-Status": "rejected"}, observed, observed, "account")
	if len(rejected) != 1 || !rejected[0].LimitReached || rejected[0].UsedPercent != nil {
		t.Fatal("rejection invented percent")
	}
	codex := parseQuotaSignals("codex", map[string]string{"X-Codex-Primary-Used-Percent": "51", "X-Codex-Primary-Window-Minutes": "300", "X-Codex-Primary-Reset-After-Seconds": "120"}, observed, observed.Add(time.Minute), "account")
	if len(codex) != 1 || *codex[0].UsedPercent != 51 || codex[0].ResetAt.Unix() != observed.Unix()+120 {
		t.Fatal("Codex percent/reset anchor wrong")
	}
}

func TestSDKQuotaMalformedResetRemainsSerializable(t *testing.T) {
	now := time.Unix(1800000000, 0)
	rows := parseQuotaSignals("claude", map[string]string{"Anthropic-Ratelimit-Unified-5h-Utilization": "0.53", "Anthropic-Ratelimit-Unified-5h-Reset": "253402300800"}, now, now, "account")
	if len(rows) != 1 || rows[0].ResetAt != nil || rows[0].RemainingPercent == nil {
		t.Fatal("invalid reset retained or measurement lost")
	}
}
