package tools

import (
	"crynux_as/config"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims defines the JWT claims structure
type JWTClaims struct {
	Address string `json:"address"`
	jwt.RegisteredClaims
}

// JWTManager handles JWT token operations
type JWTManager struct {
	secretKey []byte
	expiresIn time.Duration
}

// NewJWTManager creates a new JWT manager
func NewJWTManager(secretKey string, expiresIn time.Duration) *JWTManager {
	return &JWTManager{
		secretKey: []byte(secretKey),
		expiresIn: expiresIn,
	}
}

// GenerateToken generates a new JWT token for the given address
func (jm *JWTManager) GenerateToken(address string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(jm.expiresIn)
	claims := &JWTClaims{
		Address: address,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "crynux-as",
			Subject:   address,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jm.secretKey)
	if err != nil {
		return "", time.Time{}, err
	}
	return tokenString, exp, nil
}

// ValidateToken validates and parses a JWT token
func (jm *JWTManager) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jm.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// ExtractAddressFromToken extracts the address from a JWT token
func (jm *JWTManager) ExtractAddressFromToken(tokenString string) (string, error) {
	claims, err := jm.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Address, nil
}

// Default JWT manager instance
var DefaultJWTManager *JWTManager

// InitializeJWTManager initializes the default JWT manager with configuration
func InitializeJWTManager() {
	appConfig := config.GetConfig()
	DefaultJWTManager = NewJWTManager(appConfig.Http.JWT.SecretKey, time.Duration(appConfig.Http.JWT.ExpiresIn)*time.Second)
}

// GenerateToken generates a JWT token with default settings
func GenerateToken(address string) (string, time.Time, error) {
	return DefaultJWTManager.GenerateToken(address)
}

// ValidateToken validates a JWT token with default settings
func ValidateToken(tokenString string) (*JWTClaims, error) {
	return DefaultJWTManager.ValidateToken(tokenString)
}

// ExtractAddressFromToken extracts address from token with default settings
func ExtractAddressFromToken(tokenString string) (string, error) {
	return DefaultJWTManager.ExtractAddressFromToken(tokenString)
}
