package upstream

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSDKQuotaPoolsConservativeAndPrivate(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	accounts := []ConnectionQuota{
		{ConnectionID: "secret-a", AccountLabel: "a@example.test", Provider: "claude", Source: "sdk", Status: "ok", Buckets: []QuotaBucket{{Name: "5-hour window", Unit: "percent", Scope: "account", WindowMinutes: 300, UsedPercent: f(53), RemainingPercent: f(47), ObservationState: "observed"}}},
		{ConnectionID: "secret-b", AccountLabel: "b@example.test", Provider: "claude", Source: "sdk", Status: "ok", Buckets: []QuotaBucket{{Name: "5-hour window", Unit: "percent", Scope: "account", WindowMinutes: 300, UsedPercent: f(10), RemainingPercent: f(90), ObservationState: "observed"}}},
	}
	pools := MergeAccountPools("Claude DAK", accounts)
	if len(pools) != 1 || len(pools[0].Buckets) != 1 || pools[0].Buckets[0].RemainingPercent == nil || *pools[0].Buckets[0].RemainingPercent != 47 || pools[0].Buckets[0].Total != 0 {
		t.Fatalf("fabricated pooled capacity: %+v", pools)
	}
	raw, _ := json.Marshal(pools)
	if strings.Contains(string(raw), "@") || strings.Contains(string(raw), "secret-") {
		t.Fatal("account identity leaked")
	}
	accounts = append(accounts, ConnectionQuota{Source: "sdk", Provider: "claude", Status: "ok", ObservationState: "waiting"})
	pools = MergeAccountPools("Claude DAK", accounts)
	if !pools[0].Buckets[0].Incomplete || pools[0].Buckets[0].Warning {
		t.Fatal("unknown account treated as complete fresh capacity")
	}
}
