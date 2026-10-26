package providerengine

import (
	"context"
	"testing"
	"time"
)

func TestProviderRemovalCancelsLoginAndDisconnectsAccounts(t *testing.T) {
	s, d := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	grant, err := s.newGrant("p", "", "claude")
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{db: d, store: s, sessions: map[string]*loginState{"pending": {LoginSession: LoginSession{ID: "pending", ProviderID: "p", State: "pending", ExpiresAt: time.Now().Add(time.Minute)}, grant: grant}}}
	if err = e.RemoveProvider(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if e.sessions["pending"].State != "cancelled" {
		t.Fatal("provider login kept alive")
	}
	accounts, err := s.accounts(context.Background(), "p")
	if err != nil || len(accounts) != 0 {
		t.Fatalf("accounts remain: %v %v", accounts, err)
	}
	if _, err = s.Save(context.Background(), a); err == nil {
		t.Fatal("removed provider account resurrected")
	}
}
