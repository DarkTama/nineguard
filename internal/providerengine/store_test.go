package providerengine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"nineguard/internal/db"
	"nineguard/internal/vault"
)

type testTokenStorage struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Expired string `json:"expired"`
}

func (*testTokenStorage) SaveTokenToFile(string) error {
	panic("plaintext token persistence forbidden")
}

func testStore(t *testing.T) (*credentialStore, *db.DB) {
	t.Helper()
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err = vault.SetMasterKeyExplicit(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	_, err = d.Exec(`INSERT INTO providers(id,name,route,prefix,backend,family) VALUES ('p','Claude','','p','cliproxy','claude')`)
	if err != nil {
		t.Fatal(err)
	}
	return newCredentialStore(d), d
}
func grantRecord(t *testing.T, s *credentialStore) *coreauth.Auth {
	t.Helper()
	grant, err := s.newGrant("p", "", "claude")
	if err != nil {
		t.Fatal(err)
	}
	a := &coreauth.Auth{ID: "upstream-user.json", Provider: "claude", Storage: &testTokenStorage{Access: "fake-access-secret", Refresh: "fake-refresh-secret", Expired: time.Now().Add(time.Hour).Format(time.RFC3339)}, Metadata: map[string]any{"email": "user@example.test"}}
	if err = s.bindGrant(grant, a); err != nil {
		t.Fatal(err)
	}
	return a
}
func TestCredentialStoreEncryptedRestartAndStoragePayload(t *testing.T) {
	s, d := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	var ciphertext string
	if err := d.QueryRow(`SELECT payload FROM provider_credentials WHERE account_id=?`, a.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ciphertext, vault.Prefix) || strings.Contains(ciphertext, "fake-") {
		t.Fatal("credentials are not encrypted")
	}
	restored, err := newCredentialStore(d).List(context.Background())
	if err != nil || len(restored) != 1 {
		t.Fatalf("restore: %v %v", restored, err)
	}
	if restored[0].Metadata["access_token"] != "fake-access-secret" || restored[0].Metadata["refresh_token"] != "fake-refresh-secret" {
		t.Fatalf("Storage payload lost: %v", restored[0].Metadata)
	}
	accounts, err := s.accounts(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(accounts)
	if strings.Contains(string(raw), "fake-") {
		t.Fatal("DTO leaked credential")
	}
	_, err = d.Exec(`UPDATE provider_credentials SET payload='enc:v1:broken'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.List(context.Background()); err == nil {
		t.Fatal("corrupted credentials silently accepted")
	}
}
func TestCredentialStoreDisconnectAndVersionFences(t *testing.T) {
	s, _ := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	id := a.ID
	a.CredentialVersion = 2
	a.Generation = 2
	a.Metadata["access_token"] = "rotated-secret"
	a.Storage = nil
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	stale := a.Clone()
	stale.CredentialVersion = 1
	stale.Generation = 1
	if _, err := s.Save(context.Background(), stale); err == nil {
		t.Fatal("stale update accepted")
	}
	if err := s.disconnect(context.Background(), "p", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(context.Background(), a); err == nil {
		t.Fatal("late refresh resurrected account")
	}
	grant, err := s.newGrant("p", id, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.bindGrant(grant, a); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if a.ID != id {
		t.Fatal("reconnect changed identity")
	}
	if err = s.disconnect(context.Background(), "p", id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(context.Background(), a); err == nil {
		t.Fatal("creation grant replayed")
	}
}

func TestCredentialStoreNewRegistrationAllowsGenerationReset(t *testing.T) {
	s, _ := testStore(t)
	a := grantRecord(t, s)
	a.RegistrationEpoch, a.CredentialVersion, a.Generation = 1, 2, 40
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	a.RegistrationEpoch, a.CredentialVersion, a.Generation = 2, 3, 1
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatalf("new SDK registration rejected: %v", err)
	}
}

func TestCredentialStoreRejectsPriorLoginAfterReconnect(t *testing.T) {
	s, _ := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	old := a.Clone()
	if err := s.disconnect(context.Background(), "p", a.ID); err != nil {
		t.Fatal(err)
	}
	grant, err := s.newGrant("p", a.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.bindGrant(grant, a); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	old.RegistrationEpoch, old.CredentialVersion, old.Generation = 99, 99, 99
	if _, err = s.Save(context.Background(), old); err == nil {
		t.Fatal("previous login refresh overwrote reconnected credential")
	}
}

func TestCredentialStoreRefreshCannotConsumePendingLoginGrant(t *testing.T) {
	s, _ := testStore(t)
	old := grantRecord(t, s)
	if _, err := s.Save(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	grant, err := s.newGrant("p", old.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	login := old.Clone()
	if err = s.bindGrant(grant, login); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(context.Background(), login); err != nil {
		t.Fatalf("refresh consumed pending login: %v", err)
	}
}
