package traffic

import (
	"strings"
	"testing"
	"time"
)

// insertAt writes a traffic row with an explicit UTC timestamp.
func insertAt(t *testing.T, mgr *Manager, ts, keyName, model string, status, tokens int) {
	t.Helper()
	_, err := mgr.db.Exec(`
		INSERT INTO traffic_logs (timestamp, api_key, api_key_name, model, total_tokens, status_code, client_ip, level)
		VALUES (?, 'sk-ng-...abcd', ?, ?, ?, ?, '127.0.0.1', '')`,
		ts, keyName, model, tokens, status)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func fixNow(t *testing.T, rfc3339 string) {
	t.Helper()
	fixed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatal(err)
	}
	old := nowFunc
	nowFunc = func() time.Time { return fixed }
	t.Cleanup(func() { nowFunc = old })
}

func TestUsageReportLastActiveIgnoresPeriod(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T10:00:00Z")

	insertAt(t, mgr, "2026-09-15 08:00:00", "pi-dev", "9r/claude", 200, 100) // last month
	insertAt(t, mgr, "2026-10-06 09:26:40", "pi-dev", "9r/claude", 403, 0)   // today, blocked

	report, err := mgr.GetUsageReports("last_month", "", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.KeysBreakdown) != 1 {
		t.Fatalf("expected 1 key, got %d", len(report.KeysBreakdown))
	}
	k := report.KeysBreakdown[0]
	if k.TotalRequests != 1 {
		t.Errorf("period stats should only count last month: got %d requests", k.TotalRequests)
	}
	if k.LastActiveAt == nil || *k.LastActiveAt != "2026-10-06T09:26:40Z" {
		t.Errorf("key last_active_at = %v, want 2026-10-06T09:26:40Z (all-time, includes 403)", k.LastActiveAt)
	}
	if len(report.ModelsBreakdown) != 1 {
		t.Fatalf("expected 1 model, got %d", len(report.ModelsBreakdown))
	}
	if la := report.ModelsBreakdown[0].LastActiveAt; la == nil || *la != "2026-10-06T09:26:40Z" {
		t.Errorf("model last_active_at = %v", la)
	}
}

func TestTodayUsesViewerTimezone(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	// 02:00 UTC = 09:00 in Jakarta. Jakarta "today" starts 2026-10-05 17:00 UTC.
	fixNow(t, "2026-10-06T02:00:00Z")
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	insertAt(t, mgr, "2026-10-05 16:59:59", "a", "m", 200, 1) // Jakarta: Oct 5 23:59:59 (yesterday)
	insertAt(t, mgr, "2026-10-05 17:00:00", "a", "m", 200, 1) // Jakarta: Oct 6 00:00 (today)
	insertAt(t, mgr, "2026-10-06 01:30:00", "a", "m", 200, 1) // Jakarta: Oct 6 08:30 (today)

	stats, err := mgr.GetDashboardStats("today", "", "", jkt)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != 2 {
		t.Errorf("jakarta today: got %d requests, want 2", stats.TotalRequests)
	}
	if stats.Comparison.PrevRequests != 1 {
		t.Errorf("jakarta yesterday: got %d, want 1", stats.Comparison.PrevRequests)
	}
	// Hourly buckets are labelled in Jakarta time: 00:00 and 08:00.
	got := map[string]int{}
	for _, p := range stats.VolumeSeries {
		got[p.Time] = p.Requests
	}
	if got["00:00"] != 1 || got["08:00"] != 1 || got["17:00"] != 0 {
		t.Errorf("buckets not shifted to Jakarta: %v", got)
	}

	statsUTC, err := mgr.GetDashboardStats("today", "", "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if statsUTC.TotalRequests != 1 {
		t.Errorf("utc today: got %d, want 1", statsUTC.TotalRequests)
	}
}

func TestQueryLogsPeriodUsesLoc(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	fixNow(t, "2026-10-06T02:00:00Z")
	jkt, _ := time.LoadLocation("Asia/Jakarta")

	insertAt(t, mgr, "2026-10-05 16:59:59", "a", "m", 200, 1)
	insertAt(t, mgr, "2026-10-05 17:00:00", "a", "m", 200, 1)

	_, total, err := mgr.QueryLogs(FilterParams{Period: "today", Loc: jkt})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("got %d, want 1", total)
	}
	_, total, _ = mgr.QueryLogs(FilterParams{Period: "yesterday", Loc: jkt})
	if total != 1 {
		t.Errorf("yesterday: got %d, want 1", total)
	}
}

func TestUsageReportTimestampsAreRFC3339UTC(t *testing.T) {
	mgr, cleanup := setupTestDB(t)
	defer cleanup()
	_ = mgr.Record(&LogEntry{APIKey: "sk-ng-test-1234567", APIKeyName: "k", Model: "m", StatusCode: 200})
	report, err := mgr.GetUsageReports("all", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	la := report.KeysBreakdown[0].LastActiveAt
	if la == nil || !strings.HasSuffix(*la, "Z") || !strings.Contains(*la, "T") {
		t.Errorf("last_active_at not RFC 3339 UTC: %v", la)
	}
}

func TestPeriodVolumeWindow(t *testing.T) {
	fixNow(t, "2026-10-06T02:00:00Z")
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := nowFunc()

	from, to := periodVolumeWindow("today", "", "", jkt, now)
	if !from.Equal(time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 6, 16, 59, 59, 0, time.UTC)) {
		t.Errorf("today: %v .. %v", from, to)
	}
	from, to = periodVolumeWindow("yesterday", "", "", jkt, now)
	if !from.Equal(time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 5, 16, 59, 59, 0, time.UTC)) {
		t.Errorf("yesterday: %v .. %v", from, to)
	}
	from, to = periodVolumeWindow("7d", "", "", jkt, now)
	if !from.Equal(now.Add(-7*24*time.Hour)) || !to.Equal(now) {
		t.Errorf("7d: %v .. %v", from, to)
	}
	from, to = periodVolumeWindow("", "2026-10-01", "2026-10-01", jkt, now)
	if !from.Equal(time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 10, 1, 16, 59, 59, 0, time.UTC)) {
		t.Errorf("custom: %v .. %v", from, to)
	}
}
