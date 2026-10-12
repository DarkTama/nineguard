package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nineguard/internal/keys"
)

// validateModelQuotas normalizes the submitted Model Quota rows and checks them (ADR 0007).
// stored holds the model ids already saved on this key (lower-cased): those rows are accepted
// as they are, even when they are inactive now, so editing a key never forces deleting an
// orphan. New rows must name a known, enabled, present Model, use its canonical id and be
// allowed by access (the access settings from the same payload).
func (h *Handler) validateModelQuotas(rows []keys.ModelQuota, stored map[string]bool, access *keys.KeyInfo) ([]keys.ModelQuota, int, error) {
	out := make([]keys.ModelQuota, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		id := strings.TrimSpace(r.ModelID)
		if id == "" {
			return nil, http.StatusBadRequest, fmt.Errorf("model quota without model_id")
		}
		if r.QuotaLimit <= 0 {
			return nil, http.StatusBadRequest, fmt.Errorf("model quota for '%s': limit must be greater than 0", id)
		}
		period := strings.ToLower(strings.TrimSpace(r.QuotaPeriod))
		if !keys.IsModelQuotaPeriod(period) {
			return nil, http.StatusBadRequest, fmt.Errorf("model quota for '%s': period must be daily, weekly, monthly or total", id)
		}
		lid := strings.ToLower(id)
		if seen[lid] {
			return nil, http.StatusBadRequest, fmt.Errorf("duplicate model quota for '%s'", id)
		}
		seen[lid] = true

		if !stored[lid] {
			if h.models == nil || h.providers == nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("Model catalog not available")
			}
			found, enabled, removed := h.models.Status(id)
			switch {
			case !found:
				return nil, http.StatusBadRequest, fmt.Errorf("unknown model '%s'", id)
			case removed:
				return nil, http.StatusBadRequest, fmt.Errorf("model '%s' was removed from its provider", id)
			case !enabled:
				return nil, http.StatusBadRequest, fmt.Errorf("model '%s' is disabled", id)
			case !h.providers.IsCanonicalModelID(id):
				return nil, http.StatusBadRequest, fmt.Errorf("use the full model id with its provider prefix (e.g. prefix/%s)", id)
			case !access.IsModelAllowed(id):
				return nil, http.StatusBadRequest, fmt.Errorf("model '%s' is not allowed for this key", id)
			}
		}
		out = append(out, keys.ModelQuota{ModelID: id, QuotaLimit: r.QuotaLimit, QuotaPeriod: period})
	}
	return out, http.StatusOK, nil
}

// GetKey returns one key with its full Model Quotas (list responses carry a summary only).
func (h *Handler) GetKey(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil {
		jsonError(w, http.StatusBadRequest, "Keys manager not available")
		return
	}
	k, err := h.keys.GetKey(r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusNotFound, "API key not found")
		return
	}
	jsonResponse(w, http.StatusOK, k)
}

// GetQuotas serves the Quotas page and the dashboard card.
func (h *Handler) GetQuotas(w http.ResponseWriter, r *http.Request) {
	if h.keys == nil || h.traffic == nil {
		jsonResponse(w, http.StatusOK, keys.QuotaOverview{WarnPercent: 80, Keys: []keys.OverviewKey{}})
		return
	}
	ov, err := h.keys.QuotaOverview(h.traffic.GetQuotaWarnPercent(), time.Now())
	if err != nil {
		slog.Error("failed to build quota overview", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve quotas")
		return
	}
	jsonResponse(w, http.StatusOK, ov)
}

// GetKeyModelVelocity returns approximate usage history for one key and Model (ADR 0008).
func (h *Handler) GetKeyModelVelocity(w http.ResponseWriter, r *http.Request) {
	if h.traffic == nil {
		jsonError(w, http.StatusBadRequest, "Traffic manager not available")
		return
	}
	keyID := r.PathValue("id")
	modelID := strings.TrimSpace(r.URL.Query().Get("model"))
	if keyID == "" || modelID == "" {
		jsonError(w, http.StatusBadRequest, "key id and model are required")
		return
	}
	// Requests that left the provider prefix off were logged with the bare name when the
	// prefix belongs to the default provider; match those older rows too.
	bare := ""
	if h.providers != nil {
		if i := strings.Index(modelID, "/"); i >= 0 && i < len(modelID)-1 {
			cand := modelID[i+1:]
			if _, _, resolved, err := h.providers.ResolveModelID(cand); err == nil && strings.EqualFold(resolved, modelID) {
				bare = cand
			}
		}
	}
	stats, err := h.traffic.GetModelVelocityStats(keyID, modelID, bare, time.Now())
	if err != nil {
		slog.Error("failed to get model velocity", "key", keyID, "model", modelID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to retrieve velocity stats")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"tokens_24h":    stats.Tokens24h,
		"tokens_7d_avg": stats.Tokens7dAvg,
		"tokens_30d":    stats.Tokens30d,
		"approximate":   true,
	})
}
