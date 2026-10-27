package upstream

import (
	"testing"
	"time"
)

func TestSDKQuotaAlertsRequireFreshCompleteMeasurement(t *testing.T) {
	now := time.Now()
	left := 5.0
	result := &ProviderQuotaResult{ProviderID: "sdk", ProviderName: "SDK", QuotaType: "sdk", Source: "sdk", Status: "ok", Connections: []ConnectionQuota{{ConnectionID: "a", Status: "ok", Buckets: []QuotaBucket{{Name: "5-hour window", Unit: "percent", RemainingPercent: &left, ObservedAt: &now, ObservationState: "stale", Warning: true}}}}}
	alerts := NewQuotaAlerter()
	if len(alerts.Observe(result, now)) != 0 {
		t.Fatal("stale snapshot alerted")
	}
	result.Connections[0].Buckets[0].ObservationState = "observed"
	if len(alerts.Observe(result, now)) != 1 {
		t.Fatal("fresh measured depletion did not alert")
	}
	result.Connections[0].Buckets[0].Incomplete = true
	if len(alerts.Observe(result, now)) != 0 {
		t.Fatal("partial snapshot changed alert state")
	}
}

func TestSDKQuotaAlertScopesDoNotRecoverEachOther(t *testing.T) {
	now := time.Now()
	low, high := 5.0, 95.0
	result := &ProviderQuotaResult{ProviderID: "sdk", QuotaType: "sdk", Status: "ok", Connections: []ConnectionQuota{{ConnectionID: "a", Status: "ok", Buckets: []QuotaBucket{
		{Name: "5-hour window", Unit: "percent", Scope: "account", WindowMinutes: 300, RemainingPercent: &low, ObservedAt: &now, ObservationState: "observed", Warning: true},
		{Name: "5-hour window", Unit: "percent", Scope: "claude-model", WindowMinutes: 300, RemainingPercent: &high, ObservedAt: &now, ObservationState: "observed"},
	}}}}
	a := NewQuotaAlerter()
	if got := a.Observe(result, now); len(got) != 1 {
		t.Fatalf("scope collision emitted %v", got)
	}
	if got := a.Observe(result, now); len(got) != 0 {
		t.Fatalf("scope collision repeated %v", got)
	}
	result.Connections[0].Buckets[0].RemainingPercent = nil
	result.Connections[0].Buckets[0].Warning = false
	if got := a.Observe(result, now); len(got) != 0 {
		t.Fatalf("unknown snapshot announced recovery: %v", got)
	}
}
