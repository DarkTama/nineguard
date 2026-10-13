package upstream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"nineguard/internal/providers"
)

const DefaultCacheTTL = 60 * time.Second

type cachedQuota struct {
	result    *ProviderQuotaResult
	expiresAt time.Time
}

// Manager orchestrates quota fetches across various upstream provider protocols.
type Manager struct {
	router9   *Router9Adapter
	nineguard *NineGuardAdapter

	cacheMu sync.RWMutex
	cache   map[string]cachedQuota // providerID -> cachedQuota
	ttl     time.Duration
}

func NewManager() *Manager {
	return &Manager{
		router9:   NewRouter9Adapter(),
		nineguard: NewNineGuardAdapter(),
		cache:     make(map[string]cachedQuota),
		ttl:       DefaultCacheTTL,
	}
}

// SetCacheTTL allows overriding cache duration (useful for tests).
func (m *Manager) SetCacheTTL(ttl time.Duration) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	m.ttl = ttl
}

// InvalidateCache clears the cached quota for a specific provider.
func (m *Manager) InvalidateCache(providerID string) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	delete(m.cache, providerID)
}

// GetProviderQuotas retrieves quotas for a provider, using cache unless force=true or expired.
func (m *Manager) GetProviderQuotas(ctx context.Context, p providers.Provider, force bool) (*ProviderQuotaResult, error) {
	if p.QuotaType == "disabled" || p.QuotaType == "" {
		return &ProviderQuotaResult{
			ProviderID:   p.ID,
			ProviderName: p.Name,
			QuotaType:    "disabled",
			Status:       "unsupported",
			LastUpdated:  time.Now(),
			Connections:  make([]ConnectionQuota, 0),
		}, nil
	}

	// Check cache
	if !force {
		m.cacheMu.RLock()
		cached, ok := m.cache[p.ID]
		m.cacheMu.RUnlock()
		if ok && time.Now().Before(cached.expiresAt) {
			return cached.result, nil
		}
	}

	var res *ProviderQuotaResult
	var err error

	switch p.QuotaType {
	case "9router":
		res, err = m.router9.FetchQuotas(ctx, p, force)
	case "nineguard":
		res, err = m.nineguard.FetchQuotas(ctx, p, force)
	default:
		return nil, fmt.Errorf("unsupported quota type: %s", p.QuotaType)
	}

	if err != nil && res == nil {
		res = &ProviderQuotaResult{
			ProviderID:   p.ID,
			ProviderName: p.Name,
			QuotaType:    p.QuotaType,
			Status:       "error",
			ErrorMsg:     err.Error(),
			LastUpdated:  time.Now(),
			Connections:  make([]ConnectionQuota, 0),
		}
	}

	// Update cache
	if res != nil {
		m.cacheMu.Lock()
		m.cache[p.ID] = cachedQuota{
			result:    res,
			expiresAt: time.Now().Add(m.ttl),
		}
		m.cacheMu.Unlock()
	}

	return res, err
}
