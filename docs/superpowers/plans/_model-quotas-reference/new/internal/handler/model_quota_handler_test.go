package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/traffic"
)

type mqH struct {
	h  *handler.Handler
	d  *db.DB
	km *keys.Manager
}

func newMQH(t *testing.T) *mqH {
	t.Helper()
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	km := keys.NewManager(d, "")
	pm := providers.NewManager(d)
	if _, err := pm.CreateProvider("Router", "http://x", "", "9router", true, true); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO models (id,name,provider_id,enabled) VALUES ('9router/a','9router/a','9router',1),('9router/b','9router/b','9router',1),('9router/off','9router/off','9router',0),('a','a','',1)`,
		`INSERT INTO models (id,name,provider_id,enabled,removed_at) VALUES ('9router/gone','9router/gone','9router',1,CURRENT_TIMESTAMP)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	h := handler.New(nil, models.NewManager(d), traffic.NewManager(d), nil, km, pm, nil, "")
	return &mqH{h: h, d: d, km: km}
}

func (e *mqH) call(fn http.HandlerFunc, method, path, body, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func TestCreateKeyWithModelQuotas(t *testing.T) {
	e := newMQH(t)
	ok := e.call(e.h.CreateKey, "POST", "/api/v1/keys", `{"name":"ari","quota_mode":"model","model_quotas":[{"model_id":"9router/a","quota_limit":1000,"quota_period":"daily"}]}`, "")
	if ok.Code != 200 {
		t.Fatalf("valid create: %d %s", ok.Code, ok.Body.String())
	}
	var ki keys.KeyInfo
	_ = json.Unmarshal(ok.Body.Bytes(), &ki)
	if ki.QuotaMode != "model" || len(ki.ModelQuotas) != 1 {
		t.Fatalf("response: %+v", ki)
	}

	bad := map[string]string{
		"limit zero":     `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"9router/a","quota_limit":0,"quota_period":"daily"}]}`,
		"period none":    `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"9router/a","quota_limit":5,"quota_period":"none"}]}`,
		"duplicate":      `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"9router/a","quota_limit":5,"quota_period":"daily"},{"model_id":"9ROUTER/A","quota_limit":5,"quota_period":"daily"}]}`,
		"unknown model":  `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"9router/zzz","quota_limit":5,"quota_period":"daily"}]}`,
		"disabled":       `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"9router/off","quota_limit":5,"quota_period":"daily"}]}`,
		"removed":        `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"9router/gone","quota_limit":5,"quota_period":"daily"}]}`,
		"not canonical":  `{"name":"x","quota_mode":"model","model_quotas":[{"model_id":"a","quota_limit":5,"quota_period":"daily"}]}`,
		"not allowed":    `{"name":"x","model_access_mode":"custom","allowed_models":["9router/b"],"quota_mode":"model","model_quotas":[{"model_id":"9router/a","quota_limit":5,"quota_period":"daily"}]}`,
		"bad quota_mode": `{"name":"x","quota_mode":"nope"}`,
	}
	for name, body := range bad {
		if rec := e.call(e.h.CreateKey, "POST", "/api/v1/keys", body, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	var n int
	_ = e.d.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE name='x'`).Scan(&n)
	if n != 0 {
		t.Errorf("rejected creates must not leave keys behind, found %d", n)
	}
}

func TestUpdateKeyModelQuotaSemantics(t *testing.T) {
	e := newMQH(t)
	k, _ := e.km.CreateKeyWithOptions("ari", keys.CreateKeyOptions{
		QuotaMode:   "model",
		ModelQuotas: []keys.ModelQuota{{ModelID: "9router/a", QuotaLimit: 10, QuotaPeriod: "daily"}},
	})
	upd := func(body string) *httptest.ResponseRecorder {
		return e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/"+k.ID, body, k.ID)
	}
	count := func(rec *httptest.ResponseRecorder) int {
		var ki keys.KeyInfo
		_ = json.Unmarshal(rec.Body.Bytes(), &ki)
		return len(ki.ModelQuotas)
	}

	if rec := upd(`{"name":"ari"}`); rec.Code != 200 || count(rec) != 1 {
		t.Fatalf("omitted model_quotas must keep rows: %d %s", rec.Code, rec.Body.String())
	}
	// Access changes so the stored row is no longer allowed; the unchanged row is still accepted.
	rec := upd(`{"name":"ari","model_access_mode":"custom","allowed_models":["9router/b"],"model_quotas":[{"model_id":"9router/a","quota_limit":20,"quota_period":"weekly"}]}`)
	if rec.Code != 200 || count(rec) != 1 {
		t.Fatalf("stored inactive row must be accepted: %d %s", rec.Code, rec.Body.String())
	}
	// A NEW row that is not allowed is rejected.
	rec = upd(`{"name":"ari","model_access_mode":"custom","allowed_models":["9router/b"],"model_quotas":[{"model_id":"9router/a","quota_limit":20,"quota_period":"weekly"},{"model_id":"9router/off","quota_limit":1,"quota_period":"daily"}]}`)
	if rec.Code != 400 {
		t.Fatalf("new disabled row: %d %s", rec.Code, rec.Body.String())
	}
	if rec := upd(`{"name":"ari","model_quotas":[]}`); rec.Code != 200 || count(rec) != 0 {
		t.Fatalf("empty list must clear: %d %s", rec.Code, rec.Body.String())
	}
	if rec := upd(`{"name":"ari","quota_mode":"key"}`); rec.Code != 200 {
		t.Fatalf("mode switch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.call(e.h.UpdateKey, "PUT", "/api/v1/keys/nope", `{"name":"x"}`, "nope"); rec.Code != 400 {
		t.Fatalf("unknown key keeps the existing 400: %d", rec.Code)
	}
}

func TestGetKeyQuotasAndVelocityHandlers(t *testing.T) {
	e := newMQH(t)
	k, _ := e.km.CreateKeyWithOptions("ari", keys.CreateKeyOptions{
		QuotaMode: "model",
		ModelQuotas: []keys.ModelQuota{
			{ModelID: "9router/a", QuotaLimit: 100, QuotaPeriod: "daily"},
			{ModelID: "9router/b", QuotaLimit: 100, QuotaPeriod: "daily"},
		},
	})
	_, _ = e.d.Exec(`INSERT INTO traffic_logs (api_key_id,model,model_id,status_code,total_tokens,timestamp) VALUES (?,?,?,200,150,datetime('now','-1 hours'))`, k.ID, "9router/a", "9router/a")
	_, _ = e.d.Exec(`INSERT INTO traffic_logs (api_key_id,model,status_code,total_tokens,timestamp) VALUES (?,?,200,40,datetime('now','-1 hours'))`, k.ID, "b") // legacy bare row

	rec := e.call(e.h.GetKey, "GET", "/api/v1/keys/"+k.ID, "", k.ID)
	var ki keys.KeyInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &ki)
	if rec.Code != 200 || len(ki.ModelQuotas) != 2 {
		t.Fatalf("GetKey: %d %s", rec.Code, rec.Body.String())
	}
	var used int64
	for _, q := range ki.ModelQuotas {
		if q.ModelID == "9router/a" {
			used = q.Usage
		}
	}
	if used != 150 {
		t.Errorf("usage for 9router/a = %d", used)
	}
	if rec := e.call(e.h.GetKey, "GET", "/api/v1/keys/none", "", "none"); rec.Code != 404 {
		t.Errorf("unknown key: %d", rec.Code)
	}

	rec = e.call(e.h.GetQuotas, "GET", "/api/v1/quotas", "", "")
	var ov keys.QuotaOverview
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil || rec.Code != 200 {
		t.Fatalf("quotas: %d %v %s", rec.Code, err, rec.Body.String())
	}
	if ov.WarnPercent != 80 || ov.Summary.Exhausted != 1 {
		t.Errorf("overview: %+v", ov)
	}

	if rec := e.call(e.h.SetTrafficSettings, "POST", "/api/v1/settings/traffic", `{"quota_warn_percent":0}`, ""); rec.Code != 400 {
		t.Errorf("warn 0 must be 400, got %d", rec.Code)
	}
	if rec := e.call(e.h.SetTrafficSettings, "POST", "/api/v1/settings/traffic", `{"quota_warn_percent":90}`, ""); rec.Code != 200 {
		t.Errorf("warn 90: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.call(e.h.GetTrafficSettings, "GET", "/api/v1/settings/traffic", "", "")
	if !strings.Contains(rec.Body.String(), `"quota_warn_percent":90`) {
		t.Errorf("settings: %s", rec.Body.String())
	}

	// model-velocity: the bare legacy row counts for 9router/b (default provider), flagged approximate
	req := httptest.NewRequest("GET", "/api/v1/keys/"+k.ID+"/model-velocity?model=9router/b", nil)
	req.SetPathValue("id", k.ID)
	vrec := httptest.NewRecorder()
	e.h.GetKeyModelVelocity(vrec, req)
	var v map[string]any
	_ = json.Unmarshal(vrec.Body.Bytes(), &v)
	if vrec.Code != 200 || v["approximate"] != true || v["tokens_24h"].(float64) != 40 {
		t.Errorf("velocity: %d %s", vrec.Code, vrec.Body.String())
	}
	req = httptest.NewRequest("GET", "/api/v1/keys/"+k.ID+"/model-velocity", nil)
	req.SetPathValue("id", k.ID)
	vrec = httptest.NewRecorder()
	e.h.GetKeyModelVelocity(vrec, req)
	if vrec.Code != 400 {
		t.Errorf("missing model: %d", vrec.Code)
	}
}
