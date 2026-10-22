package crypto_test

import (
	"strings"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

func TestPasswordRubyBcryptCompatibility(t *testing.T) {
	t.Parallel()

	password := strings.Repeat("é", 40)

	digest, err := crypto.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(digest, "$2a$") || digest == password {
		t.Fatalf("not a bcrypt digest: %q", digest)
	}

	if !crypto.PasswordMatches(digest, password) {
		t.Fatal("original multibyte password rejected")
	}

	if !crypto.PasswordMatches(digest, strings.Repeat("é", 36)+"other") {
		t.Fatal("Ruby's 72-byte bcrypt input semantics differ")
	}

	if crypto.PasswordMatches(digest, "wrong-password") {
		t.Fatal("wrong password accepted")
	}
}
