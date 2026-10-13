package db_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/vault"
)

func TestVaultMigration_AutoEncryptsPlaintextSecrets(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_vault_migration.db")

	// 1. Manually create pre-existing database with plaintext credentials
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw sqlite: %v", err)
	}

	_, err = rawDB.Exec(`
		CREATE TABLE providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			route TEXT NOT NULL,
			api_key TEXT,
			prefix TEXT NOT NULL DEFAULT '',
			is_default INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			quota_type TEXT DEFAULT 'disabled',
			quota_auth_secret TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE plugins (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			url TEXT DEFAULT '',
			secret TEXT DEFAULT '',
			timeout_ms INTEGER DEFAULT 3000,
			failure_policy TEXT DEFAULT 'open',
			bypassable INTEGER DEFAULT 1,
			pipeline_order INTEGER DEFAULT 100,
			category TEXT NOT NULL DEFAULT 'other',
			summary TEXT DEFAULT '',
			default_settings TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		INSERT INTO providers (id, name, route, api_key, quota_auth_secret)
		VALUES ('p1', 'Legacy 9router', 'http://localhost:20128', 'sk-upstream-plaintext-123', 'dash-pass-plaintext-456');

		INSERT INTO settings (key, value)
		VALUES ('router_api_key', 'sk-router-plaintext-789');

		INSERT INTO plugins (id, kind, name, secret)
		VALUES ('plug1', 'http', 'Custom Guard', 'plug-secret-plaintext-999');
	`)
	if err != nil {
		t.Fatalf("failed to seed plaintext database: %v", err)
	}
	rawDB.Close()

	// 2. Now boot NineGuard via InitDB. It should automatically detect and encrypt all plaintext secrets in-place!
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	// 3. Inspect raw SQLite table rows to verify values on disk start with "enc:v1:"
	var storedKey, storedSecret string
	err = database.QueryRow("SELECT api_key, quota_auth_secret FROM providers WHERE id = 'p1'").Scan(&storedKey, &storedSecret)
	if err != nil {
		t.Fatalf("query providers failed: %v", err)
	}
	if !strings.HasPrefix(storedKey, vault.Prefix) {
		t.Fatalf("expected stored api_key to be encrypted with enc:v1:, got %q", storedKey)
	}
	if !strings.HasPrefix(storedSecret, vault.Prefix) {
		t.Fatalf("expected stored quota_auth_secret to be encrypted with enc:v1:, got %q", storedSecret)
	}

	var storedRouterKey string
	err = database.QueryRow("SELECT value FROM settings WHERE key = 'router_api_key'").Scan(&storedRouterKey)
	if err != nil {
		t.Fatalf("query settings failed: %v", err)
	}
	if !strings.HasPrefix(storedRouterKey, vault.Prefix) {
		t.Fatalf("expected stored router_api_key to be encrypted with enc:v1:, got %q", storedRouterKey)
	}

	var storedPlugSecret string
	err = database.QueryRow("SELECT secret FROM plugins WHERE id = 'plug1'").Scan(&storedPlugSecret)
	if err != nil {
		t.Fatalf("query plugins failed: %v", err)
	}
	if !strings.HasPrefix(storedPlugSecret, vault.Prefix) {
		t.Fatalf("expected stored plugin secret to be encrypted with enc:v1:, got %q", storedPlugSecret)
	}

	// 4. Verify that decrypting them yields the original plaintext secrets
	decKey, err := vault.Decrypt(storedKey)
	if err != nil || decKey != "sk-upstream-plaintext-123" {
		t.Fatalf("decrypted api_key mismatch: %v, %q", err, decKey)
	}

	decSecret, err := vault.Decrypt(storedSecret)
	if err != nil || decSecret != "dash-pass-plaintext-456" {
		t.Fatalf("decrypted quota_auth_secret mismatch: %v, %q", err, decSecret)
	}

	decRouterKey, err := vault.Decrypt(storedRouterKey)
	if err != nil || decRouterKey != "sk-router-plaintext-789" {
		t.Fatalf("decrypted router_api_key mismatch: %v, %q", err, decRouterKey)
	}

	decPlugSecret, err := vault.Decrypt(storedPlugSecret)
	if err != nil || decPlugSecret != "plug-secret-plaintext-999" {
		t.Fatalf("decrypted plugin secret mismatch: %v, %q", err, decPlugSecret)
	}
}
