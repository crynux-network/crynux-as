package blockchain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// SignatureVerifier handles blockchain signature verification
type SignatureVerifier struct{}

// NewSignatureVerifier creates a new signature verifier instance
func NewSignatureVerifier() *SignatureVerifier {
	return &SignatureVerifier{}
}

// VerifySignature verifies a blockchain signature against a message and expected address
func (v *SignatureVerifier) VerifySignature(message, signature, expectedAddress string) error {
	recoveredAddress, err := v.RecoverAddress(message, signature)
	if err != nil {
		return fmt.Errorf("failed to recover address from signature: %w", err)
	}

	if !strings.EqualFold(recoveredAddress, expectedAddress) {
		return errors.New("signature address mismatch")
	}

	return nil
}

// GenerateSignMessage generates a standard message for signing
func (v *SignatureVerifier) GenerateSignMessage(action, address string, timestamp int64) string {
	return fmt.Sprintf("Crynux AS\nAction: %s\nAddress: %s\nTimestamp: %d",
		action, address, timestamp)
}

// RecoverAddress recovers the Ethereum address from a message and signature
func (v *SignatureVerifier) RecoverAddress(message, signature string) (string, error) {
	signature = strings.TrimPrefix(signature, "0x")

	sigBytes, err := hexutil.Decode("0x" + signature)
	if err != nil {
		return "", fmt.Errorf("invalid signature format: %w", err)
	}

	if len(sigBytes) != 65 {
		return "", errors.New("signature must be 65 bytes")
	}

	// Ethereum signatures have recovery ID in the last byte
	// Convert recovery ID from 27/28 to 0/1 if necessary
	if sigBytes[64] == 27 || sigBytes[64] == 28 {
		sigBytes[64] -= 27
	}

	messageHash := v.hashPersonalMessage(message)

	publicKey, err := crypto.SigToPub(messageHash, sigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to recover public key: %w", err)
	}

	address := crypto.PubkeyToAddress(*publicKey)
	return address.Hex(), nil
}

// hashPersonalMessage creates the hash that Ethereum wallets sign
// This follows the "\x19Ethereum Signed Message:\n" + len(message) + message format
func (v *SignatureVerifier) hashPersonalMessage(message string) []byte {
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	return crypto.Keccak256([]byte(prefix + message))
}

// ValidateAddress checks if an address is a valid Ethereum address
func (v *SignatureVerifier) ValidateAddress(address string) error {
	if !common.IsHexAddress(address) {
		return errors.New("invalid Ethereum address format")
	}
	return nil
}

// ValidateSignatureFormat checks if a signature has the correct format
func (v *SignatureVerifier) ValidateSignatureFormat(signature string) error {
	signature = strings.TrimPrefix(signature, "0x")

	if len(signature) != 130 {
		return errors.New("signature must be 130 hex characters (65 bytes)")
	}

	_, err := hexutil.Decode("0x" + signature)
	if err != nil {
		return fmt.Errorf("signature contains invalid hex characters: %w", err)
	}

	return nil
}

// SignMessage signs a message with a private key (for testing purposes)
func (v *SignatureVerifier) SignMessage(message, privateKeyHex string) (string, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %w", err)
	}

	messageHash := v.hashPersonalMessage(message)

	signature, err := crypto.Sign(messageHash, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign message: %w", err)
	}

	// Convert recovery ID to 27/28 format (standard for Ethereum)
	signature[64] += 27

	return hexutil.Encode(signature), nil
}
