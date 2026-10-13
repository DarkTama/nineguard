package proxy_test

import (
	"encoding/json"
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

func TestChatCommand_Interception(t *testing.T) {
	tmpDir := t.TempDir()
	d, err := db.InitDB(filepath.Join(tmpDir, "test_cmd_interception.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer d.Close()

	km := keys.NewManager(d, "")
	tm := traffic.NewManager(d)
	mm := models.NewManager(d)
	pm := providers.NewManager(d)

	k, err := km.CreateKeyWithOptions("Cmd Key", keys.CreateKeyOptions{
		QuotaLimit:  500000,
		QuotaPeriod: "daily",
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	p, err := proxy.NewProxy(mm, tm, km, pm, nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	// 1. Test non-streaming /quota
	bodyJSON := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"/quota"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(bodyJSON))
	req.Header.Set("Authorization", "Bearer "+k.RawKey)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	content := resp.Choices[0].Message.Content
	if !strings.Contains(content, "NineGuard Quota Report") {
		t.Fatalf("expected quota report markdown, got: %s", content)
	}
	if !strings.Contains(content, "500,000 tokens remaining") {
		t.Fatalf("expected 500,000 tokens in report, got: %s", content)
	}
	if resp.Usage.TotalTokens != 0 {
		t.Fatalf("expected 0 total tokens, got %d", resp.Usage.TotalTokens)
	}

	// 2. Test streaming /limits
	streamBodyJSON := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"/limits"}]}`
	streamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBodyJSON))
	streamReq.Header.Set("Authorization", "Bearer "+k.RawKey)
	streamRec := httptest.NewRecorder()

	p.ServeHTTP(streamRec, streamReq)

	if streamRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", streamRec.Code, streamRec.Body.String())
	}
	streamOutput := streamRec.Body.String()
	if !strings.Contains(streamOutput, "data: [DONE]") {
		t.Fatalf("expected SSE stream with [DONE], got: %s", streamOutput)
	}
	if !strings.Contains(streamOutput, "NineGuard Quota Report") {
		t.Fatalf("expected markdown in stream chunk, got: %s", streamOutput)
	}

	// 3. Test toggle: disable chat commands
	_ = tm.SetChatCommandsEnabled(false)

	// A normal request when disabled should proceed to upstream routing rather than intercepting
	recDisabled := httptest.NewRecorder()
	p.ServeHTTP(recDisabled, req)
	// Without upstream provider configured for gpt-4o, it fails at model routing, not intercepted
	if strings.Contains(recDisabled.Body.String(), "NineGuard Quota Report") {
		t.Fatalf("expected chat command to be bypassed when disabled, but it was intercepted")
	}
}
