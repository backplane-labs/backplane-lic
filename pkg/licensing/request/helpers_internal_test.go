package request

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yaronf/httpsign"

	"github.com/backplane-labs/backplane-lic/pkg/licensing"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
)

// Identifiers of the fake machine and license, UUID-shaped like the
// licensing API returns them.
const (
	fakeMachineID = "d3a1d6e0-1f2e-4c9d-8a7b-3f5e9c2b1a01"
	fakeLicenseID = "9c0b6a34-2f0e-4c8f-9a1b-5d7e3f2c4a06"
)

// fakeAPI is a licensing API double for one machine. It answers the
// three calls of Client.ValidateMachine: the machine by fingerprint,
// the license, and the scoped validation. machineLookups counts hits on
// GET machines/{fingerprint}.
type fakeAPI struct {
	key installation.Key

	// mismatchedKey, when set, is served as the machine's publicKey
	// metadata while the machine keeps key's fingerprint.
	mismatchedKey *installation.Key

	// machineNotFound answers the machine lookup with 404.
	machineNotFound bool

	// machineStatus, when non-zero, answers the machine lookup with
	// that bare status.
	machineStatus int

	// licenseCode and licenseValid are the validation verdict.
	licenseCode  string
	licenseValid bool

	// machineLookups counts machine lookups; the handler runs on the
	// server goroutine, so the counter is atomic.
	machineLookups atomic.Int64
}

// newFakeAPI returns a fake whose machine is bound to key and whose
// license validates as VALID.
func newFakeAPI(key installation.Key) *fakeAPI {
	return &fakeAPI{key: key, licenseCode: "VALID", licenseValid: true}
}

// lookups returns the number of machine lookups served so far.
func (f *fakeAPI) lookups() int64 {
	return f.machineLookups.Load()
}

// handler routes the three ValidateMachine calls.
func (f *fakeAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")

		switch {
		case strings.Contains(r.URL.Path, "/machines/"):
			f.machineLookups.Add(1)
			f.writeMachine(w)
		case strings.HasSuffix(r.URL.Path, "/actions/validate"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"type":          "licenses",
					"id":            fakeLicenseID,
					"attributes":    map[string]any{},
					"relationships": nullRelationships(),
				},
				"meta": map[string]any{
					"code":   f.licenseCode,
					"valid":  f.licenseValid,
					"detail": "fake validation",
					"ts":     "2024-01-01T00:00:00.000Z",
				},
			})
		case strings.Contains(r.URL.Path, "/licenses/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"type": "licenses",
					"id":   fakeLicenseID,
					"attributes": map[string]any{
						"name":    "Fake License",
						"key":     "FAKE-KEY",
						"status":  "ACTIVE",
						"created": "2024-01-01T00:00:00.000Z",
						"updated": "2024-01-01T00:00:00.000Z",
					},
					"relationships": nullRelationships(),
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// writeMachine answers the machine lookup: the 404 when configured,
// the machine document otherwise.
func (f *fakeAPI) writeMachine(w http.ResponseWriter) {
	if f.machineStatus != 0 {
		w.WriteHeader(f.machineStatus)

		return
	}

	if f.machineNotFound {
		w.WriteHeader(http.StatusNotFound)

		writeJSON(w, map[string]any{
			"errors": []any{map[string]any{
				"title":  "Not found",
				"detail": "machine not found",
				"code":   "NOT_FOUND",
			}},
		})

		return
	}

	pub := f.key.Public()

	if f.mismatchedKey != nil {
		pub = f.mismatchedKey.Public()
	}

	writeJSON(w, map[string]any{
		"data": map[string]any{
			"type": "machines",
			"id":   fakeMachineID,
			"attributes": map[string]any{
				"fingerprint": f.key.Fingerprint(),
				"metadata":    map[string]string{"publicKey": base64.StdEncoding.EncodeToString(pub)},
			},
			"relationships": map[string]any{
				"license": map[string]any{"data": map[string]any{"type": "licenses", "id": fakeLicenseID}},
			},
		},
	})
}

// writeJSON renders v as the response body, answering 500 when v cannot
// be encoded.
func writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	_, _ = w.Write(body)
}

