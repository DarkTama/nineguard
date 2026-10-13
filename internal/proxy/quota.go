package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// quotaExceededBody builds the 429 JSON for a Key Quota (modelID == "") or a Model Quota
// (ADR 0005, ADR 0007). retryAfter is 0 for a lifetime ("total") quota, which never resets:
// the caller must then omit the Retry-After header.
func quotaExceededBody(modelID string, consumed, limit int64, period string, resetAt, now time.Time) (body []byte, retryAfter int) {
	prefix := "API key token quota exceeded"
	code := "quota_exceeded"
	if modelID != "" {
		prefix = fmt.Sprintf("Model quota exceeded for '%s'", modelID)
		code = "model_quota_exceeded"
	}

	var msg string
	if period == "total" {
		msg = fmt.Sprintf("%s (%s / %s tokens total). Lifetime quota, does not reset.", prefix, formatTokenCount(consumed), formatTokenCount(limit))
	} else {
		until := resetAt.Sub(now)
		hours := int(until.Hours())
		mins := int(until.Minutes()) % 60
		resetDesc := fmt.Sprintf("%dh %dm", hours, mins)
		if hours <= 0 {
			resetDesc = fmt.Sprintf("%dm", mins)
		}

		wibLoc, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			wibLoc = time.FixedZone("WIB", 7*3600)
		}
		wibTime := resetAt.In(wibLoc).Format("15:04 WIB")
		utcTime := resetAt.UTC().Format("15:04 UTC")
		atDisplay := fmt.Sprintf("%s / %s", wibTime, utcTime)
		if period == "weekly" || period == "monthly" {
			atDisplay = fmt.Sprintf("%s on %s / %s", wibTime, resetAt.In(wibLoc).Format("02 Jan"), utcTime)
		}
		msg = fmt.Sprintf("%s (%s / %s tokens %s). Resets in %s (at %s).", prefix, formatTokenCount(consumed), formatTokenCount(limit), period, resetDesc, atDisplay)

		retryAfter = int(until.Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
	}

	errObj := map[string]any{"message": msg, "type": "insufficient_quota", "code": code}
	if modelID != "" {
		errObj["param"] = "model"
	}
	body, _ = json.MarshalIndent(map[string]any{"error": errObj}, "", "\t")
	return body, retryAfter
}

// writeQuotaExceeded sends the 429 and returns its message for the traffic log.
func writeQuotaExceeded(w http.ResponseWriter, modelID string, consumed, limit int64, period string, resetAt time.Time) {
	body, retryAfter := quotaExceededBody(modelID, consumed, limit, period, resetAt, time.Now())
	w.Header().Set("Content-Type", "application/json")
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write(body)
}
