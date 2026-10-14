package traffic_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestUpstreamSyncInterval_DefaultAndRoundTrip(t *testing.T) {
	d, err := db.InitDB(filepath.Join(t.TempDir(), "sync_interval.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	tm := traffic.NewManager(d)
	if got := tm.GetUpstreamSyncInterval(); got != "10m" {
		t.Fatalf("default = %q, want 10m", got)
	}
	if err := tm.SetUpstreamSyncInterval("30m"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if got := tm.GetUpstreamSyncInterval(); got != "30m" {
		t.Fatalf("after set = %q, want 30m", got)
	}
}
