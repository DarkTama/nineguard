package upstream

import (
	"fmt"
	"sort"
	"time"
)

func mergeSDKPools(providerName string, connections []ConnectionQuota) []MergedPoolQuota {
	type group struct {
		family   string
		accounts []ConnectionQuota
	}
	groups := map[string]*group{}
	order := []string{}
	for _, c := range connections {
		if c.Status == "disabled" {
			continue
		}
		family, _ := resolveSubProvider(c)
		g := groups[family]
		if g == nil {
			g = &group{family: family}
			groups[family] = g
			order = append(order, family)
		}
		g.accounts = append(g.accounts, c)
	}
	out := []MergedPoolQuota{}
	for _, key := range order {
		g := groups[key]
		pool := MergedPoolQuota{Provider: providerName, SubProvider: key, Family: providerName, Source: "sdk", Status: "ok", ObservationState: "waiting", Buckets: []QuotaBucket{}}
		type aggregate struct {
			bucket QuotaBucket
			seen   int
		}
		values := map[string]*aggregate{}
		eligible := 0
		for _, c := range g.accounts {
			if c.Status != "ok" {
				pool.Status = "degraded"
				continue
			}
			eligible++
			seen := map[string]bool{}
			for _, b := range c.Buckets {
				if b.Unit != "percent" || b.Scope != "account" {
					continue
				}
				id := fmt.Sprintf("%s|%d", b.Name, b.WindowMinutes)
				if seen[id] {
					continue
				}
				seen[id] = true
				a := values[id]
				if a == nil {
					copy := b
					copy.Warning = false
					copy.Incomplete = false
					a = &aggregate{bucket: copy}
					values[id] = a
				}
				if b.RemainingPercent != nil {
					a.seen++
				}
				if b.RemainingPercent != nil && (a.bucket.RemainingPercent == nil || *b.RemainingPercent < *a.bucket.RemainingPercent) {
					v := *b.RemainingPercent
					a.bucket.RemainingPercent = &v
				}
				if b.UsedPercent != nil && (a.bucket.UsedPercent == nil || *b.UsedPercent > *a.bucket.UsedPercent) {
					v := *b.UsedPercent
					a.bucket.UsedPercent = &v
				}
				if b.ResetAt != nil && (a.bucket.ResetAt == nil || b.ResetAt.Before(*a.bucket.ResetAt)) {
					t := *b.ResetAt
					a.bucket.ResetAt = &t
				}
				if b.ObservedAt != nil && (a.bucket.ObservedAt == nil || b.ObservedAt.Before(*a.bucket.ObservedAt)) {
					t := *b.ObservedAt
					a.bucket.ObservedAt = &t
				}
				if b.ObservationState == "stale" {
					a.bucket.ObservationState = "stale"
				}
				a.bucket.LimitReached = a.bucket.LimitReached || b.LimitReached
			}
		}
		ids := []string{}
		for id := range values {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			a := values[id]
			b := a.bucket
			b.Incomplete = a.seen < eligible
			if b.RemainingPercent == nil {
				b.ObservationState = "waiting"
			}
			b.Warning = !b.Incomplete && b.ObservationState == "observed" && b.RemainingPercent != nil && *b.RemainingPercent < 25
			pool.Warning = pool.Warning || b.Warning
			if b.ObservedAt != nil && (pool.ObservedAt == nil || b.ObservedAt.Before(*pool.ObservedAt)) {
				t := *b.ObservedAt
				pool.ObservedAt = &t
			}
			if b.ObservationState == "stale" {
				pool.ObservationState = "stale"
			} else if pool.ObservationState == "waiting" {
				pool.ObservationState = b.ObservationState
			}
			pool.Buckets = append(pool.Buckets, b)
		}
		out = append(out, pool)
	}
	return out
}

// PercentBucketFresh is used by alerts; stale and partial snapshots never manufacture a new warning.
func PercentBucketFresh(b QuotaBucket, now time.Time) bool {
	return b.Unit == "percent" && b.RemainingPercent != nil && !b.Incomplete && b.ObservationState == "observed" && b.ObservedAt != nil && now.Sub(*b.ObservedAt) <= 5*time.Minute && (b.ResetAt == nil || now.Before(*b.ResetAt))
}
