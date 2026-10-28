package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ProviderKeyEncryption holds the process-local AES-256-GCM key for Shared API Keys.
type ProviderKeyEncryption struct {
	key []byte
}

func NewProviderKeyEncryption(key []byte) (*ProviderKeyEncryption, error) {
	if len(key) != 32 {
		return nil, errors.New("provider encryption key must be exactly 32 bytes")
	}
	copied := make([]byte, 32)
	copy(copied, key)
	return &ProviderKeyEncryption{key: copied}, nil
}

// LoadProviderEncryptionKeyFile reads a base64-encoded 32-byte key from file.
func LoadProviderEncryptionKeyFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("provider encryption key file encoding is invalid: %w", err)
	}
	if len(decoded) != 32 {
		return nil, errors.New("provider encryption key must decode to exactly 32 bytes")
	}
	return decoded, nil
}

func SharedAPIKeyAAD(publicID string, ownerUserID uint, serviceProvider string) []byte {
	return []byte(fmt.Sprintf("crynux-as:shared-api-key:%s:%d:%s", publicID, ownerUserID, serviceProvider))
}

func FingerprintAPIKey(plaintext []byte) string {
	sum := sha256.Sum256(plaintext)
	return hex.EncodeToString(sum[:])
}

func (e *ProviderKeyEncryption) Encrypt(plaintext []byte, aad []byte) (ciphertext, nonce []byte, err error) {
	if e == nil || len(e.key) != 32 {
		return nil, nil, errors.New("provider encryption key is not initialized")
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, aad)
	return ciphertext, nonce, nil
}

func (e *ProviderKeyEncryption) Decrypt(ciphertext, nonce, aad []byte) ([]byte, error) {
	if e == nil || len(e.key) != 32 {
		return nil, errors.New("provider encryption key is not initialized")
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid key nonce length")
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}

func ClearBytes(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}

var providerKeyEncryption *ProviderKeyEncryption

func SetProviderKeyEncryption(enc *ProviderKeyEncryption) {
	providerKeyEncryption = enc
}

func GetProviderKeyEncryption() *ProviderKeyEncryption {
	return providerKeyEncryption
}
