package utils

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateRandomToken returns a cryptographically random hex string of the
// given byte length, used for project endpoint tokens and API keys.
func GenerateRandomToken(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