// nullRelationships is the empty relationship block of a fake document.
func nullRelationships() map[string]any {
	return map[string]any{
		"policy":  map[string]any{"data": nil},
		"product": map[string]any{"data": nil},
		"owner":   map[string]any{"data": nil},
	}
}

// newTestAuthenticator serves fake and returns an Authenticator against
// it. Each call gets its own server, so tests do not share state.
func newTestAuthenticator(t *testing.T, fake *fakeAPI, opts AuthOptions) *Authenticator {
	t.Helper()

	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)

	client, err := licensing.New(licensing.Config{
		APIURL:  server.URL,
		Account: "test",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	auth, err := NewAuthenticator(client, opts)
	if err != nil {
		t.Fatal(err)
	}

	return auth
}

// signRequest signs req with key exactly the way the client Transport
// does, through the httpsign library: a sha-256 Content-Digest when the
// request has a body, then the Signature and Signature-Input headers
// with a fresh nonce.
func signRequest(tb testing.TB, key installation.Key, req *http.Request) {
	tb.Helper()

	if req.Body != nil && req.Body != http.NoBody {
		header, err := httpsign.GenerateContentDigestHeader(&req.Body, []string{httpsign.DigestSha256})
		if err != nil {
			tb.Fatalf("compute content digest: %v", err)
		}

		req.Header.Set(headerContentDigest, header)
	}

	nonce, err := generateNonce()
	if err != nil {
		tb.Fatalf("generate nonce: %v", err)
	}

	signer, err := httpsign.NewEd25519Signer(
		key.PrivateKey(),
		httpsign.NewSignConfig().SetKeyID(key.Fingerprint()).SetNonce(nonce),
		signedFields(),
	)
	if err != nil {
		tb.Fatalf("create signer: %v", err)
	}

	sigInput, sig, err := httpsign.SignRequest(sigLabel, *signer, req)
	if err != nil {
		tb.Fatalf("sign request: %v", err)
	}

	req.Header.Set("Signature", sig)
	req.Header.Set("Signature-Input", sigInput)
}

// createdRe matches the created parameter inside a Signature-Input header.
var createdRe = regexp.MustCompile(`created=\d+`)

// createdParamRe matches the created parameter with its separator inside
// a Signature-Input header.
var createdParamRe = regexp.MustCompile(`created=\d+;?`)

// setCreated rewrites the created parameter of the request's
// Signature-Input header to the Unix time ts. The signature no longer
// matches afterwards; Authenticate must check the created window before
// it verifies the signature, which is what tests using this rely on.
func setCreated(req *http.Request, ts int64) {
	input := req.Header.Get("Signature-Input")
	req.Header.Set("Signature-Input", createdRe.ReplaceAllString(input, "created="+strconv.FormatInt(ts, 10)))
}

// keyidRe matches the keyid parameter inside a Signature-Input header.
var keyidRe = regexp.MustCompile(`keyid="[^"]*"`)

// setKeyid rewrites the keyid parameter of the request's Signature-Input
// header to keyID. The signature no longer matches afterwards;
// Authenticate must check the keyid shape before any lookup.
func setKeyid(req *http.Request, keyID string) {
	input := req.Header.Get("Signature-Input")
	req.Header.Set("Signature-Input", keyidRe.ReplaceAllString(input, `keyid="`+keyID+`"`))
}

// tamperSignature flips the first byte of the Signature header's
// base64 payload, keeping the header well-formed.
func tamperSignature(req *http.Request) error {
	encoded := strings.TrimSuffix(strings.TrimPrefix(req.Header.Get("Signature"), sigLabel+"=:"), ":")

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}

	raw[0] ^= 0xff

	req.Header.Set("Signature", sigLabel+"=:"+base64.StdEncoding.EncodeToString(raw)+":")

	return nil
}

// tamperDigest replaces the Content-Digest header with the digest of a
// different body, keeping the header well-formed.
func tamperDigest(req *http.Request) {
	hash := sha256.Sum256([]byte(`{"tampered":true}`))
	req.Header.Set(headerContentDigest, "sha-256=:"+base64.StdEncoding.EncodeToString(hash[:])+":")
}
