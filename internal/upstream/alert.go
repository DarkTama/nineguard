package upstream

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// QuotaAlerter tracks upstream quota warnings and connection errors, emitting
// edge-triggered logs via slog when thresholds are crossed or recovered.
type QuotaAlerter struct {
	mu           sync.Mutex
	bucketWarned map[string]bool // key: providerID|connectionID|bucketName -> warned
	connError    map[string]bool // key: providerID|connectionID -> inError
}

// NewQuotaAlerter creates a new state-based, edge-triggered upstream quota alerter.
func NewQuotaAlerter() *QuotaAlerter {
	return &QuotaAlerter{
		bucketWarned: make(map[string]bool),
		connError:    make(map[string]bool),
	}
}

// Observe inspects a ProviderQuotaResult and emits slog events on edge transitions.
// Returns the list of emitted alert message strings (useful for tests).
func (a *QuotaAlerter) Observe(res *ProviderQuotaResult, now time.Time) []string {
	if a == nil || res == nil || res.Status == "unsupported" || res.QuotaType == "disabled" {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	provName := res.ProviderName
	if provName == "" {
		provName = res.ProviderID
	}

	var emitted []string

	// Provider-level error when no connections exist
	if len(res.Connections) == 0 {
		cKey := fmt.Sprintf("%s|", res.ProviderID)
		inError := a.connError[cKey]
		if res.Status == "error" {
			if !inError {
				msg := "upstream_connection_error: upstream connection error"
				slog.Warn(msg,
					"source", "upstream",
					"provider", provName,
					"account", "",
					"connection_id", "",
					"error", res.ErrorMsg,
				)
				emitted = append(emitted, msg)
				a.connError[cKey] = true
			}
		} else if res.Status == "ok" {
			if inError {
				msg := "upstream_connection_recovered: upstream connection recovered"
				slog.Info(msg,
					"source", "upstream",
					"provider", provName,
					"account", "",
					"connection_id", "",
				)
				emitted = append(emitted, msg)
				a.connError[cKey] = false
			}
		}
		return emitted
	}

	for _, conn := range res.Connections {
		cKey := fmt.Sprintf("%s|%s", res.ProviderID, conn.ConnectionID)
		inError := a.connError[cKey]

		if conn.Status == "error" {
			if !inError {
				msg := "upstream_connection_error: upstream connection error"
				slog.Warn(msg,
					"source", "upstream",
					"provider", provName,
					"account", conn.AccountLabel,
					"connection_id", conn.ConnectionID,
					"error", conn.ErrorMsg,
				)
				emitted = append(emitted, msg)
				a.connError[cKey] = true
			}
		} else if conn.Status == "ok" {
			if inError {
				msg := "upstream_connection_recovered: upstream connection recovered"
				slog.Info(msg,
					"source", "upstream",
					"provider", provName,
					"account", conn.AccountLabel,
					"connection_id", conn.ConnectionID,
				)
				emitted = append(emitted, msg)
				a.connError[cKey] = false
			}
		}
		// Notice: "degraded" is treated as not-error, no alert emitted.

		for _, b := range conn.Buckets {
			if b.Unit == "percent" && !PercentBucketFresh(b, now) { continue }
			bKey := fmt.Sprintf("%s|%s|%s", res.ProviderID, conn.ConnectionID, b.Name)
			if b.Unit == "percent" { bKey = fmt.Sprintf("%s|%s|%s|%s|%d", res.ProviderID, conn.ConnectionID, b.Name, b.Scope, b.WindowMinutes) }
			wasWarned, exists := a.bucketWarned[bKey]

			var resetAtStr string
			if b.ResetAt != nil {
				resetAtStr = b.ResetAt.Format(time.RFC3339)
			}
			percentage := b.RemainingPercentage
			if b.Unit == "percent" && b.RemainingPercent != nil { percentage = int(*b.RemainingPercent) }

			if b.Warning {
				// Alert if first seen warning (!exists) or transitioning false -> true (!wasWarned)
				if !exists || !wasWarned {
					var msg string
					if b.IsCreditBalance {
						msg = "upstream_quota_low: upstream credit balance exhausted"
					} else {
						msg = "upstream_quota_low: upstream quota running low"
					}
					slog.Warn(msg,
						"source", "upstream",
						"provider", provName,
						"account", conn.AccountLabel,
						"bucket", b.Name,
						"remaining_percentage", percentage,
						"reset_at", resetAtStr,
					)
					emitted = append(emitted, msg)
					a.bucketWarned[bKey] = true
				}
			} else {
				// Recover if previously warned (exists && wasWarned)
				if exists && wasWarned {
					var msg string
					if b.IsCreditBalance {
						msg = "upstream_quota_recovered: upstream credit balance restored"
					} else {
						msg = "upstream_quota_recovered: upstream quota recovered"
					}
					slog.Info(msg,
						"source", "upstream",
						"provider", provName,
						"account", conn.AccountLabel,
						"bucket", b.Name,
						"remaining_percentage", percentage,
						"reset_at", resetAtStr,
					)
					emitted = append(emitted, msg)
					a.bucketWarned[bKey] = false
				} else if !exists {
					a.bucketWarned[bKey] = false
				}
			}
		}
	}

	return emitted
}
