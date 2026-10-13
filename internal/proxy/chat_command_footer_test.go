package proxy_test

import (
	"strings"
	"testing"
	"time"

	"nineguard/internal/proxy"
	"nineguard/internal/upstream"
)

func quotaRespWithPool(syncInterval string) *upstream.UnifiedQuotaResponse {
	now := time.Now().Add(-2 * time.Minute)
	return &upstream.UnifiedQuotaResponse{
		UpstreamPools: []upstream.MergedPoolQuota{{
			Provider: "9router",
			Family:   "Gemini",
			Buckets: []upstream.QuotaBucket{
				{Name: "Gemini (5h)", Used: 130, Total: 1000, Remaining: 870, RemainingPercentage: 87},
			},
		}},
		LastUpdated:  &now,
		SyncInterval: syncInterval,
	}
}

func TestFormatQuotaMarkdown_FooterUsesConfiguredInterval(t *testing.T) {
	md := proxy.FormatQuotaMarkdown(quotaRespWithPool("15m"), "")
	if !strings.Contains(md, "Auto-refreshes every 15m") {
		t.Fatalf("footer should show configured interval, got:\n%s", md)
	}
	if strings.Contains(md, "every 5m") {
		t.Fatalf("footer must not show hardcoded 5m, got:\n%s", md)
	}
}

func TestFormatQuotaMarkdown_FooterFallsBackToDefault(t *testing.T) {
	md := proxy.FormatQuotaMarkdown(quotaRespWithPool(""), "")
	if !strings.Contains(md, "Auto-refreshes every 10m") {
		t.Fatalf("footer should fall back to 10m default, got:\n%s", md)
	}
}
