package providerengine

import (
	"context"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedEnginePublishesRestoredAccounts(t *testing.T) {
	s, d := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for range 2 {
		e, err := New(d, dir)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err = e.Start(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		models, err := e.Models(ctx, "p")
		if err != nil || len(models) == 0 {
			t.Fatalf("restored catalog: %d %v", len(models), err)
		}
		accounts, err := e.Accounts(ctx, "p")
		if err != nil || len(accounts) != 1 || accounts[0].ID != a.ID {
			t.Fatalf("accounts: %+v %v", accounts, err)
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = e.Close(closeCtx)
		closeCancel()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && filepath.Ext(path) == ".json" {
			t.Errorf("plaintext auth file created: %s", path)
		}
		return nil
	})
}
func TestEnginePrivateSurface(t *testing.T) {
	_, d := testStore(t)
	e, err := New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	resp, err := http.Get(e.address + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated engine status %d", resp.StatusCode)
	}
}

func TestAccountsReflectRuntimeInvalidGrant(t *testing.T) {
	s, d := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	e, err := New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	current, _ := e.manager.GetByID(a.ID)
	_, _, err = e.manager.Modify(context.Background(), a.ID, current.RegistrationEpoch, func(record *coreauth.Auth) error {
		record.LastError = &coreauth.Error{Code: "invalid_grant", HTTPStatus: 401, Message: "sensitive upstream response"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := e.Accounts(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if accounts[0].Status != "needs_login" || strings.Contains(accounts[0].LastError, "sensitive") {
		t.Fatalf("terminal account health not sanitized: %+v", accounts)
	}
}
