package pveweb

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"crypto/pbkdf2"
)

const (
	passwordAlgorithm  = "pbkdf2-sha256"
	passwordIterations = 600_000
	passwordSaltBytes  = 16
	passwordKeyBytes   = 32
)

// HashPassword returns a salted password verifier suitable for the local
// root-readable PowerCheck account file.
func HashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", fmt.Errorf("web password must contain at least 12 characters")
	}
	if len(password) > 1024 {
		return "", fmt.Errorf("web password must not exceed 1024 characters")
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password verifier: %w", err)
	}
	return fmt.Sprintf(
		"%s$%d$%s$%s",
		passwordAlgorithm,
		passwordIterations,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordAlgorithm {
		return false
	}
	if parts[1] != fmt.Sprintf("%d", passwordIterations) {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != passwordSaltBytes {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != passwordKeyBytes {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func validPasswordHash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 ||
		parts[0] != passwordAlgorithm ||
		parts[1] != fmt.Sprintf("%d", passwordIterations) {
		return false
	}
	salt, saltErr := base64.RawURLEncoding.DecodeString(parts[2])
	key, keyErr := base64.RawURLEncoding.DecodeString(parts[3])
	return saltErr == nil && keyErr == nil &&
		len(salt) == passwordSaltBytes && len(key) == passwordKeyBytes
}
