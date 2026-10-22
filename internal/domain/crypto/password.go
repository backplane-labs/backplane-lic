package crypto

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptMaxBytes is bcrypt's effective input size. Ruby bcrypt truncates
// longer byte sequences, including multibyte passwords whose character count
// still satisfies the reference's 72-character model validation.
const bcryptMaxBytes = 72

// HashPassword stores a Ruby-compatible bcrypt digest.
func HashPassword(password string) (string, error) {
	digest, err := bcrypt.GenerateFromPassword(passwordBytes(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt password: %w", err)
	}

	return string(digest), nil
}

// PasswordMatches compares with the same input semantics as Ruby bcrypt.
func PasswordMatches(digest, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(digest), passwordBytes(password)) == nil
}

func passwordBytes(password string) []byte {
	if len(password) > bcryptMaxBytes {
		password = password[:bcryptMaxBytes]
	}

	return []byte(password)
}
