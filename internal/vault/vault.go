package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const Prefix = "enc:v1:"

var (
	mu      sync.RWMutex
	gcmInst cipher.AEAD
)

// SetMasterKeyExplicit sets the master key directly (used for tests).
func SetMasterKeyExplicit(key []byte) error {
	if len(key) != 32 {
		return errors.New("master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	mu.Lock()
	gcmInst = gcm
	mu.Unlock()
	return nil
}

// Init resolves or generates the 32-byte master key.
// Resolution hierarchy:
// 1. Env NINEGUARD_ENCRYPTION_KEY (64-char hex, 44-char base64, or 32 raw bytes).
// 2. Existing key file: dir/.master.key.
// 3. Auto-generation: 32 random bytes written to dir/.master.key with mode 0600.
func Init(dir string) error {
	// 1. Check environment variable
	if envKey := strings.TrimSpace(os.Getenv("NINEGUARD_ENCRYPTION_KEY")); envKey != "" {
		key, err := parseKeyString(envKey)
		if err != nil {
			return fmt.Errorf("invalid NINEGUARD_ENCRYPTION_KEY: %w", err)
		}
		return SetMasterKeyExplicit(key)
	}

	// In-memory mode (e.g. :memory: tests without disk dir)
	if dir == ":memory:" {
		newKey := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
			return fmt.Errorf("failed to generate random master key: %w", err)
		}
		return SetMasterKeyExplicit(newKey)
	}

	if dir == "" || dir == "." {
		dir = "data"
	}

	keyFile := filepath.Join(dir, ".master.key")
	if data, err := os.ReadFile(keyFile); err == nil && len(data) > 0 {
		key, err := parseKeyString(strings.TrimSpace(string(data)))
		if err == nil && len(key) == 32 {
			return SetMasterKeyExplicit(key)
		}
	}

	// Generate new 32-byte random key
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		return fmt.Errorf("failed to generate random master key: %w", err)
	}

	_ = os.MkdirAll(dir, 0700)
	hexKey := hex.EncodeToString(newKey)
	if err := os.WriteFile(keyFile, []byte(hexKey), 0600); err != nil {
		// If writing fails (e.g. read-only filesystem or ephemeral test), still set key in memory
		return SetMasterKeyExplicit(newKey)
	}

	return SetMasterKeyExplicit(newKey)
}

func parseKeyString(s string) ([]byte, error) {
	// Try hex
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	// Try base64
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// Raw string
	if len(s) == 32 {
		return []byte(s), nil
	}
	return nil, errors.New("key must be 64-char hex, base64 (32 bytes), or 32 raw bytes")
}

func getGCM() (cipher.AEAD, error) {
	mu.RLock()
	inst := gcmInst
	mu.RUnlock()
	if inst != nil {
		return inst, nil
	}

	// Auto-fallback: lazy init in-memory ephemeral key if Init() was never called
	mu.Lock()
	defer mu.Unlock()
	if gcmInst != nil {
		return gcmInst, nil
	}
	ephemeral := make([]byte, 32)
	_, _ = rand.Read(ephemeral)
	block, err := aes.NewCipher(ephemeral)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	gcmInst = gcm
	return gcmInst, nil
}

// Encrypt encrypts a plaintext string with AES-256-GCM.
// Returns "enc:v1:<base64(nonce || ciphertext)>".
// If input is empty or already starts with "enc:v1:", returns it unmodified.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" || strings.HasPrefix(plaintext, Prefix) {
		return plaintext, nil
	}

	gcm, err := getGCM()
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return Prefix + base64.StdEncoding.EncodeToString(payload), nil
}

// Decrypt decrypts an "enc:v1:..." ciphertext with AES-256-GCM.
// If input does not have "enc:v1:" prefix, returns input as legacy plaintext.
func Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" || !strings.HasPrefix(ciphertext, Prefix) {
		return ciphertext, nil
	}

	gcm, err := getGCM()
	if err != nil {
		return "", err
	}

	rawB64 := strings.TrimPrefix(ciphertext, Prefix)
	payload, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return "", fmt.Errorf("invalid base64 in encrypted secret: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return "", errors.New("encrypted secret payload too short")
	}

	nonce := payload[:nonceSize]
	encData := payload[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encData, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (authentication tag mismatch): %w", err)
	}

	return string(plaintext), nil
}
