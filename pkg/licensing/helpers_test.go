package licensing_test

import (
	"crypto/elliptic"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ellipticP256 is the P-256 curve.
func ellipticP256() elliptic.Curve {
	return elliptic.P256()
}

// pemBlock renders a PEM block.
func pemBlock(t *testing.T, blockType string, der []byte) string {
	t.Helper()

	var out strings.Builder

	if err := pem.Encode(&out, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatalf("pem encode: %v", err)
	}

	return out.String()
}

// readAll drains a request body.
func readAll(request *http.Request) ([]byte, error) {
	return io.ReadAll(request.Body)
}
