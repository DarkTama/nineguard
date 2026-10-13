package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nineguard/internal/providers"
	"nineguard/internal/upstream"
)

func TestParseSyncInterval(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"5m", 5 * time.Minute, false},
		{"10m", 10 * time.Minute, false},
		{" 15m ", 15 * time.Minute, false},
		{"30m", 30 * time.Minute, false},
		{"1m", 5 * time.Minute, false},   // below floor clamps up
		{"30s", 5 * time.Minute, false},  // below floor clamps up
		{"7m", 0, true},                  // not an allowed value
		{"1h", 0, true},                  // above max
		{"abc", 0, true},                 // unparsable
		{"", 0, true},                    // empty
	}
	for _, c := range cases {
		got, err := upstream.ParseSyncInterval(c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("ParseSyncInterval(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
		if !c.wantErr && got != c.want {
			t.Fatalf("ParseSyncInterval(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCacheTTLForInterval(t *testing.T) {
	if got := upstream.CacheTTLForInterval(10 * time.Minute); got != 15*time.Minute {
		t.Fatalf("10m interval TTL = %v, want 15m", got)
	}
	if got := upstream.CacheTTLForInterval(30 * time.Minute); got != 35*time.Minute {
		t.Fatalf("30m interval TTL = %v, want 35m", got)
	}
	if upstream.DefaultCacheTTL != 15*time.Minute {
		t.Fatalf("DefaultCacheTTL = %v, want 15m", upstream.DefaultCacheTTL)
	}
}

func TestFormatInterval(t *testing.T) {
	if got := upstream.FormatInterval(10 * time.Minute); got != "10m" {
		t.Fatalf("FormatInterval = %q, want 10m", got)
	}
}

// GetCachedQuota must honor the TTL: fresh entries are served, expired ones return nil.
func TestGetCachedQuota_HonorsTTL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connections":[]}`))
	}))
	defer server.Close()

	mgr := upstream.NewManager()
	prov := providers.Provider{
		ID:        "p-ttl",
		Name:      "TTL Router",
		Route:     server.URL + "/v1",
		QuotaType: "9router",
	}

	if got := mgr.GetCachedQuota(prov); got != nil {
		t.Fatalf("expected nil before any fetch, got %+v", got)
	}

	mgr.SetCacheTTL(150 * time.Millisecond)
	if _, err := mgr.GetProviderQuotas(context.Background(), prov, false); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if got := mgr.GetCachedQuota(prov); got == nil {
		t.Fatal("expected fresh cache entry to be served")
	}

	time.Sleep(250 * time.Millisecond)
	if got := mgr.GetCachedQuota(prov); got != nil {
		t.Fatal("expected expired cache entry to return nil")
	}
}
