package tools

import (
	"crynux_as/blockchain"
	"errors"
	"time"
)

// SignatureValidator handles wallet signature validation for login
type SignatureValidator struct {
	verifier       *blockchain.SignatureVerifier
	timeoutSeconds int64
}

// NewSignatureValidator creates a new signature validator
func NewSignatureValidator(timeoutSeconds int64) *SignatureValidator {
	return &SignatureValidator{
		verifier:       blockchain.NewSignatureVerifier(),
		timeoutSeconds: timeoutSeconds,
	}
}

// ValidateTimestamp validates if the timestamp is within acceptable range
func (sv *SignatureValidator) ValidateTimestamp(timestamp int64) error {
	now := time.Now().Unix()

	// Allow some clock drift - accept timestamps from 1 minute ago to 1 minute in the future
	const clockDriftSeconds = 60

	if timestamp < now-sv.timeoutSeconds-clockDriftSeconds {
		return errors.New("signature timestamp too old")
	}

	if timestamp > now+clockDriftSeconds {
		return errors.New("signature timestamp too far in the future")
	}

	return nil
}

// GenerateLoginMessage generates the standard message for wallet login
func (sv *SignatureValidator) GenerateLoginMessage(address string, timestamp int64) string {
	return sv.verifier.GenerateSignMessage("Login", address, timestamp)
}

// ValidateAndRecoverAddress validates signature and returns the recovered address
func (sv *SignatureValidator) ValidateAndRecoverAddress(message, signature string, timestamp int64) (string, error) {
	if err := sv.ValidateTimestamp(timestamp); err != nil {
		return "", err
	}

	if err := sv.verifier.ValidateSignatureFormat(signature); err != nil {
		return "", err
	}

	recoveredAddress, err := sv.verifier.RecoverAddress(message, signature)
	if err != nil {
		return "", err
	}

	return recoveredAddress, nil
}

// Global signature validator instance
var DefaultSignatureValidator = NewSignatureValidator(60)

// GenerateLoginMessage generates the standard message for wallet login
func GenerateLoginMessage(address string, timestamp int64) string {
	return DefaultSignatureValidator.GenerateLoginMessage(address, timestamp)
}

// ValidateAndRecover validates signature and returns recovered address with default settings
func ValidateAndRecover(message, signature string, timestamp int64) (string, error) {
	return DefaultSignatureValidator.ValidateAndRecoverAddress(message, signature, timestamp)
}
