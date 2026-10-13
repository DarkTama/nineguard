package upstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"nineguard/internal/providers"
	"nineguard/internal/upstream"
)

func TestRouter9Adapter_SuccessAndAuth(t *testing.T) {
	var loggedIn atomic.Bool
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		switch r.URL.Path {
		case "/api/auth/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] == "secret123" {
				loggedIn.Store(true)
				http.SetCookie(w, &http.Cookie{
					Name:  "auth_token",
					Value: "mock_jwt_token",
					Path:  "/",
				})
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success":true}`))
				return
			}
			http.Error(w, "invalid password", http.StatusUnauthorized)

		case "/api/providers/client":
			cookie, err := r.Cookie("auth_token")
			if err != nil || cookie.Value != "mock_jwt_token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"connections": [
					{
						"id": "c1",
						"provider": "anthropic",
						"displayName": "Claude Pro",
						"isActive": true
					}
				]
			}`))

		case "/api/usage/c1":
			cookie, err := r.Cookie("auth_token")
			if err != nil || cookie.Value != "mock_jwt_token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"plan": "Pro",
				"quotas": {
					"five_hour": {
						"used": 20,
						"total": 100,
						"remaining": 80,
						"remainingPercentage": 80,
						"displayName": "5h Session"
					},
					"seven_day": {
						"used": 800,
						"total": 1000,
						"remaining": 200,
						"remainingPercentage": 20,
						"displayName": "Weekly Limit"
					}
				}
			}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mgr := upstream.NewManager()
	prov := providers.Provider{
		ID:              "p-9r",
		Name:            "9router",
		Route:           server.URL + "/v1",
		QuotaType:       "9router",
		QuotaAuthSecret: "secret123",
	}

	res, err := mgr.GetProviderQuotas(context.Background(), prov, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected status ok, got %s: %s", res.Status, res.ErrorMsg)
	}
	if len(res.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(res.Connections))
	}

	conn := res.Connections[0]
	if conn.ConnectionID != "c1" || conn.AccountLabel != "Claude Pro" {
		t.Fatalf("unexpected connection: %+v", conn)
	}
	if len(conn.Buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(conn.Buckets))
	}

	// Verify warning flag on 20% bucket
	var weeklyBucket, sessionBucket *upstream.QuotaBucket
	for i := range conn.Buckets {
		if conn.Buckets[i].Name == "Weekly Limit" {
			weeklyBucket = &conn.Buckets[i]
		} else if conn.Buckets[i].Name == "5h Session" {
			sessionBucket = &conn.Buckets[i]
		}
	}
	if weeklyBucket == nil || !weeklyBucket.Warning {
		t.Fatalf("expected weekly bucket to have warning: %+v", weeklyBucket)
	}
	if sessionBucket == nil || sessionBucket.Warning {
		t.Fatalf("expected session bucket to not have warning: %+v", sessionBucket)
	}

	// Test caching: repeat call should not hit server again
	initialCalls := callCount.Load()
	cachedRes, err := mgr.GetProviderQuotas(context.Background(), prov, false)
	if err != nil || cachedRes == nil {
		t.Fatalf("cached call failed: %v", err)
	}
	if callCount.Load() != initialCalls {
		t.Fatalf("expected cached response without server hit, got calls %d vs %d", callCount.Load(), initialCalls)
	}

	// Test force bypass
	_, err = mgr.GetProviderQuotas(context.Background(), prov, true)
	if err != nil {
		t.Fatalf("force call failed: %v", err)
	}
	if callCount.Load() <= initialCalls {
		t.Fatalf("expected new calls after force=true")
	}
}

func TestNineGuardAdapter_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/quota" {
			authHdr := r.Header.Get("Authorization")
			if authHdr != "Bearer sk-ng-secret" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"key_quota": {
					"mode": "key",
					"limit": 1000000,
					"used": 400000,
					"remaining": 600000,
					"period": "daily",
					"warning": false
				},
				"upstream_pools": [
					{
						"provider": "9router",
						"family": "Claude",
						"buckets": [
							{
								"name": "5h Session",
								"used": 10,
								"total": 100,
								"remaining": 90,
								"remainingPercentage": 90,
								"warning": false
							}
						]
					}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mgr := upstream.NewManager()
	prov := providers.Provider{
		ID:        "p-ng",
		Name:      "Company NineGuard",
		Route:     server.URL,
		APIKey:    "sk-ng-secret",
		QuotaType: "nineguard",
	}

	res, err := mgr.GetProviderQuotas(context.Background(), prov, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected status ok, got %s", res.Status)
	}
	if len(res.Connections) != 2 { // 1 synthesized company budget + 1 pool
		t.Fatalf("expected 2 connections, got %d", len(res.Connections))
	}
}

func TestDisabledQuotaType(t *testing.T) {
	mgr := upstream.NewManager()
	prov := providers.Provider{
		ID:        "p-nano",
		Name:      "NanoGPT",
		Route:     "https://nano-gpt.com/api/v1",
		QuotaType: "disabled",
	}

	res, err := mgr.GetProviderQuotas(context.Background(), prov, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "unsupported" {
		t.Fatalf("expected unsupported status, got %s", res.Status)
	}
}
