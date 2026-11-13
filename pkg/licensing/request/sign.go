package request

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/yaronf/httpsign"
)

// sigLabel is the signature label used in the Signature and
// Signature-Input headers.
const sigLabel = "sig1"

// algEd25519 is the RFC 9421 algorithm name for Ed25519: the only
// algorithm this package signs with and accepts.
const algEd25519 = "ed25519"

// headerContentDigest is the RFC 9530 digest header covered by the
// signature for requests with a body.
const headerContentDigest = "Content-Digest"

// nonceSize is the byte length of the random signature nonce.
const nonceSize = 16

// signedFields returns the covered components: "@method" and
// "@target-uri" always, plus "content-digest" when the header is
// present. Marking content-digest optional makes the signer cover it
// exactly for requests with a body, and makes the verifier require it
// to be covered whenever the header appears — stripping the header or
// adding an unsigned one fails verification.
func signedFields() httpsign.Fields {
	return *httpsign.NewFields().
		AddHeaders("@method", "@target-uri").
		AddHeaderOptional(headerContentDigest)
}

// generateNonce produces a cryptographically random nonce for
// signature freshness, base64url-encoded without padding.
func generateNonce() (string, error) {
	buf := make([]byte, nonceSize)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// readBody reads the request body up to maxBytes and replaces it with a
// buffer so downstream handlers can read it again. It returns the
// number of bytes read. A body larger than maxBytes is refused with
// ErrBodyTooLarge without being fully buffered.
func readBody(r *http.Request, maxBytes int64) (int, error) {
	if r.Body == nil || r.Body == http.NoBody {
		r.Body = http.NoBody

		return 0, nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return 0, fmt.Errorf("read request body: %w", err)
	}

	if int64(len(body)) > maxBytes {
		return 0, ErrBodyTooLarge
	}

	if err := r.Body.Close(); err != nil {
		return 0, fmt.Errorf("close request body: %w", err)
	}

	r.Body = io.NopCloser(bytes.NewReader(body))

	return len(body), nil
}
