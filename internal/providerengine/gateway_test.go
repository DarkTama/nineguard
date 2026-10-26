package providerengine

import (
	"context"
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
)

func TestOAuthGatewayAdmissionAndMetering(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
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
