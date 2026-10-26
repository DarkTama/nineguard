package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nineguard/internal/auth"
	"nineguard/internal/db"
	"nineguard/internal/providerengine"
	"nineguard/internal/providers"
)

func TestProviderOAuthRequiresAdministrator(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	e, err := providerengine.New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterProviderOAuth(mux, e)
	// No dashboard session is never an administrator, even when a client API key is supplied.
	req := httptest.NewRequest("GET", "/api/v1/provider-connectors", nil)
	req.Header.Set("Authorization", "Bearer sk-ng-fake-key")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("client key authorized: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	operator := contextWithUser(httptest.NewRequest("GET", "/api/v1/provider-connectors", nil), &auth.User{ID: 2, Role: "operator"})
	mux.ServeHTTP(rec, operator)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator authorized: %d", rec.Code)
	}
	// Auth-disabled local deployments intentionally inject the existing dummy administrator.
	rec = httptest.NewRecorder()
	auth.NewManager(d, false).RequireAuthMiddleware(mux).ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/provider-connectors", nil))
	if rec.Code != 200 {
		t.Fatalf("local administrator: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential metadata may be cached")
	}
}

func TestOAuthProviderDeletionRequiresAdministrator(t *testing.T) {
	h, d := setupTestHandler(t)
	defer d.Close()
	backend, family := "cliproxy", "claude"
	p, err := h.providers.CreateProviderWithOptions(providers.ProviderOptions{Name: "Claude", Prefix: "claude", Backend: &backend, Family: &family, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	req := contextWithUser(httptest.NewRequest("DELETE", "/api/v1/providers/"+p.ID, nil), &auth.User{ID: 2, Role: "operator"})
	req.SetPathValue("id", p.ID)
	rec := httptest.NewRecorder()
	h.DeleteProvider(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator deleted account-backed provider: %d", rec.Code)
	}
}
