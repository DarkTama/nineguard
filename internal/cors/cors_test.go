package cors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareAllowedOriginPreflight(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusUnauthorized)
	})

	handler := Middleware([]string{"https://app.example"})(next)

	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("expected wrapped handler not to be called on allowed-origin preflight")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://app.example")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, "GET, POST, OPTIONS")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want %q", got, "Authorization, Content-Type")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("Access-Control-Max-Age = %q, want %q", got, "600")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("unexpected Access-Control-Allow-Credentials: %q", got)
	}
	if !headerContainsToken(rec.Header().Values("Vary"), "Origin") {
		t.Fatalf("expected Vary to include Origin, got %v", rec.Header().Values("Vary"))
	}
}

func TestMiddlewareAllowedOriginGetV1Models(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	handler := Middleware([]string{"https://app.example", "http://localhost:5173"})(next)

	for _, path := range []string{"/v1", "/v1/models", "/v1beta/models"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:5173")
			}
			if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
				t.Fatalf("unexpected Access-Control-Allow-Credentials: %q", got)
			}
			if !headerContainsToken(rec.Header().Values("Vary"), "Origin") {
				t.Fatalf("expected Vary to include Origin, got %v", rec.Header().Values("Vary"))
			}
		})
	}
}

func TestMiddlewareDisallowedOrigin(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusUnauthorized)
	})

	handler := Middleware([]string{"https://app.example"})(next)

	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("expected preflight from disallowed origin to pass through to next handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertNoAccessControlHeaders(t, rec.Header())
}

func TestMiddlewareNonProxyPathsNeverGetCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware([]string{"https://app.example"})(next)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/keys"},
		{http.MethodOptions, "/api/v1/keys"},
		{http.MethodGet, "/portal"},
		{http.MethodGet, "/"},
		{http.MethodOptions, "/v10/models"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Origin", "https://app.example")
			if tc.method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", "GET")
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			assertNoAccessControlHeaders(t, rec.Header())
		})
	}
}

func TestMiddlewareEmptyConfig(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(nil)(next)

	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		req := httptest.NewRequest(method, "/v1/models", nil)
		req.Header.Set("Origin", "https://app.example")
		if method == http.MethodOptions {
			req.Header.Set("Access-Control-Request-Method", "GET")
		}
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assertNoAccessControlHeaders(t, rec.Header())
	}
}

func TestMiddlewareStripsUpstreamAccessControlHeaders(t *testing.T) {
	upstreamSim := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Credentials", "true")
		w.Header().Add("Access-Control-Expose-Headers", "X-Upstream-Header")
		w.Header().Add("Vary", "Accept-Encoding")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := Middleware([]string{"https://app.example"})(upstreamSim)

	t.Run("allowed origin gets single origin and no upstream CORS leaks", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
		req.Header.Set("Origin", "https://app.example")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		gotOrigins := rec.Header().Values("Access-Control-Allow-Origin")
		if len(gotOrigins) != 1 || gotOrigins[0] != "https://app.example" {
			t.Fatalf("Access-Control-Allow-Origin = %v, want [https://app.example]", gotOrigins)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("unexpected Access-Control-Allow-Credentials: %q", got)
		}
		if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "" {
			t.Fatalf("unexpected Access-Control-Expose-Headers: %q", got)
		}
		if !headerContainsToken(rec.Header().Values("Vary"), "Origin") || !headerContainsToken(rec.Header().Values("Vary"), "Accept-Encoding") {
			t.Fatalf("expected Vary to keep Accept-Encoding and add Origin, got %v", rec.Header().Values("Vary"))
		}
	})

	t.Run("disallowed origin removes upstream Access-Control-* headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
		req.Header.Set("Origin", "http://evil.example")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assertNoAccessControlHeaders(t, rec.Header())
	})
}

func TestMiddlewareStreamingFlusherAndResponseController(t *testing.T) {
	streamHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected wrapped ResponseWriter to implement http.Flusher")
		}

		_, _ = w.Write([]byte("data: chunk1\n\n"))
		flusher.Flush()

		rc := http.NewResponseController(w)
		if err := rc.Flush(); err != nil {
			t.Fatalf("ResponseController.Flush() error: %v", err)
		}
	})

	handler := Middleware([]string{"https://app.example"})(streamHandler)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !rec.Flushed {
		t.Fatal("expected underlying ResponseRecorder to be flushed")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://app.example")
	}
	if body := rec.Body.String(); body != "data: chunk1\n\n" {
		t.Fatalf("body = %q, want %q", body, "data: chunk1\n\n")
	}
}

func assertNoAccessControlHeaders(t *testing.T, h http.Header) {
	t.Helper()
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Fatalf("unexpected CORS header %s: %v", k, h.Values(k))
		}
	}
}

func headerContainsToken(values []string, target string) bool {
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), target) {
				return true
			}
		}
	}
	return false
}
