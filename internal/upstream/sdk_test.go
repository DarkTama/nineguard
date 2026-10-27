package upstream

import (
	"context"
	"nineguard/internal/providers"
	"testing"
	"time"
)

type quotaSnapshotSource struct {
	observed time.Time
	calls    int
}

func (s *quotaSnapshotSource) SDKQuotas(_ context.Context, p providers.Provider, _ time.Time) (*ProviderQuotaResult, error) {
	s.calls++
	return &ProviderQuotaResult{ProviderID: p.ID, QuotaType: "sdk", Source: "sdk", Status: "ok", ObservedAt: &s.observed}, nil
}
func TestSDKQuotaOverridesDisabledTracker(t *testing.T) {
	m := NewManager()
	source := &quotaSnapshotSource{observed: time.Unix(1800000000, 0)}
	m.SetSDKQuotaSource(source)
	p := providers.Provider{ID: "sdk", Backend: "cliproxy", Family: "claude", QuotaType: "disabled"}
	for _, force := range []bool{false, true} {
		res, err := m.GetProviderQuotas(context.Background(), p, force)
		if err != nil || res.QuotaType != "sdk" || res.Status == "unsupported" || !res.ObservedAt.Equal(source.observed) {
			t.Fatalf("SDK dispatch: %+v %v", res, err)
		}
	}
	if source.calls != 2 {
		t.Fatal("SDK observation cached as a fresh measurement")
	}
	p.Backend = "http"
	res, err := m.GetProviderQuotas(context.Background(), p, false)
	if err != nil || res.Status != "unsupported" {
		t.Fatal("HTTP disabled behavior changed")
	}
}
