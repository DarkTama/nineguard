package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
	"nineguard/internal/providers"
	"nineguard/internal/proxy"
	"nineguard/internal/traffic"
)

func TestLoopProtection_AbortsRecursiveRequests(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_loop.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	tm := traffic.NewManager(d)
	mm := models.NewManager(d)
	pm := providers.NewManager(d)

	k, err := km.CreateKeyWithOptions("Loop Key", keys.CreateKeyOptions{})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(mm, tm, km, pm, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	instanceID := "inst-uuid-12345"
	p.SetInstanceID(instanceID)

	// 1. Request with trace containing our instance ID -> 508 Loop Detected
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	req.Header.Set("X-NineGuard-Trace", "other-inst,"+instanceID)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != 508 {
		t.Fatalf("expected HTTP 508 Loop Detected, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "loop_detected") {
		t.Fatalf("expected loop_detected in error body, got: %s", rec.Body.String())
	}

	// 2. Request with hop count >= 3 -> 508 Loop Detected
	reqHop := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	reqHop.Header.Set("Authorization", "Bearer "+k.RawKey)
	reqHop.Header.Set("X-NineGuard-Hop", "3")
	recHop := httptest.NewRecorder()

	p.ServeHTTP(recHop, reqHop)

	if recHop.Code != 508 {
		t.Fatalf("expected HTTP 508 for hop count 3, got %d: %s", recHop.Code, recHop.Body.String())
	}
}
