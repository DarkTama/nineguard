package providerengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
	upstreamquota "nineguard/internal/upstream"
)

func TestOAuthGatewayAdmissionAndMetering(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.53")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Reset", "2000018000")
		w.Header().Set("Anthropic-Ratelimit-Unified-7d-Utilization", "0.0")
		io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"gateway smoke"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":3}}`)
	}))
	defer upstream.Close()
	s, d := testStore(t)
	a := grantRecord(t, s)
	a.Attributes["base_url"] = upstream.URL
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	e, err := New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	catalog, err := e.Models(context.Background(), "p")
	if err != nil || len(catalog) == 0 {
		t.Fatal(err)
	}
	id := "p/" + catalog[0]["id"].(string)
	if _, err = d.Exec(`INSERT INTO models(id,name,provider_id,enabled) VALUES(?,?,?,1)`, id, id, "p"); err != nil {
		t.Fatal(err)
	}
	km := keys.NewManager(d, "")
	key, err := km.CreateKeyWithOptions("smoke", keys.CreateKeyOptions{ModelAccessMode: "custom", AllowedModels: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	pm := providers.NewManager(d)
	pm.SetEngine(e)
	tm := traffic.NewManager(d)
	mm := models.NewManager(d)
	gateway, err := proxy.NewProxy(mm, tm, km, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	gateway.SetProviderEngine(e)
	quotaManager := upstreamquota.NewManager()
	quotaManager.SetSDKQuotaSource(e)
	gateway.SetUpstreamManager(quotaManager)
	call := func(model string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"`+model+`","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer "+key.RawKey)
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		return rec
	}
	rec := call(id)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "gateway smoke") {
		t.Fatalf("gateway %d %s", rec.Code, rec.Body.String())
	}
	var requests, tokens int
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err = d.QueryRow(`SELECT COUNT(*),COALESCE(SUM(total_tokens),0) FROM traffic_logs WHERE api_key_id=? AND status_code=200`, key.ID).Scan(&requests, &tokens); err != nil {
			t.Fatal(err)
		}
		if requests > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if requests != 1 || tokens != 14 {
		t.Fatalf("metering %d requests, %d tokens", requests, tokens)
	}
	provider, err := pm.GetProvider("p")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := quotaManager.GetProviderQuotas(context.Background(), *provider, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Connections) != 1 || len(snapshot.Connections[0].Buckets) < 2 {
		t.Fatalf("SDK headers not projected: %+v", snapshot)
	}
	var found bool
	for _, bucket := range snapshot.Connections[0].Buckets {
		if bucket.RemainingPercent != nil && *bucket.RemainingPercent == 47 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected observed 47%% left, got %+v", snapshot.Connections[0].Buckets)
	}
	req := httptest.NewRequest("GET", "/v1/quota?force=1", nil)
	req.Header.Set("Authorization", "Bearer "+key.RawKey)
	quotaRec := httptest.NewRecorder()
	gateway.ServeHTTP(quotaRec, req)
	var quotaResult upstreamquota.UnifiedQuotaResponse
	if err := json.Unmarshal(quotaRec.Body.Bytes(), &quotaResult); err != nil {
		t.Fatal(err)
	}
	if len(quotaResult.UpstreamPools) == 0 || len(quotaResult.UpstreamPools[0].Buckets) == 0 {
		t.Fatalf("SDK capacity absent from client quota: %s", quotaRec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("quota refresh issued inference: %d", calls)
	}
	otherKey, err := km.CreateKeyWithOptions("other", keys.CreateKeyOptions{ModelAccessMode: "custom", AllowedModels: []string{"unrelated/model"}})
	if err != nil {
		t.Fatal(err)
	}
	otherRequest := httptest.NewRequest("GET", "/v1/quota", nil)
	otherRequest.Header.Set("Authorization", "Bearer "+otherKey.RawKey)
	otherResponse := httptest.NewRecorder()
	gateway.ServeHTTP(otherResponse, otherRequest)
	var hidden upstreamquota.UnifiedQuotaResponse
	if err := json.Unmarshal(otherResponse.Body.Bytes(), &hidden); err != nil {
		t.Fatal(err)
	}
	if len(hidden.UpstreamPools) != 0 {
		t.Fatal("SDK capacity visible to unrelated restricted key")
	}
	rec = call("p/forbidden")
	if rec.Code != http.StatusForbidden || calls != 1 {
		t.Fatalf("unauthorized model reached upstream: status %d calls %d", rec.Code, calls)
	}
	if err = mm.SetModelEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	rec = call(id)
	if rec.Code != http.StatusForbidden || calls != 1 {
		t.Fatalf("disabled model reached upstream: status %d calls %d", rec.Code, calls)
	}
}
