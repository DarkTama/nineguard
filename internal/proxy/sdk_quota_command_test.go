package proxy

import (
	"nineguard/internal/upstream"
	"strings"
	"testing"
)

func TestSDKQuotaCommandUsesPercentUnits(t *testing.T) {
	used, left := 53.0, 47.0
	text := FormatQuotaMarkdown(&upstream.UnifiedQuotaResponse{UpstreamPools: []upstream.MergedPoolQuota{{Provider: "Claude SDK", Family: "Claude SDK", Source: "sdk", Buckets: []upstream.QuotaBucket{{Name: "5-hour window", Unit: "percent", UsedPercent: &used, RemainingPercent: &left, ObservationState: "observed"}}}}}, "")
	if !strings.Contains(text, "53.0% used") || !strings.Contains(text, "47.0% left") || strings.Contains(text, "0 / 0") {
		t.Fatal(text)
	}
	waiting := FormatQuotaMarkdown(&upstream.UnifiedQuotaResponse{UpstreamPools: []upstream.MergedPoolQuota{{Provider: "Claude SDK", Source: "sdk", ObservationState: "waiting"}}}, "")
	if !strings.Contains(waiting, "waiting for upstream telemetry") {
		t.Fatal(waiting)
	}
}
