package conformance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// ed25519KeySize is the raw byte length of an ed25519 public key.
const ed25519KeySize = 32

// Ed25519Key fetches the target account's ed25519 public key from
// GET /v1/accounts/{id}: the account's meta carries base64-of-hex keys.
func Ed25519Key(tb testing.TB, target Target) ed25519.PublicKey {
	tb.Helper()

	status, raw := call(tb, target, http.MethodGet, "/v1/accounts/"+AccountID, "", map[string]string{
		"Authorization": "Bearer " + AdminToken(tb, target),
	})
	if status != http.StatusOK {
		tb.Fatalf("conformance: fetch account keys: unexpected status %d: %s", status, raw)
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		tb.Fatalf("conformance: fetch account keys: decode body %q: %v", raw, err)
	}

	encoded, found := lookup(doc, "data.meta.keys.ed25519")
	if !found {
		tb.Fatalf("conformance: fetch account keys: no data.meta.keys.ed25519 in %s", raw)
	}

	hexKey, err := base64.StdEncoding.DecodeString(fmt.Sprint(encoded))
	if err != nil {
		tb.Fatalf("conformance: fetch account keys: decode base64: %v", err)
	}

	key, err := hex.DecodeString(string(hexKey))
	if err != nil {
		tb.Fatalf("conformance: fetch account keys: decode hex: %v", err)
	}

	if len(key) != ed25519KeySize {
		tb.Fatalf("conformance: fetch account keys: want %d bytes, got %d", ed25519KeySize, len(key))
	}

	return ed25519.PublicKey(key)
}

// verifySignature checks the Keygen-Signature header (ed25519 only; the
// scenarios never request another algorithm) and the Digest header against
// the raw response body.
func verifySignature(
	tb testing.TB,
	method string,
	path string,
	host string,
	header http.Header,
	raw []byte,
	key ed25519.PublicKey,
) {
	tb.Helper()

	value := header.Get("Keygen-Signature")
	if value == "" {
		return
	}

	fields := parseSignatureHeader(value)

	if algorithm := fields["algorithm"]; algorithm != "ed25519" {
		tb.Errorf("conformance: %s %s: unsupported signature algorithm %q", method, path, algorithm)

		return
	}

	signature, err := base64.StdEncoding.DecodeString(fields["signature"])
	if err != nil {
		tb.Errorf("conformance: %s %s: decode signature: %v", method, path, err)

		return
	}

	signingString := signingString(method, path, host, fields["headers"], header)
	if !ed25519.Verify(key, []byte(signingString), signature) {
		tb.Errorf("conformance: %s %s: keygen signature does not verify:\n%s", method, path, signingString)
	}

	sum := sha256.Sum256(raw)
	if digest := header.Get("Digest"); digest != "sha-256="+base64.StdEncoding.EncodeToString(sum[:]) {
		tb.Errorf("conformance: %s %s: digest %q does not match the response body", method, path, digest)
	}
}

// parseSignatureHeader splits `keyid="…", algorithm="…", signature="…",
// headers="…"` into its quoted fields.
func parseSignatureHeader(value string) map[string]string {
	fields := map[string]string{}

	for _, match := range regexp.MustCompile(`([a-zA-Z-]+)="([^"]*)"`).FindAllStringSubmatch(value, -1) {
		fields[match[1]] = match[2]
	}

	return fields
}

// signingString rebuilds the signed data from the response headers the
// server said it signed: "(request-target)" uses the original request path
// (query included), "host" the request host without port.
func signingString(method, path, host, signed string, header http.Header) string {
	var lines []string

	for _, name := range strings.Fields(signed) {
		switch name {
		case "(request-target)":
			lines = append(lines, "(request-target): "+strings.ToLower(method)+" "+path)
		case "host":
			lines = append(lines, "host: "+host)
		default:
			lines = append(lines, name+": "+header.Get(name))
		}
	}

	return strings.Join(lines, "\n")
}
