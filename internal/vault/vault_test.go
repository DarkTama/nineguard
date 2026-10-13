package vault_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nineguard/internal/vault"
)

func TestVault_EncryptDecryptRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	if err := vault.Init(tmpDir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	secret := "sk-ant-api03-test-secret-key-12345"
	encrypted, err := vault.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !strings.HasPrefix(encrypted, "enc:v1:") {
		t.Fatalf("expected enc:v1: prefix, got: %s", encrypted)
	}
	if encrypted == secret {
		t.Fatalf("encrypted string matches plaintext")
	}

	// Repeated encrypt produces different ciphertexts due to random nonce
	encrypted2, err := vault.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}
	if encrypted == encrypted2 {
		t.Fatalf("expected different ciphertexts for repeated encryption (nonce randomization)")
	}

	// Double encrypt returns unmodified
	doubleEnc, err := vault.Encrypt(encrypted)
	if err != nil || doubleEnc != encrypted {
		t.Fatalf("expected double encrypt to return same string: %v", err)
	}

	decrypted, err := vault.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != secret {
		t.Fatalf("expected %q, got %q", secret, decrypted)
	}
}

func TestVault_LegacyPlaintextFallback(t *testing.T) {
	legacy := "unencrypted-legacy-key"
	decrypted, err := vault.Decrypt(legacy)
	if err != nil {
		t.Fatalf("Decrypt failed on legacy plaintext: %v", err)
	}
	if decrypted != legacy {
		t.Fatalf("expected legacy plaintext unchanged, got %q", decrypted)
	}
}

func TestVault_EmptyString(t *testing.T) {
	enc, err := vault.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("expected empty string encrypt: %v", err)
	}
	dec, err := vault.Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("expected empty string decrypt: %v", err)
	}
}

func TestVault_EnvOverride(t *testing.T) {
	customHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	os.Setenv("NINEGUARD_ENCRYPTION_KEY", customHex)
	defer os.Unsetenv("NINEGUARD_ENCRYPTION_KEY")

	if err := vault.Init(t.TempDir()); err != nil {
		t.Fatalf("Init with env failed: %v", err)
	}

	enc, err := vault.Encrypt("my-secret")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	dec, err := vault.Decrypt(enc)
	if err != nil || dec != "my-secret" {
		t.Fatalf("Decrypt failed: %v", err)
	}
}

func TestVault_MasterKeyFilePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	if err := vault.Init(tmpDir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	keyFile := filepath.Join(tmpDir, ".master.key")
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("expected .master.key to exist: %v", err)
	}
	// Check mode (on Unix, should be 0600)
	if info.Mode().Perm() != 0600 && os.PathSeparator == '/' {
		t.Fatalf("expected 0600 mode on .master.key, got %o", info.Mode().Perm())
	}

	// Encrypt something with first Init
	enc, err := vault.Encrypt("persistent-secret")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Re-init with same directory (simulating restart)
	if err := vault.Init(tmpDir); err != nil {
		t.Fatalf("Re-init failed: %v", err)
	}

	dec, err := vault.Decrypt(enc)
	if err != nil || dec != "persistent-secret" {
		t.Fatalf("Decrypt failed after re-init: %v", err)
	}
}
