package providerengine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"nineguard/internal/db"
	"nineguard/internal/vault"
)

type creationGrant struct {
	providerID, accountID, family string
	version                       int64
	expires                       time.Time
	bound                         bool
	incarnation                   string
}
type credentialStore struct {
	db       *db.DB
	mu       sync.Mutex
	grants   map[string]*creationGrant
	creation map[string]string
}

func newCredentialStore(d *db.DB) *credentialStore {
	return &credentialStore{db: d, grants: map[string]*creationGrant{}, creation: map[string]string{}}
}
func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (s *credentialStore) newGrant(providerID, accountID, family string) (string, error) {
	var backend, storedFamily string
	if err := s.db.QueryRow(`SELECT backend,family FROM providers WHERE id=?`, providerID).Scan(&backend, &storedFamily); err != nil {
		return "", err
	}
	if backend != "cliproxy" || family != storedFamily {
		return "", errors.New("OAuth provider mismatch")
	}
	var version int64
	if accountID != "" {
		if err := s.db.QueryRow(`SELECT version FROM provider_accounts WHERE id=? AND provider_id=?`, accountID, providerID).Scan(&version); err != nil {
			return "", err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := randomID()
	s.grants[id] = &creationGrant{providerID: providerID, accountID: accountID, family: family, version: version, expires: time.Now().Add(15 * time.Minute)}
	return id, nil
}
func (s *credentialStore) bindGrant(id string, a *coreauth.Auth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.grants[id]
	if g == nil || g.bound || time.Now().After(g.expires) || a.Provider != g.family {
		return errors.New("OAuth login session is no longer valid")
	}
	if g.accountID == "" {
		g.accountID = randomID()
	}
	a.ID = g.accountID
	a.FileName = ""
	a.Prefix = "ng-" + a.ID
	attrs := map[string]string{}
	for k, v := range a.Attributes {
		attrs[k] = v
	}
	attrs["nineguard_provider"] = g.providerID
	g.incarnation = randomID()
	attrs["nineguard_login"] = g.incarnation
	a.Attributes = attrs
	g.bound = true
	s.creation[a.ID] = id
	return nil
}
func (s *credentialStore) cancelGrant(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.grants[id]; g != nil {
		delete(s.creation, g.accountID)
	}
	delete(s.grants, id)
}

func credentialPayload(a *coreauth.Auth) ([]byte, error) {
	clone := a.Clone()
	metadata := map[string]any{}
	if a.Storage != nil {
		raw, err := json.Marshal(a.Storage)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &metadata); err != nil {
			return nil, err
		}
	}
	// Runtime metadata is authoritative after token refresh. Initial login metadata contains only identity.
	for k, v := range a.Metadata {
		metadata[k] = v
	}
	metadata["type"] = a.Provider
	clone.Metadata = metadata
	clone.Storage = nil
	return json.Marshal(clone)
}
func (s *credentialStore) Save(ctx context.Context, a *coreauth.Auth) (string, error) {
	if a == nil || a.ID == "" {
		return "", errors.New("missing OAuth credential identity")
	}
	raw, err := credentialPayload(a)
	if err != nil {
		return "", err
	}
	encrypted, err := vault.Encrypt(string(raw))
	if err != nil {
		return "", err
	}
	providerID := a.Attributes["nineguard_provider"]
	if providerID == "" {
		return "", errors.New("credential has no provider binding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grantID := s.creation[a.ID]
	g := s.grants[grantID]
	creating := g != nil && g.bound && g.incarnation == a.Attributes["nineguard_login"] && time.Now().Before(g.expires)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var count int
	var version int64
	var disconnected bool
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(version),0),COALESCE(MAX(disconnected),0) FROM provider_accounts WHERE id=? AND provider_id=?`, a.ID, providerID).Scan(&count, &version, &disconnected); err != nil {
		return "", err
	}
	if creating {
		if version != g.version {
			return "", errors.New("OAuth login superseded by an account change")
		}
	} else if count == 0 || disconnected {
		return "", errors.New("OAuth account was disconnected")
	}
	var prevCV, prevGen, prevEpoch uint64
	if count > 0 && !creating {
		var previous string
		if err = tx.QueryRowContext(ctx, `SELECT credential_version,generation,registration_epoch,payload FROM provider_credentials WHERE account_id=?`, a.ID).Scan(&prevCV, &prevGen, &prevEpoch, &previous); err != nil {
			return "", err
		}
		plain, decryptErr := vault.Decrypt(previous)
		if decryptErr != nil {
			return "", decryptErr
		}
		var saved coreauth.Auth
		if err = json.Unmarshal([]byte(plain), &saved); err != nil {
			return "", err
		}
		if saved.Attributes["nineguard_login"] != a.Attributes["nineguard_login"] {
			return "", errors.New("OAuth login has been replaced")
		}
		if a.RegistrationEpoch < prevEpoch || (a.RegistrationEpoch == prevEpoch && (a.CredentialVersion < prevCV || (a.CredentialVersion == prevCV && a.Generation < prevGen))) {
			return "", errors.New("stale OAuth credential update")
		}
	}
	label, _ := a.Metadata["email"].(string)
	if label == "" {
		label = a.Label
	}
	if label == "" {
		label = a.Provider + " account"
	}
	status := "connected"
	if a.Disabled {
		status = "disabled"
	}
	lifecycleChange := 0
	if creating {
		lifecycleChange = 1
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_accounts(id,provider_id,family,label,status,version,disconnected) VALUES(?,?,?,?,?,1,0)
 ON CONFLICT(id) DO UPDATE SET label=excluded.label,status=excluded.status,version=provider_accounts.version+?,disconnected=0`, a.ID, providerID, a.Provider, label, status, lifecycleChange)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_credentials(account_id,payload,credential_version,generation,registration_epoch) VALUES(?,?,?,?,?)
 ON CONFLICT(account_id) DO UPDATE SET payload=excluded.payload,credential_version=excluded.credential_version,generation=excluded.generation,registration_epoch=excluded.registration_epoch`, a.ID, encrypted, a.CredentialVersion, a.Generation, a.RegistrationEpoch)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	if creating {
		delete(s.creation, a.ID)
		delete(s.grants, grantID)
	}
	// The SDK's post-persist hook must receive usable metadata too; Storage is not serialized by Auth.
	var saved coreauth.Auth
	if err = json.Unmarshal(raw, &saved); err != nil {
		return "", err
	}
	a.Metadata = saved.Metadata
	return "sqlite://provider-accounts/" + a.ID, nil
}
func (s *credentialStore) List(ctx context.Context) ([]*coreauth.Auth, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.payload FROM provider_credentials c JOIN provider_accounts a ON a.id=c.account_id JOIN providers p ON p.id=a.provider_id WHERE a.disconnected=0 AND p.backend='cliproxy'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*coreauth.Auth{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		plain, err := vault.Decrypt(payload)
		if err != nil {
			return nil, fmt.Errorf("decrypt OAuth credential: %w", err)
		}
		var a coreauth.Auth
		if err = json.Unmarshal([]byte(plain), &a); err != nil {
			return nil, errors.New("invalid encrypted OAuth credential")
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
func (s *credentialStore) Delete(ctx context.Context, id string) error {
	var providerID string
	if err := s.db.QueryRowContext(ctx, `SELECT provider_id FROM provider_accounts WHERE id=?`, id).Scan(&providerID); err != nil {
		return err
	}
	return s.disconnect(ctx, providerID, id)
}
func (s *credentialStore) disconnect(ctx context.Context, providerID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE provider_accounts SET disconnected=1,status='disconnected',version=version+1 WHERE id=? AND provider_id=?`, id, providerID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return errors.New("OAuth account not found")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM provider_credentials WHERE account_id=?`, id); err != nil {
		return err
	}
	delete(s.creation, id)
	return tx.Commit()
}
func (s *credentialStore) accounts(ctx context.Context, providerID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider_id,family,label,status FROM provider_accounts WHERE provider_id=? AND disconnected=0 ORDER BY label,id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if err = rows.Scan(&a.ID, &a.ProviderID, &a.Family, &a.Label, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
