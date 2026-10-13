package upstream

import (
	"math"
	"strings"
	"time"
)

// MergeAccountPools aggregates multiple connection quotas from the same provider into
// a single privacy-preserving MergedPoolQuota. Account IDs, counts, and personal emails
// are completely omitted.
func MergeAccountPools(providerName string, connections []ConnectionQuota) *MergedPoolQuota {
	if len(connections) == 0 {
		return nil
	}

	merged := &MergedPoolQuota{
		Provider: providerName,
		Family:   providerName,
		Status:   "ok",
		Buckets:  make([]QuotaBucket, 0),
	}

	type bucketAgg struct {
		used            int64
		total           int64
		remaining       int64
		isCredit        bool
		hasTotal        bool
		earliestReset   *time.Time
		order           int
	}

	bucketMap := make(map[string]*bucketAgg)
	orderedKeys := make([]string, 0)
	hasAnyDegraded := false
	hasAnyError := false

	for _, conn := range connections {
		if conn.Status == "degraded" {
			hasAnyDegraded = true
		} else if conn.Status == "error" {
			hasAnyError = true
		}

		for _, b := range conn.Buckets {
			name := strings.TrimSpace(b.Name)
			if name == "" {
				continue
			}

			agg, exists := bucketMap[name]
			if !exists {
				agg = &bucketAgg{
					isCredit: b.IsCreditBalance,
					order:    len(orderedKeys),
				}
				bucketMap[name] = agg
				orderedKeys = append(orderedKeys, name)
			}

			agg.used += b.Used
			agg.total += b.Total
			agg.remaining += b.Remaining
			if b.Total > 0 {
				agg.hasTotal = true
			}

			if b.ResetAt != nil {
				if agg.earliestReset == nil || b.ResetAt.Before(*agg.earliestReset) {
					tCopy := *b.ResetAt
					agg.earliestReset = &tCopy
				}
			}
		}
	}

	if hasAnyError {
		merged.Status = "error"
	} else if hasAnyDegraded {
		merged.Status = "degraded"
	}

	// Build final merged buckets
	for _, name := range orderedKeys {
		agg := bucketMap[name]
		var pct int
		if agg.hasTotal && agg.total > 0 {
			remaining := agg.total - agg.used
			if remaining < 0 {
				remaining = 0
			}
			agg.remaining = remaining
			pct = int(math.Round((float64(remaining) / float64(agg.total)) * 100))
			if pct < 0 {
				pct = 0
			} else if pct > 100 {
				pct = 100
			}
		} else if agg.isCredit {
			pct = 100
		}

		warning := (pct < 25 && agg.hasTotal) || (agg.isCredit && agg.remaining <= 0)
		if warning {
			merged.Warning = true
		}

		merged.Buckets = append(merged.Buckets, QuotaBucket{
			Name:                name,
			Used:                agg.used,
			Total:               agg.total,
			Remaining:           agg.remaining,
			RemainingPercentage: pct,
			ResetAt:             agg.earliestReset,
			IsCreditBalance:     agg.isCredit,
			Warning:             warning,
		})
	}

	return merged
}

// FilterPoolsByModelAccess filters merged pools according to the client key's allowed models.
func FilterPoolsByModelAccess(pools []MergedPoolQuota, allowedModels []string) []MergedPoolQuota {
	if len(allowedModels) == 0 {
		return pools
	}

	// Check if wildcard "*" is in allowed models
	for _, m := range allowedModels {
		if m == "*" {
			return pools
		}
	}

	filtered := make([]MergedPoolQuota, 0, len(pools))
	for _, pool := range pools {
		if len(pool.AllowedModels) == 0 {
			// If pool doesn't declare model restrictions, keep it
			filtered = append(filtered, pool)
			continue
		}

		matches := false
		for _, pm := range pool.AllowedModels {
			for _, am := range allowedModels {
				if strings.EqualFold(pm, am) || strings.HasPrefix(strings.ToLower(am), strings.ToLower(pm)) {
					matches = true
					break
				}
			}
			if matches {
				break
			}
		}

		if matches {
			filtered = append(filtered, pool)
		}
	}

	return filtered
}
