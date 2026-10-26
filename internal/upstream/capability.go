package upstream

import (
	"context"
	"nineguard/internal/providers"
	"time"
)

// SDKQuotaSource supplies read-only observations from the embedded engine, never a remote gateway.
type SDKQuotaSource interface {
	SDKQuotas(context.Context, providers.Provider, time.Time) (*ProviderQuotaResult, error)
}

func EffectiveQuotaType(p providers.Provider) string {
	if p.Backend == "cliproxy" {
		return "sdk"
	}
	return p.QuotaType
}
func SupportsQuotaTracking(p providers.Provider) bool {
	kind := EffectiveQuotaType(p)
	return kind != "" && kind != "disabled"
}
func (m *Manager) SetSDKQuotaSource(source SDKQuotaSource) { m.sdk = source }
