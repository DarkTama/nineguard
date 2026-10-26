package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"nineguard/internal/auth"
	"nineguard/internal/providerengine"
)

// RegisterProviderOAuth exposes only owner-bound operations, never the SDK's management API.
func RegisterProviderOAuth(mux *http.ServeMux, e *providerengine.Engine) {
	admin := func(fn func(http.ResponseWriter, *http.Request, int64)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u := auth.UserFromContext(r.Context())
			if u == nil || u.Role != "admin" {
				jsonError(w, http.StatusForbidden, "Only administrators can manage provider accounts")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			fn(w, r, u.ID)
		}
	}
	failed := func(w http.ResponseWriter, err error) { jsonError(w, http.StatusBadRequest, err.Error()) }
	mux.HandleFunc("GET /api/v1/provider-connectors", admin(func(w http.ResponseWriter, r *http.Request, _ int64) {
		jsonResponse(w, 200, map[string]any{"connectors": e.Connectors()})
	}))
	mux.HandleFunc("GET /api/v1/providers/{id}/accounts", admin(func(w http.ResponseWriter, r *http.Request, _ int64) {
		a, err := e.Accounts(r.Context(), r.PathValue("id"))
		if err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"accounts": a})
	}))
	mux.HandleFunc("POST /api/v1/providers/{id}/oauth/sessions", admin(func(w http.ResponseWriter, r *http.Request, uid int64) {
		var b struct {
			AccountID string `json:"account_id"`
			Mode      string `json:"mode"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
			jsonError(w, 400, "Invalid login request")
			return
		}
		if b.Mode == "" {
			b.Mode = "browser"
		}
		s, err := e.StartLogin(r.Context(), uid, r.PathValue("id"), b.AccountID, b.Mode)
		if err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, s)
	}))
	mux.HandleFunc("GET /api/v1/providers/{id}/oauth/sessions/{sessionId}", admin(func(w http.ResponseWriter, r *http.Request, uid int64) {
		s, err := e.LoginStatus(r.Context(), uid, r.PathValue("id"), r.PathValue("sessionId"))
		if err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, s)
	}))
	mux.HandleFunc("POST /api/v1/providers/{id}/oauth/sessions/{sessionId}/callback", admin(func(w http.ResponseWriter, r *http.Request, uid int64) {
		var b struct {
			URL string `json:"callback_url"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
			jsonError(w, 400, "Invalid callback")
			return
		}
		if err := e.SubmitCallback(r.Context(), uid, r.PathValue("id"), r.PathValue("sessionId"), strings.TrimSpace(b.URL)); err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "pending"})
	}))
	mux.HandleFunc("DELETE /api/v1/providers/{id}/oauth/sessions/{sessionId}", admin(func(w http.ResponseWriter, r *http.Request, uid int64) {
		if err := e.CancelLogin(r.Context(), uid, r.PathValue("id"), r.PathValue("sessionId")); err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "cancelled"})
	}))
	mux.HandleFunc("POST /api/v1/providers/{id}/accounts/{accountId}/refresh", admin(func(w http.ResponseWriter, r *http.Request, _ int64) {
		if err := e.Refresh(r.Context(), r.PathValue("id"), r.PathValue("accountId")); err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("DELETE /api/v1/providers/{id}/accounts/{accountId}", admin(func(w http.ResponseWriter, r *http.Request, _ int64) {
		if err := e.Disconnect(r.Context(), r.PathValue("id"), r.PathValue("accountId")); err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("PATCH /api/v1/providers/{id}/accounts/{accountId}", admin(func(w http.ResponseWriter, r *http.Request, _ int64) {
		var b struct {
			Disabled *bool `json:"disabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil || b.Disabled == nil {
			jsonError(w, 400, "disabled is required")
			return
		}
		if err := e.SetDisabled(r.Context(), r.PathValue("id"), r.PathValue("accountId"), *b.Disabled); err != nil {
			failed(w, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ok"})
	}))
}
