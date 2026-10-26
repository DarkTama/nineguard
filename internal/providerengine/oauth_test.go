package providerengine

import (
	"context"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"net/http"
	"testing"
	"time"
)

func TestOAuthSessionOwnershipAndReplay(t *testing.T) {
	s, d := testStore(t)
	grant, err := s.newGrant("p", "", "claude")
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{db: d, store: s, sessions: map[string]*loginState{}}
	e.sessions["login"] = &loginState{LoginSession: LoginSession{ID: "login", ProviderID: "p", State: "pending", ExpiresAt: time.Now().Add(time.Minute)}, adminID: 1, family: "claude", grant: grant, sdkState: "correct"}
	for _, who := range []struct {
		admin    int64
		provider string
	}{{2, "p"}, {1, "other"}} {
		if _, err = e.LoginStatus(context.Background(), who.admin, who.provider, "login"); err == nil {
			t.Fatal("session ownership bypass")
		}
	}
	if err = e.SubmitCallback(context.Background(), 1, "p", "login", "http://localhost/callback?state=wrong&code=x"); err == nil {
		t.Fatal("wrong state accepted")
	}
	if err = e.CancelLogin(context.Background(), 1, "p", "login"); err != nil {
		t.Fatal(err)
	}
	info := &coreauth.RequestInfo{Headers: http.Header{sessionHeader: []string{"login"}}}
	ctx := coreauth.WithRequestInfo(context.Background(), info)
	if err = e.prepareLoginRecord(ctx, &coreauth.Auth{Provider: "claude"}); err == nil {
		t.Fatal("cancelled login attached account")
	}
}

func TestCancelLoginDoesNotClaimSuccessDuringCommit(t *testing.T) {
	s, d := testStore(t)
	e := &Engine{db: d, store: s, sessions: map[string]*loginState{"committing": {LoginSession: LoginSession{ID: "committing", ProviderID: "p", State: "pending", ExpiresAt: time.Now().Add(time.Minute)}, adminID: 1, committing: true}}}
	if err := e.CancelLogin(context.Background(), 1, "p", "committing"); err == nil {
		t.Fatal("reported cancellation while token commit is active")
	}
	if e.sessions["committing"].State != "pending" {
		t.Fatal("committing login was marked cancelled")
	}
}
