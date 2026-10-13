package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
	"nineguard/internal/upstream"
)

func TestGetUpstreamQuotas_AdminHandler(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_handler_quota.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	pm := providers.NewManager(d)

	// Mock upstream 9router server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/providers/client":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"connections":[{"id":"c1","provider":"anthropic","displayName":"Claude Account"}]}`))
		case "/api/usage/c1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"quotas": {
					"five_hour": {
						"used": 10,
						"total": 100,
						"remainingPercentage": 90,
						"displayName": "5h Session"
					}
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	secret := "pass"
	_, err = pm.CreateProviderWithOptions(providers.ProviderOptions{
		Name:            "9router",
		Route:           mockServer.URL,
		Prefix:          "9r",
		IsDefault:       true,
		IsActive:        true,
		QuotaType:       "9router",
		QuotaAuthSecret: &secret,
	})
	if err != nil {
		t.Fatalf("CreateProviderWithOptions failed: %v", err)
	}

	h := handler.New(nil, nil, nil, nil, nil, pm, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/upstream-quotas", nil)
	rec := httptest.NewRecorder()

	h.GetUpstreamQuotas(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Providers []*upstream.ProviderQuotaResult `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Providers) != 1 {
		t.Fatalf("expected 1 provider result, got %d", len(resp.Providers))
	}
	pRes := resp.Providers[0]
	if pRes.ProviderName != "9router" || pRes.Status != "ok" {
		t.Fatalf("unexpected provider result: %+v", pRes)
	}
	if len(pRes.Connections) != 1 || len(pRes.Connections[0].Buckets) != 1 {
		t.Fatalf("expected 1 connection with 1 bucket, got: %+v", pRes)
	}
	bucket := pRes.Connections[0].Buckets[0]
	if bucket.Name != "5h Session" || bucket.RemainingPercentage != 90 {
		t.Fatalf("unexpected bucket: %+v", bucket)
	}
}

func TestConsumeUpstreamReset(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_handler_reset.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	pm := providers.NewManager(d)

	var resetCalled bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/usage/c1/claude-reset" && r.Method == "POST" {
			resetCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true, "message": "Reset applied"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	prov, err := pm.CreateProviderWithOptions(providers.ProviderOptions{
		Name:      "9router",
		Route:     mockServer.URL,
		Prefix:    "9r",
		IsDefault: true,
		IsActive:  true,
		QuotaType: "9router",
	})
	if err != nil {
		t.Fatalf("CreateProviderWithOptions failed: %v", err)
	}

	h := handler.New(nil, nil, nil, nil, nil, pm, nil, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upstream-quotas/"+prov.ID+"/connections/c1/reset", strings.NewReader(`{"provider":"claude","grant_id":"g1"}`))
	req.SetPathValue("id", prov.ID)
	req.SetPathValue("connId", "c1")
	rec := httptest.NewRecorder()

	h.ConsumeUpstreamReset(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !resetCalled {
		t.Fatalf("expected 9router claude-reset endpoint to be called")
	}
}

func TestGetV1Quota_ClientProxy(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_proxy_quota.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	pm := providers.NewManager(d)
	tm := traffic.NewManager(d)

	// Create test client key with budget
	createdKey, err := km.CreateKeyWithOptions("Test Key", keys.CreateKeyOptions{
		QuotaLimit:  1000000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}

	// Mock upstream 9router server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/providers/client":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"connections": [
					{"id":"c1","provider":"anthropic","displayName":"Claude #1"},
					{"id":"c2","provider":"anthropic","displayName":"Claude #2"}
				]
			}`))
		case "/api/usage/c1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"quotas": {
					"five_hour": { "used": 10, "total": 100, "displayName": "5h Session" }
				}
			}`))
		case "/api/usage/c2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"quotas": {
					"five_hour": { "used": 20, "total": 100, "displayName": "5h Session" }
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	_, err = pm.CreateProviderWithOptions(providers.ProviderOptions{
		Name:      "9router",
		Route:     mockServer.URL,
		Prefix:    "9r",
		IsDefault: true,
		IsActive:  true,
		QuotaType: "9router",
	})
	if err != nil {
		t.Fatalf("CreateProviderWithOptions failed: %v", err)
	}

	revProxy, err := proxy.NewProxy(nil, tm, km, pm, nil)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/quota", nil)
	req.Header.Set("Authorization", "Bearer "+createdKey.RawKey)
	rec := httptest.NewRecorder()

	revProxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp upstream.UnifiedQuotaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify client key budget
	if resp.KeyQuota == nil {
		t.Fatalf("expected non-nil KeyQuota")
	}
	if resp.KeyQuota.Limit != 1000000 || resp.KeyQuota.Period != "daily" {
		t.Fatalf("unexpected KeyQuota: %+v", resp.KeyQuota)
	}

	// Verify merged upstream pools (2 accounts merged into 1 pool!)
	if len(resp.UpstreamPools) != 1 {
		t.Fatalf("expected 1 merged pool, got %d", len(resp.UpstreamPools))
	}
	pool := resp.UpstreamPools[0]
	if pool.Provider != "9router" || len(pool.Buckets) != 1 {
		t.Fatalf("unexpected pool: %+v", pool)
	}
	b := pool.Buckets[0]
	// Used 10 + 20 = 30; Total 100 + 100 = 200; Remaining 170 = 85%
	if b.Used != 30 || b.Total != 200 || b.Remaining != 170 || b.RemainingPercentage != 85 {
		t.Fatalf("unexpected merged bucket math: %+v", b)
	}
}
