package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

type mqEnv struct {
	p  *proxy.Proxy
	km *keys.Manager
	tm *traffic.Manager
	d  *db.DB
}

func newMQEnv(t *testing.T) *mqEnv {
	t.Helper()
	d, err := db.InitDB(filepath.Join(t.TempDir(), "mq.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	t.Cleanup(up.Close)

	km := keys.NewManager(d, "")
	pm := providers.NewManager(d)
	if _, err := pm.CreateProvider("Mock", up.URL, "", "mock", true, true); err != nil {
		t.Fatal(err)
	}
	tm := traffic.NewManager(d)
	p, err := proxy.NewProxy(models.NewManager(d), tm, km, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &mqEnv{p: p, km: km, tm: tm, d: d}
}

func (e *mqEnv) send(t *testing.T, rawKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	e.p.ServeHTTP(rec, req)
	return rec
}

func chatBody(model string) string {
	return `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
}

func (e *mqEnv) used(t *testing.T, k *keys.KeyInfo, modelID string, tokens int) {
	t.Helper()
	if err := e.tm.Record(&traffic.LogEntry{APIKey: k.RawKey, APIKeyName: k.Name, APIKeyID: k.ID, Model: modelID, ModelID: modelID, StatusCode: 200, TotalTokens: tokens}); err != nil {
		t.Fatal(err)
	}
}

func (e *mqEnv) waitModelID(t *testing.T, keyID, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var got string
		_ = e.d.QueryRow(`SELECT COALESCE(model_id,'') FROM traffic_logs WHERE api_key_id=? AND status_code=200 ORDER BY id DESC LIMIT 1`, keyID).Scan(&got)
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no 200 log row with model_id %q", want)
}

func TestModelQuotaBlocksOnlyThatModel(t *testing.T) {
	e := newMQEnv(t)
	k, err := e.km.CreateKeyWithOptions("ari", keys.CreateKeyOptions{
		QuotaMode:   "model",
		ModelQuotas: []keys.ModelQuota{{ModelID: "mock/a", QuotaLimit: 100, QuotaPeriod: "daily"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.used(t, k, "mock/a", 150)

	for _, model := range []string{"mock/a", "a"} { // bare name resolves to mock/a
		rec := e.send(t, k.RawKey, chatBody(model))
		if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "model_quota_exceeded") {
			t.Fatalf("%s: want 429 model_quota_exceeded, got %d %s", model, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: missing Retry-After", model)
		}
	}
	var mid string
	var tok int
	_ = e.d.QueryRow(`SELECT COALESCE(model_id,''), total_tokens FROM traffic_logs WHERE api_key_id=? AND status_code=429 ORDER BY id DESC LIMIT 1`, k.ID).Scan(&mid, &tok)
	if mid != "mock/a" || tok != 0 {
		t.Errorf("429 log row: model_id=%q tokens=%d", mid, tok)
	}

	rec := e.send(t, k.RawKey, chatBody("mock/b")) // another model on the same key still works
	if rec.Code != http.StatusOK {
		t.Fatalf("mock/b: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	e.waitModelID(t, k.ID, "mock/b")

	// A bare request that is under its limit is logged with the resolved id.
	rec = e.send(t, k.RawKey, chatBody("c"))
	if rec.Code != http.StatusOK {
		t.Fatalf("c: %d", rec.Code)
	}
	e.waitModelID(t, k.ID, "mock/c")
}

func TestModelQuotaModesAreExclusive(t *testing.T) {
	e := newMQEnv(t)
	rows := []keys.ModelQuota{{ModelID: "mock/a", QuotaLimit: 100, QuotaPeriod: "daily"}}

	// model mode ignores an exhausted Key Quota
	km, _ := e.km.CreateKeyWithOptions("m", keys.CreateKeyOptions{QuotaMode: "model", QuotaLimit: 10, QuotaPeriod: "daily", ModelQuotas: rows})
	e.used(t, km, "mock/zzz", 500)
	if rec := e.send(t, km.RawKey, chatBody("mock/a")); rec.Code != http.StatusOK {
		t.Fatalf("model mode must ignore Key Quota, got %d %s", rec.Code, rec.Body.String())
	}

	// key mode ignores exhausted Model Quota rows
	kk, _ := e.km.CreateKeyWithOptions("k", keys.CreateKeyOptions{QuotaMode: "key", ModelQuotas: rows})
	e.used(t, kk, "mock/a", 500)
	if rec := e.send(t, kk.RawKey, chatBody("mock/a")); rec.Code != http.StatusOK {
		t.Fatalf("key mode must ignore Model Quotas, got %d %s", rec.Code, rec.Body.String())
	}

	// key mode still enforces its own Key Quota, with the unchanged code
	kq, _ := e.km.CreateKeyWithOptions("q", keys.CreateKeyOptions{QuotaLimit: 10, QuotaPeriod: "daily"})
	e.used(t, kq, "mock/a", 50)
	rec := e.send(t, kq.RawKey, chatBody("mock/a"))
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"quota_exceeded"`) || strings.Contains(rec.Body.String(), "model_quota") {
		t.Fatalf("key quota: %d %s", rec.Code, rec.Body.String())
	}
}

func TestLifetimeQuotaHasNoRetryAfter(t *testing.T) {
	e := newMQEnv(t)
	km, _ := e.km.CreateKeyWithOptions("m", keys.CreateKeyOptions{QuotaMode: "model", ModelQuotas: []keys.ModelQuota{{ModelID: "mock/a", QuotaLimit: 5, QuotaPeriod: "total"}}})
	e.used(t, km, "mock/a", 9)
	rec := e.send(t, km.RawKey, chatBody("mock/a"))
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "" || !strings.Contains(rec.Body.String(), "does not reset") {
		t.Fatalf("model total: %d ra=%q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}

	kk, _ := e.km.CreateKeyWithOptions("k", keys.CreateKeyOptions{QuotaLimit: 5, QuotaPeriod: "total"})
	e.used(t, kk, "mock/a", 9)
	rec = e.send(t, kk.RawKey, chatBody("mock/a"))
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "" || !strings.Contains(rec.Body.String(), "does not reset") {
		t.Fatalf("key total: %d ra=%q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestAccessCheckPrecedesModelQuota(t *testing.T) {
	e := newMQEnv(t)
	k, _ := e.km.CreateKeyWithOptions("c", keys.CreateKeyOptions{
		ModelAccessMode: "custom", AllowedModels: []string{"mock/x"}, QuotaMode: "model",
		ModelQuotas: []keys.ModelQuota{{ModelID: "mock/a", QuotaLimit: 1, QuotaPeriod: "daily"}},
	})
	e.used(t, k, "mock/a", 99)
	rec := e.send(t, k.RawKey, chatBody("mock/a"))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "model_not_allowed") {
		t.Fatalf("want 403 model_not_allowed, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRequestWithoutModelSkipsModelQuota(t *testing.T) {
	e := newMQEnv(t)
	k, _ := e.km.CreateKeyWithOptions("n", keys.CreateKeyOptions{QuotaMode: "model", ModelQuotas: []keys.ModelQuota{{ModelID: "mock/a", QuotaLimit: 1, QuotaPeriod: "daily"}}})
	e.used(t, k, "mock/a", 99)
	rec := e.send(t, k.RawKey, `{"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("a request with no model must not hit a Model Quota: %d %s", rec.Code, rec.Body.String())
	}
}
