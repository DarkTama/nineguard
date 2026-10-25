package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestProxyQuotaExceededReturns429(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	k, err := keyMgr.CreateKeyWithOptions("quota-key", keys.CreateKeyOptions{
		QuotaLimit:  1000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	// Insert past traffic exceeding 1000 tokens today
	err = trafficMgr.Record(&traffic.LogEntry{
		APIKey:      k.Key,
		APIKeyName:  k.Name,
		APIKeyID:    k.ID,
		Model:       "test-model",
		StatusCode:  200,
		TotalTokens: 1500,
		ClientIP:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("insert traffic: %v", err)
	}

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", w.Code, w.Body.String())
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Errorf("expected Retry-After header to be set")
	}
	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "quota_exceeded") {
		t.Errorf("expected quota_exceeded in body, got %s", bodyStr)
	}

	// Verify error log recorded in traffic
	logs, _, err := trafficMgr.QueryLogs(traffic.FilterParams{Limit: 10})
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected blocked request to be logged in traffic, got err: %v, count: %d", err, len(logs))
	}
	if logs[0].StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected logged status 429, got %d", logs[0].StatusCode)
	}
}

func TestProxyQuotaUnderLimitProceeds(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	modelsMgr := models.NewManager(d)
	providersMgr := providers.NewManager(d)

	k, err := keyMgr.CreateKeyWithOptions("quota-key-under", keys.CreateKeyOptions{
		QuotaLimit:  1000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	// Insert past traffic below 1000 tokens
	_ = trafficMgr.Record(&traffic.LogEntry{
		APIKey:      k.Key,
		APIKeyName:  k.Name,
		APIKeyID:    k.ID,
		Model:       "test-model",
		StatusCode:  200,
		TotalTokens: 500,
		ClientIP:    "127.0.0.1",
	})

	p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)

	// Since there is no upstream provider configured, expect 502 Bad Gateway (NOT 429!)
	if w.Code == http.StatusTooManyRequests {
		t.Fatalf("expected request NOT to be blocked with 429 when under quota, got 429")
	}
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502 (no provider), got %d: %s", w.Code, w.Body.String())
	}
}

func TestUnifiedQuotaIncludesKeyName(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	keyMgr := keys.NewManager(d, "")
	trafficMgr := traffic.NewManager(d)
	p, err := proxy.NewProxy(models.NewManager(d), trafficMgr, keyMgr, providers.NewManager(d), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	k, err := keyMgr.CreateKeyWithOptions("Cursor Laptop", keys.CreateKeyOptions{})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/quota", nil)
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"key_name":"Cursor Laptop"`) {
		t.Fatalf("expected key_name in /v1/quota body, got %s", w.Body.String())
	}
}

func TestUnifiedQuotaFlagsInactiveModelQuotas(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO models (id,name,provider_id,enabled) VALUES ('p/on','p/on','p',1),('p/off','p/off','p',0)`); err != nil {
		t.Fatal(err)
	}

	keyMgr := keys.NewManager(d, "")
	p, err := proxy.NewProxy(models.NewManager(d), traffic.NewManager(d), keyMgr, providers.NewManager(d), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	k, err := keyMgr.CreateKeyWithOptions("team", keys.CreateKeyOptions{QuotaMode: "model", ModelQuotas: []keys.ModelQuota{
		{ModelID: "p/on", QuotaLimit: 1000, QuotaPeriod: "daily"},
		{ModelID: "p/off", QuotaLimit: 1000, QuotaPeriod: "daily"},
	}})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/quota", nil)
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	var resp struct {
		ModelQuotas []struct {
			ModelID  string `json:"model_id"`
			Inactive bool   `json:"inactive"`
		} `json:"model_quotas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v: %s", err, w.Body.String())
	}
	got := map[string]bool{}
	for _, r := range resp.ModelQuotas {
		got[r.ModelID] = r.Inactive
	}
	if len(got) != 2 || got["p/on"] || !got["p/off"] {
		t.Fatalf("expected p/off inactive and p/on active, got %v (%s)", got, w.Body.String())
	}
}
