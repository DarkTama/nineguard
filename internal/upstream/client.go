package upstream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"nineguard/internal/providers"
)

const (
	DefaultCacheTTL      = 60 * time.Second
	DefaultForceCooldown = 30 * time.Second
)

type cachedQuota struct {
	result    *ProviderQuotaResult
	expiresAt time.Time
}

// Manager orchestrates quota fetches across various upstream provider protocols.
type Manager struct {
	router9       *Router9Adapter
	nineguard     *NineGuardAdapter
	instanceID    string
	lastForce     map[string]time.Time
	forceCooldown time.Duration

	cacheMu sync.RWMutex
	cache   map[string]cachedQuota // providerID -> cachedQuota
	ttl     time.Duration
}

func NewManager() *Manager {
	return &Manager{
		router9:       NewRouter9Adapter(),
		nineguard:     NewNineGuardAdapter(),
		lastForce:     make(map[string]time.Time),
		forceCooldown: DefaultForceCooldown,
		cache:         make(map[string]cachedQuota),
		ttl:           DefaultCacheTTL,
	}
}

// SetForceCooldown allows overriding the cooldown for tests.
func (m *Manager) SetForceCooldown(d time.Duration) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	m.forceCooldown = d
}

// SetInstanceID configures instance UUID for loop protection.
func (m *Manager) SetInstanceID(id string) {
	m.instanceID = id
	if m.nineguard != nil {
		m.nineguard.SetInstanceID(id)
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
	m.cacheMu.RLock()
	cached, hasCache := m.cache[p.ID]
	lastForceTime := m.lastForce[p.ID]
	m.cacheMu.RUnlock()

	// If force requested but 30s cooldown hasn't passed, serve valid cache silently
	if force && hasCache && time.Since(lastForceTime) < m.forceCooldown {
		if time.Now().Before(cached.expiresAt) {
			return cached.result, nil
		}
	}

	if !force && hasCache && time.Now().Before(cached.expiresAt) {
		return cached.result, nil
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
		if force {
			m.lastForce[p.ID] = time.Now()
		}
		m.cacheMu.Unlock()
	}

	return res, err
}

// ConsumeReset delegates to router9 adapter and invalidates provider cache.
func (m *Manager) ConsumeReset(ctx context.Context, p providers.Provider, connectionID, providerKind, grantID string) error {
	if p.QuotaType != "9router" {
		return fmt.Errorf("quota resets are only supported for 9router providers")
	}
	err := m.router9.ConsumeReset(ctx, p, connectionID, providerKind, grantID)
	if err == nil {
		m.InvalidateCache(p.ID)
	}
	return err
}
