package providerengine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const sessionHeader = "X-NineGuard-OAuth-Session"

type loginState struct {
	LoginSession
	adminID                 int64
	family, grant, sdkState string
	submitted               bool
	committing              bool
}

func (e *Engine) Connectors() []Connector {
	return []Connector{
		{ID: "claude", Label: "Claude", Protocols: []string{"messages", "chat", "responses"}, LoginModes: []string{"browser"}, Icon: "claude"},
		{ID: "codex", Label: "OpenAI / Codex", Protocols: []string{"responses", "chat", "messages"}, LoginModes: []string{"browser"}, Icon: "openai"},
	}
}
func (e *Engine) prepareLoginRecord(ctx context.Context, a *coreauth.Auth) error {
	info := coreauth.GetRequestInfo(ctx)
	if info == nil {
		return errors.New("OAuth login ownership is missing")
	}
	id := info.Headers.Get(sessionHeader)
	e.sessionMu.Lock()
	defer e.sessionMu.Unlock()
	s := e.sessions[id]
	if s == nil || s.State != "pending" || time.Now().After(s.ExpiresAt) || a.Provider != s.family {
		return errors.New("OAuth login is expired or cancelled")
	}
	if err := e.store.bindGrant(s.grant, a); err != nil {
		return err
	}
	s.AccountID = a.ID
	s.committing = true
	return nil
}
func (e *Engine) StartLogin(ctx context.Context, adminID int64, providerID, accountID, mode string) (LoginSession, error) {
	var family string
	if err := e.db.QueryRowContext(ctx, `SELECT family FROM providers WHERE id=? AND backend='cliproxy'`, providerID).Scan(&family); err != nil {
		return LoginSession{}, errors.New("OAuth provider not found")
	}
	path := ""
	switch family {
	case "claude":
		path = "anthropic-auth-url"
	case "codex":
		path = "codex-auth-url"
	default:
		return LoginSession{}, errors.New("OAuth connector is not available")
	}
	if mode != "browser" {
		return LoginSession{}, errors.New("unsupported login mode")
	}
	grant, err := e.store.newGrant(providerID, accountID, family)
	if err != nil {
		return LoginSession{}, err
	}
	s := &loginState{LoginSession: LoginSession{ID: randomID(), ProviderID: providerID, AccountID: accountID, State: "pending", ExpiresAt: time.Now().Add(5 * time.Minute)}, adminID: adminID, family: family, grant: grant}
	e.sessionMu.Lock()
	for id, old := range e.sessions {
		if time.Now().After(old.ExpiresAt) {
			e.cancelSession(old)
			delete(e.sessions, id)
		}
	}
	e.sessions[s.ID] = s
	e.sessionMu.Unlock()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1/v0/management/"+path+"?is_webui=true", nil)
	if err != nil {
		return LoginSession{}, err
	}
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+e.managementSecret)
	req.Header.Set(sessionHeader, s.ID)
	raw, err := e.manageRequest(req)
	if err != nil {
		e.CancelLogin(ctx, adminID, providerID, s.ID)
		return LoginSession{}, err
	}
	var response struct {
		URL   string `json:"url"`
		State string `json:"state"`
	}
	if err = json.Unmarshal(raw, &response); err != nil || response.URL == "" || response.State == "" {
		e.CancelLogin(ctx, adminID, providerID, s.ID)
		return LoginSession{}, errors.New("OAuth provider did not return an authorization URL")
	}
	e.sessionMu.Lock()
	s.AuthorizationURL = response.URL
	s.sdkState = response.State
	out := s.LoginSession
	e.sessionMu.Unlock()
	return out, nil
}
func (e *Engine) session(adminID int64, providerID, id string) (*loginState, error) {
	s := e.sessions[id]
	if s == nil || s.adminID != adminID || s.ProviderID != providerID {
		return nil, errors.New("OAuth session not found")
	}
	if s.State == "pending" && time.Now().After(s.ExpiresAt) {
		e.cancelSession(s)
		s.State = "expired"
	}
	return s, nil
}
func (e *Engine) LoginStatus(ctx context.Context, adminID int64, providerID, id string) (LoginSession, error) {
	e.sessionMu.Lock()
	s, err := e.session(adminID, providerID, id)
	if err != nil {
		e.sessionMu.Unlock()
		return LoginSession{}, err
	}
	state := s.sdkState
	out := s.LoginSession
	e.sessionMu.Unlock()
	if out.State != "pending" || state == "" {
		return out, nil
	}
	raw, err := e.manage(ctx, "GET", "get-auth-status?state="+url.QueryEscape(state), nil)
	if err != nil {
		return out, err
	}
	var status struct {
		Status string `json:"status"`
	}
	if err = json.Unmarshal(raw, &status); err != nil {
		return out, err
	}
	e.sessionMu.Lock()
	defer e.sessionMu.Unlock()
	if s.State != "pending" {
		return s.LoginSession, nil
	}
	switch status.Status {
	case "ok":
		if s.AccountID != "" {
			if _, ok := e.manager.GetByID(s.AccountID); ok {
				s.State = "connected"
			}
		}
	case "error":
		s.State = "failed"
		s.Error = "Provider login failed. Start a new login."
		e.store.cancelGrant(s.grant)
	}
	return s.LoginSession, nil
}
func (e *Engine) SubmitCallback(ctx context.Context, adminID int64, providerID, id, callbackURL string) error {
	u, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("paste the complete callback URL returned after authorization")
	}
	e.sessionMu.Lock()
	s, err := e.session(adminID, providerID, id)
	if err != nil {
		e.sessionMu.Unlock()
		return err
	}
	if s.State != "pending" || s.submitted || u.Query().Get("state") != s.sdkState {
		e.sessionMu.Unlock()
		return errors.New("callback does not match a pending login")
	}
	if u.Query().Get("code") == "" && u.Query().Get("error") == "" {
		e.sessionMu.Unlock()
		return errors.New("callback has no authorization result")
	}
	state, family := s.sdkState, s.family
	s.submitted = true
	e.sessionMu.Unlock()
	if family == "claude" {
		family = "anthropic"
	}
	_, err = e.manage(ctx, "POST", "oauth-callback", map[string]string{"provider": family, "state": state, "code": u.Query().Get("code"), "error": u.Query().Get("error")})
	return err
}
func (e *Engine) cancelSession(s *loginState) {
	if s.State != "pending" {
		return
	}
	s.State = "cancelled"
	e.store.cancelGrant(s.grant)
	if s.sdkState != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.manage(ctx, "DELETE", "oauth-session?state="+url.QueryEscape(s.sdkState), nil)
	}
}
func (e *Engine) CancelLogin(_ context.Context, adminID int64, providerID, id string) error {
	e.sessionMu.Lock()
	defer e.sessionMu.Unlock()
	s, err := e.session(adminID, providerID, id)
	if err != nil {
		return err
	}
	if s.committing {
		return errors.New("authorization is being saved; wait for login status before disconnecting the account")
	}
	e.cancelSession(s)
	return nil
}
func (e *Engine) Disconnect(ctx context.Context, providerID, accountID string) error {
	e.sessionMu.Lock()
	for _, s := range e.sessions {
		if s.ProviderID == providerID && s.AccountID == accountID {
			e.cancelSession(s)
		}
	}
	e.sessionMu.Unlock()
	if err := e.store.disconnect(ctx, providerID, accountID); err != nil {
		return err
	}
	if e.manager != nil {
		e.manager.Remove(ctx, accountID)
	}
	cliproxy.GlobalModelRegistry().UnregisterClient(accountID)
	return nil
}
func (e *Engine) Refresh(ctx context.Context, providerID, accountID string) error {
	if err := e.ownsAccount(ctx, providerID, accountID); err != nil {
		return err
	}
	_, err := e.manager.ForceRefreshAuth(ctx, accountID)
	if err != nil {
		status := "connected"
		if strings.Contains(strings.ToLower(err.Error()), "invalid_grant") || strings.Contains(strings.ToLower(err.Error()), "revoked") {
			status = "needs_login"
		}
		e.db.ExecContext(ctx, `UPDATE provider_accounts SET status=? WHERE id=? AND provider_id=? AND disconnected=0`, status, accountID, providerID)
		return errors.New("token refresh failed; reconnect if the grant has expired")
	}
	return nil
}
func (e *Engine) ownsAccount(ctx context.Context, providerID, accountID string) error {
	var n int
	err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_accounts WHERE id=? AND provider_id=? AND disconnected=0`, accountID, providerID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("OAuth account not found")
	}
	return nil
}
func (e *Engine) SetDisabled(ctx context.Context, providerID, accountID string, disabled bool) error {
	if err := e.ownsAccount(ctx, providerID, accountID); err != nil {
		return err
	}
	_, err := e.manage(ctx, "PATCH", "auth-files/status", map[string]any{"name": accountID, "disabled": disabled})
	return err
}

// RemoveProvider invalidates login sessions before removing all locally stored grants.
func (e *Engine) RemoveProvider(ctx context.Context, providerID string) error {
	e.sessionMu.Lock()
	for _, s := range e.sessions {
		if s.ProviderID == providerID {
			e.cancelSession(s)
		}
	}
	e.sessionMu.Unlock()
	accounts, err := e.store.accounts(ctx, providerID)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if err = e.Disconnect(ctx, providerID, a.ID); err != nil {
			return err
		}
	}
	return nil
}
