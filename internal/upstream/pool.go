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

func isBucketAllowed(name string, allowedModels []string) bool {
	nLower := strings.ToLower(name)
	if strings.Contains(nLower, "credit") || strings.Contains(nLower, "balance") {
		return true
	}

	// Specific variant checks: if bucket names a specific variant, user must have access to it
	specificVariants := []string{"opus", "sonnet", "haiku", "gpt-oss", "oss", "image"}
	for _, v := range specificVariants {
		if strings.Contains(nLower, v) {
			hasVariant := false
			for _, am := range allowedModels {
				if strings.Contains(strings.ToLower(am), v) {
					hasVariant = true
					break
				}
			}
			if !hasVariant {
				return false
			}
		}
	}

	// Family matching: gemini, claude, gpt, deepseek, etc.
	families := []string{"gemini", "claude", "gpt", "deepseek", "qwen", "mistral", "llama", "codex", "copilot", "kiro"}
	hasAnyFamilyInBucket := false
	for _, fam := range families {
		if strings.Contains(nLower, fam) {
			hasAnyFamilyInBucket = true
			for _, am := range allowedModels {
				if strings.Contains(strings.ToLower(am), fam) {
					return true
				}
			}
		}
	}

	if hasAnyFamilyInBucket {
		return false
	}

	// Unnamed generic windows (e.g. "5h Session") keep if provider is available
	return true
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
		// 1. If pool specifies AllowedModels, check provider/model level
		if len(pool.AllowedModels) > 0 {
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
			if !matches {
				continue
			}
		}

		// 2. Filter individual buckets inside the pool
		if len(pool.Buckets) > 0 {
			var allowedBuckets []QuotaBucket
			for _, b := range pool.Buckets {
				if isBucketAllowed(b.Name, allowedModels) {
					allowedBuckets = append(allowedBuckets, b)
				}
			}

			if len(allowedBuckets) > 0 {
				poolCopy := pool
				poolCopy.Buckets = allowedBuckets
				filtered = append(filtered, poolCopy)
			}
		} else {
			filtered = append(filtered, pool)
		}
	}

	return filtered
}
