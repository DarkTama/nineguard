package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/providers"
	"nineguard/internal/upstream"
)

// GetUpstreamQuotas returns diagnostic quota information for all configured providers (Admin API).
func (h *Handler) GetUpstreamQuotas(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		jsonError(w, http.StatusBadRequest, "Providers manager not available")
		return
	}
	if h.upstream == nil {
		h.upstream = upstream.NewManager()
	}

	force := r.URL.Query().Get("force") == "1"
	provList, err := h.providers.ListProviders()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list providers")
		return
	}

	type provResult struct {
		res *upstream.ProviderQuotaResult
		err error
	}

	resultsChan := make(chan provResult, len(provList))
	var wg sync.WaitGroup

	for _, p := range provList {
		wg.Add(1)
		go func(provCopy providers.Provider) {
			defer wg.Done()
			res, fetchErr := h.upstream.GetProviderQuotas(r.Context(), provCopy, force)
			resultsChan <- provResult{res: res, err: fetchErr}
		}(p)
	}

	wg.Wait()
	close(resultsChan)

	results := make([]*upstream.ProviderQuotaResult, 0, len(provList))
	for pr := range resultsChan {
		if pr.res != nil {
			results = append(results, pr.res)
		}
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"providers": results,
	})
}

// BuildUnifiedQuotaResponse constructs client-facing merged quota information filtered by key permissions.
func (h *Handler) BuildUnifiedQuotaResponse(ctx context.Context, keyInfo *keys.KeyInfo, force bool) (*upstream.UnifiedQuotaResponse, error) {
	if h.upstream == nil {
		h.upstream = upstream.NewManager()
	}

	resp := &upstream.UnifiedQuotaResponse{
		UpstreamPools: make([]upstream.MergedPoolQuota, 0),
	}

	now := time.Now()

	// 1. Client key budget
	if keyInfo != nil {
		if !keyInfo.EnforcesModelQuotas() && keyInfo.QuotaLimit > 0 && keyInfo.QuotaPeriod != "" && keyInfo.QuotaPeriod != "none" {
			if h.traffic != nil {
				consumed, resetAt, err := h.traffic.GetQuotaUsage(keyInfo.ID, keyInfo.QuotaPeriod, now)
				if err == nil {
					remaining := keyInfo.QuotaLimit - consumed
					if remaining < 0 {
						remaining = 0
					}
					var resetTime *time.Time
					if !resetAt.IsZero() {
						resetTime = &resetAt
					}
					resp.KeyQuota = &upstream.ClientKeyBudget{
						Mode:      "key",
						Limit:     keyInfo.QuotaLimit,
						Used:      consumed,
						Remaining: remaining,
						Period:    keyInfo.QuotaPeriod,
						ResetAt:   resetTime,
						Warning:   consumed >= int64(float64(keyInfo.QuotaLimit)*0.8),
					}
				}
			}
		}
	}

	// 2. Fetch active upstream providers and merge pools
	if h.providers != nil {
		provList, err := h.providers.ListProviders()
		if err == nil {
			for _, prov := range provList {
				if !prov.IsActive || prov.QuotaType == "disabled" || prov.QuotaType == "" {
					continue
				}

				res, fetchErr := h.upstream.GetProviderQuotas(ctx, prov, force)
				if fetchErr == nil && res != nil && len(res.Connections) > 0 {
					merged := upstream.MergeAccountPools(prov.Name, res.Connections)
					if merged != nil {
						resp.UpstreamPools = append(resp.UpstreamPools, *merged)
					}
				}
			}
		}
	}

	// 3. Filter pools by model access if key has restrictions
	if keyInfo != nil && !keyInfo.IsAllModelsAllowed() {
		resp.UpstreamPools = upstream.FilterPoolsByModelAccess(resp.UpstreamPools, keyInfo.AllowedModels)
	}

	return resp, nil
}

func (h *Handler) SetUpstreamManager(um *upstream.Manager) {
	h.upstream = um
}
