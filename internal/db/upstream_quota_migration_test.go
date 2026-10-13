package db_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
)

func TestUpstreamQuotaMigration(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_upstream_migration.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer database.Close()

	// Verify columns exist on providers table
	var quotaType, quotaAuthSecret string
	err = database.QueryRow(`
		SELECT quota_type, quota_auth_secret FROM providers LIMIT 1
	`).Scan(&quotaType, &quotaAuthSecret)
	// QueryRow will return sql.ErrNoRows if table is empty, which is fine, but no "no such column" error
	if err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("querying quota_type and quota_auth_secret failed: %v", err)
	}

	// Verify chat_commands_enabled default setting exists
	var chatCmds string
	err = database.QueryRow(`
		SELECT value FROM settings WHERE key = 'chat_commands_enabled'
	`).Scan(&chatCmds)
	if err != nil {
		t.Fatalf("expected chat_commands_enabled in settings, got: %v", err)
	}
	if chatCmds != "true" {
		t.Fatalf("expected chat_commands_enabled to be 'true', got %q", chatCmds)
	}
}
