package proxy_test

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
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

// Clients such as Claude Code advertise "gzip, deflate, br, zstd". NineGuard reads the
// upstream body (usage diagnostics, SSE relay, decloak), so it must receive and relay
// plaintext regardless of what the client offers.
func TestProxy_ClientAcceptEncodingDoesNotLeakCompressedBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream bool
		ctype  string
		body   string
	}{
		{"non-stream", false, "application/json",
			`{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-7-sonnet","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":11,"output_tokens":7}}`},
		{"stream", true, "text/event-stream",
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-3-7-sonnet\",\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":7}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := db.InitDB(":memory:")
			if err != nil {
				t.Fatalf("InitDB: %v", err)
			}
			defer d.Close()

			keyMgr := keys.NewManager(d, "")
			trafficMgr := traffic.NewManager(d)
			modelsMgr := models.NewManager(d)
			providersMgr := providers.NewManager(d)

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
					_, _ = w.Write([]byte(tc.body))
					return
				}
				w.Header().Set("Content-Encoding", "gzip")
				gz := gzip.NewWriter(w)
				_, _ = gz.Write([]byte(tc.body))
				_ = gz.Close()
			}))
			defer upstream.Close()

			if _, err = providersMgr.CreateProvider("Mock Anthropic", upstream.URL, "upstream-secret-key", "mock", true, true); err != nil {
				t.Fatalf("CreateProvider: %v", err)
			}
			clientKey, err := keyMgr.CreateKey("test-client", "all", nil, nil)
			if err != nil {
				t.Fatalf("CreateKey: %v", err)
			}
			p, err := proxy.NewProxy(modelsMgr, trafficMgr, keyMgr, providersMgr, nil)
			if err != nil {
				t.Fatalf("NewProxy: %v", err)
			}

			streamField := "false"
			if tc.stream {
				streamField = "true"
			}
			reqBody := `{"model":"mock/claude-3-7-sonnet","stream":` + streamField + `,"messages":[{"role":"user","content":"hi"}]}`
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
			req.Header.Set("Authorization", "Bearer "+clientKey.RawKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %q", rec.Code, rec.Body.String())
			}
			if ce := rec.Header().Get("Content-Encoding"); ce != "" {
				t.Errorf("client Content-Encoding = %q, want none (body is relayed as plaintext)", ce)
			}
			if got := rec.Body.String(); got != tc.body {
				t.Errorf("client body = %q, want upstream plaintext %q", got, tc.body)
			}
			// Give async log writer goroutine time to finish
			time.Sleep(100 * time.Millisecond)
			logs, _, err := trafficMgr.QueryLogs(traffic.FilterParams{})
			if err != nil || len(logs) != 1 {
				t.Fatalf("QueryLogs: %v, rows=%d", err, len(logs))
			}
			if logs[0].PromptTokens != 11 || logs[0].CompletionTokens != 7 {
				t.Errorf("logged usage = %d/%d, want 11/7 parsed from upstream body", logs[0].PromptTokens, logs[0].CompletionTokens)
			}
		})
	}
}
