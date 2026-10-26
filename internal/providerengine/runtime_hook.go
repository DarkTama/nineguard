package providerengine

import (
	"context"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type accountRuntimeHook struct {
	coreauth.NoopHook
	engine *Engine
}

func (h accountRuntimeHook) OnAuthRegistered(ctx context.Context, a *coreauth.Auth) { h.check(ctx, a) }
func (h accountRuntimeHook) OnAuthUpdated(ctx context.Context, a *coreauth.Auth)    { h.check(ctx, a) }
func (h accountRuntimeHook) check(ctx context.Context, a *coreauth.Auth) {
	providerID := a.Attributes["nineguard_provider"]
	if h.engine.ownsAccount(ctx, providerID, a.ID) != nil {
		h.engine.manager.Remove(ctx, a.ID)
		cliproxy.GlobalModelRegistry().UnregisterClient(a.ID)
	}
	// Observe public-safe login state only after the persisted account is known to the manager.
	h.engine.sessionMu.Lock()
	defer h.engine.sessionMu.Unlock()
	for _, s := range h.engine.sessions {
		if s.AccountID == a.ID && s.committing && s.State == "pending" && time.Now().Before(s.ExpiresAt) {
			s.State = "connected"
		}
	}
}
