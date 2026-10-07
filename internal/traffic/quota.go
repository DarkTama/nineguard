package traffic

import (
	"database/sql"
	"fmt"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/timeutil"
)

// CalcQuotaWindow returns the UTC start instant and the next reset instant for a given quota period.
func CalcQuotaWindow(period string, now time.Time) (start time.Time, resetAt time.Time) {
	now = now.UTC()
	switch period {
	case "daily":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		resetAt = start.AddDate(0, 0, 1)
	case "weekly":
		weekday := int(now.Weekday())
		if weekday == 0 { // Sunday in Go is 0, ISO Monday is 1
			weekday = 7
		}
		daysSinceMonday := weekday - 1
		start = time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.UTC)
		resetAt = start.AddDate(0, 0, 7)
	case "monthly":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		resetAt = start.AddDate(0, 1, 0)
	case "total":
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		resetAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	default:
		start = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
		resetAt = now
	}
	return start, resetAt
}

// GetQuotaUsage queries cumulative tokens consumed by apiKeyID within its active quota window.
func GetQuotaUsage(d *db.DB, apiKeyID string, period string, now time.Time) (int64, time.Time, error) {
	if period == "none" || period == "" {
		return 0, time.Time{}, nil
	}
	start, resetAt := CalcQuotaWindow(period, now)
	startStr := start.Format(timeutil.SQLiteLayout)

	query := `SELECT COALESCE(SUM(total_tokens), 0) FROM traffic_logs WHERE api_key_id = ? AND timestamp >= ?`
	var consumed int64
	err := d.QueryRow(query, apiKeyID, startStr).Scan(&consumed)
	if err != nil && err != sql.ErrNoRows {
		return 0, resetAt, fmt.Errorf("query quota usage: %w", err)
	}
	return consumed, resetAt, nil
}
