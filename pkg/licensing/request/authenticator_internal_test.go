package request

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yaronf/httpsign"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
)

// noCacheOpts disables both caches so every refusal is fresh.
func noCacheOpts() AuthOptions {
	return AuthOptions{MaxAge: time.Minute, CacheTTL: -1, NegativeTTL: -1}
}

// newSignedRequest returns a GET request to a fixed target, signed by key.
func newSignedRequest(t *testing.T, key installation.Key) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "https://example.com/api", http.NoBody)
	signRequest(t, key, req)

	return req
}

func TestAuthenticate_Valid(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	auth := newTestAuthenticator(t, fake, noCacheOpts())

	inst, err := auth.Authenticate(newSignedRequest(t, key))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"fingerprint", inst.Fingerprint, key.Fingerprint()},
		{"machine id", inst.MachineID, fakeMachineID},
		{"license id", inst.LicenseID, fakeLicenseID},
		{"license code", inst.LicenseCode, "VALID"},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}

	if !inst.PublicKey.Equal(key.Public()) {
		t.Error("public key mismatch")
	}
}

func TestAuthenticate_ValidWithBody(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	auth := newTestAuthenticator(t, fake, noCacheOpts())

	req := httptest.NewRequest(
		http.MethodPost,
		"https://example.com/api/v1/data",
		io.NopCloser(strings.NewReader(`{"method":"test"}`)),
	)
	signRequest(t, key, req)

	inst, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if inst.Fingerprint != key.Fingerprint() {
		t.Errorf("fingerprint = %q, want %q", inst.Fingerprint, key.Fingerprint())
	}

	// The body must still be readable by the handler behind the
	// authenticator.
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != `{"method":"test"}` {
		t.Errorf("body = %q, want the original body", string(body))
	}
}

func TestAuthenticate_Refusals(t *testing.T) {
	t.Parallel()

	valid := func(t *testing.T) (installation.Key, *Authenticator, *fakeAPI) {
		t.Helper()

		key, err := installation.NewKey()
		if err != nil {
			t.Fatal(err)
		}

		fake := newFakeAPI(key)

		return key, newTestAuthenticator(t, fake, noCacheOpts()), fake
	}

	tests := []struct {
		name    string
		mutate  func(t *testing.T, req *http.Request)
		wantErr error
	}{
		{
			name: "signature headers missing",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()
				req.Header.Del("Signature")
				req.Header.Del("Signature-Input")
			},
			wantErr: ErrSignatureMissing,
		},
		{
			name: "keyid is not a fingerprint",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()
				setKeyid(req, "../../not-a-fingerprint")
			},
			wantErr: ErrSignatureInvalid,
		},
		{
			name: "created beyond future skew",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()
				setCreated(req, time.Now().Add(5*time.Minute).Unix())
			},
			wantErr: ErrCreatedExpired,
		},
		{
			name: "created beyond max age",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()
				setCreated(req, time.Now().Add(-10*time.Minute).Unix())
			},
			wantErr: ErrCreatedExpired,
		},
		{
			name: "created parameter removed",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()
				input := createdParamRe.ReplaceAllString(req.Header.Get("Signature-Input"), "")
				req.Header.Set("Signature-Input", input)
			},
			wantErr: ErrCreatedMissing,
		},
		{
			name: "tampered signature",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()

				if err := tamperSignature(req); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: ErrSignatureInvalid,
		},
		{
			name: "nonce parameter removed",
			mutate: func(t *testing.T, req *http.Request) {
				t.Helper()
				input := nonceParamRe.ReplaceAllString(req.Header.Get("Signature-Input"), "")
				req.Header.Set("Signature-Input", input)
			},
			wantErr: ErrNonceMissing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, auth, _ := valid(t)

			req := newSignedRequest(t, key)
			tc.mutate(t, req)

			_, err := auth.Authenticate(req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAuthenticate_TamperedRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(req *http.Request)
	}{
		{"path", func(req *http.Request) { req.URL.Path = "/admin" }},
		{"query", func(req *http.Request) { req.URL.RawQuery = "x=1" }},
		{"method", func(req *http.Request) { req.Method = http.MethodDelete }},
		{"host", func(req *http.Request) { req.URL.Host = "evil.example.com" }},
		{"nonce parameter", func(req *http.Request) {
			input := req.Header.Get("Signature-Input")
			req.Header.Set("Signature-Input", nonceParamRe.ReplaceAllString(input, `nonce="forged"`))
		}},
		{"signed header value", func(req *http.Request) {
			req.Header.Set(headerContentDigest, "sha-256=:AAAA:")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, err := installation.NewKey()
			if err != nil {
				t.Fatal(err)
			}

			auth := newTestAuthenticator(t, newFakeAPI(key), noCacheOpts())

			req := newSignedRequest(t, key)
			tc.mutate(req)

			if _, err := auth.Authenticate(req); err == nil {
				t.Fatal("tampered request was authenticated")
			}
		})
	}
}

func TestAuthenticate_UnknownFingerprint(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	fake.machineNotFound = true

	auth := newTestAuthenticator(t, fake, noCacheOpts())

	_, err = auth.Authenticate(newSignedRequest(t, key))
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("error = %v, want %v", err, ErrUnknownKey)
	}
}

func TestAuthenticate_ReplayedNonce(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	auth := newTestAuthenticator(t, newFakeAPI(key), noCacheOpts())

	req := newSignedRequest(t, key)
	replay := req.Clone(req.Context())

	if _, err := auth.Authenticate(req); err != nil {
		t.Fatalf("first authenticate: %v", err)
	}

	_, err = auth.Authenticate(replay)
	if !errors.Is(err, ErrNonceReplayed) {
		t.Fatalf("second authenticate error = %v, want %v", err, ErrNonceReplayed)
	}
}

func TestAuthenticate_BadDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(req *http.Request)
		wantErr error
	}{
		{
			name: "digest of another body",
			mutate: func(req *http.Request) {
				tamperDigest(req)
			},
			wantErr: ErrDigestMismatch,
		},
		{
			name: "digest removed on a request with a body",
			mutate: func(req *http.Request) {
				req.Header.Del(headerContentDigest)
			},
			wantErr: ErrDigestRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, err := installation.NewKey()
			if err != nil {
				t.Fatal(err)
			}

			auth := newTestAuthenticator(t, newFakeAPI(key), noCacheOpts())

			req := httptest.NewRequest(
				http.MethodPost,
				"https://example.com/api",
				io.NopCloser(strings.NewReader(`{"body":true}`)),
			)
			signRequest(t, key, req)
			tc.mutate(req)

			_, err = auth.Authenticate(req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAuthenticate_BodyTooLarge(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	opts := noCacheOpts()
	opts.MaxBodySize = 16

	auth := newTestAuthenticator(t, newFakeAPI(key), opts)

	body := strings.Repeat("x", 32)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/api", io.NopCloser(strings.NewReader(body)))
	signRequest(t, key, req)

	_, err = auth.Authenticate(req)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want %v", err, ErrBodyTooLarge)
	}
}

func TestAuthenticate_InvalidLicenseFailsClosed(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	fake.licenseCode = "EXPIRED"
	fake.licenseValid = false

	auth := newTestAuthenticator(t, fake, noCacheOpts())

	req := newSignedRequest(t, key)

	_, err = auth.Authenticate(req)
	if !errors.Is(err, ErrLicenseInvalid) {
		t.Fatalf("error = %v, want %v", err, ErrLicenseInvalid)
	}

	var licErr *LicenseError
	if !errors.As(err, &licErr) || licErr.Code != "EXPIRED" {
		t.Errorf("error = %T(%v), want *LicenseError with code EXPIRED", err, err)
	}

	// Only the negative result is cached: the second call fails closed
	// again without another lookup.
	if _, err := auth.Authenticate(newSignedRequest(t, key)); !errors.Is(err, ErrLicenseInvalid) {
		t.Fatalf("second authenticate error = %v, want %v", err, ErrLicenseInvalid)
	}
}

func TestAuthenticate_NegativeResultCached(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	other, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		configure   func(*fakeAPI)
		wantErr     error
		wantLookups int64
	}{
		{"invalid license is cached", func(f *fakeAPI) { f.licenseCode, f.licenseValid = "EXPIRED", false }, ErrLicenseInvalid, 1},
		{"unknown key is cached", func(f *fakeAPI) { f.machineNotFound = true }, ErrUnknownKey, 1},
		{"upstream failure is not cached", func(f *fakeAPI) { f.machineStatus = http.StatusBadGateway }, ErrUpstreamUnavailable, 2},
		{"key mismatch is not cached", func(f *fakeAPI) { f.mismatchedKey = &other }, ErrKeyMismatch, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeAPI(key)
			tc.configure(fake)

			opts := AuthOptions{MaxAge: time.Minute, CacheTTL: -1, NegativeTTL: time.Minute}
			auth := newTestAuthenticator(t, fake, opts)

			for range 2 {
				_, err := auth.Authenticate(newSignedRequest(t, key))
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("authenticate error = %v, want %v", err, tc.wantErr)
				}
			}

			if got := fake.lookups(); got != tc.wantLookups {
				t.Errorf("machine lookups = %d, want %d", got, tc.wantLookups)
			}
		})
	}
}

func TestAuthenticate_LookupsRateLimited(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	opts := noCacheOpts()
	opts.LookupRate, opts.LookupBurst = 0.001, 2
	auth := newTestAuthenticator(t, fake, opts)

	for i := range 3 {
		_, err := auth.Authenticate(newSignedRequest(t, key))

		switch {
		case i < 2 && err != nil:
			t.Fatalf("lookup %d within the burst: %v", i, err)
		case i == 2 && !errors.Is(err, ErrUpstreamUnavailable):
			t.Fatalf("lookup past the burst error = %v, want %v", err, ErrUpstreamUnavailable)
		}
	}

	if got := fake.lookups(); got != 2 {
		t.Errorf("machine lookups = %d, want 2: a limited lookup must not reach the API", got)
	}
}

func TestNewAuthenticator_RejectsNegativeOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts AuthOptions
	}{
		{"max age", AuthOptions{MaxAge: -1}},
		{"future skew", AuthOptions{FutureSkew: -1}},
		{"lookup timeout", AuthOptions{LookupTimeout: -1}},
		{"max body size", AuthOptions{MaxBodySize: -1}},
		{"lookup rate", AuthOptions{LookupRate: -1}},
		{"lookup burst", AuthOptions{LookupBurst: -1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewAuthenticator(nil, tc.opts); !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("err = %v, want ErrInvalidOptions", err)
			}
		})
	}
}

func TestWriteAuthError_HidesFailureDetail(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeAuthError(rec, errors.New("dial tcp 10.0.0.7:443: connection refused"))

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Errorf("status %d body %s: a failure must not leak its cause", rec.Code, rec.Body.String())
	}
}

func TestAuthenticate_UnsignedTrafficDoesNotFillReplayStore(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	guard := &countingGuard{}
	opts := noCacheOpts()
	opts.ReplayGuard = guard
	auth := newTestAuthenticator(t, newFakeAPI(key), opts)

	req := newSignedRequest(t, key)
	if err := tamperSignature(req); err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Authenticate(req); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("error = %v, want %v", err, ErrSignatureInvalid)
	}

	if guard.calls != 0 {
		t.Errorf("replay guard called %d times for an unverified signature, want 0", guard.calls)
	}
}

// countingGuard counts CheckAndRemember calls and accepts every nonce.
type countingGuard struct{ calls int }

func (g *countingGuard) CheckAndRemember(string, string) bool {
	g.calls++

	return true
}

func TestAuthenticate_KeyMismatch(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	other, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	fake.mismatchedKey = &other

	auth := newTestAuthenticator(t, fake, noCacheOpts())

	_, err = auth.Authenticate(newSignedRequest(t, key))
	if !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("error = %v, want %v", err, ErrKeyMismatch)
	}
}

func TestAuthenticate_CacheSkipsLookupOnly(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	fake := newFakeAPI(key)
	opts := AuthOptions{MaxAge: time.Minute, CacheTTL: time.Minute, NegativeTTL: -1}
	auth := newTestAuthenticator(t, fake, opts)

	// First request populates the cache.
	if _, err := auth.Authenticate(newSignedRequest(t, key)); err != nil {
		t.Fatalf("first authenticate: %v", err)
	}

	// Second request is served from the cache: no new lookup.
	if _, err := auth.Authenticate(newSignedRequest(t, key)); err != nil {
		t.Fatalf("second authenticate: %v", err)
	}

	if got := fake.lookups(); got != 1 {
		t.Fatalf("machine lookups = %d, want 1", got)
	}

	// A cached installation does not skip verification: a tampered
	// signature is still refused, again without a new lookup.
	req := newSignedRequest(t, key)
	if err := tamperSignature(req); err != nil {
		t.Fatal(err)
	}

	_, err = auth.Authenticate(req)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("third authenticate error = %v, want %v", err, ErrSignatureInvalid)
	}

	if got := fake.lookups(); got != 1 {
		t.Errorf("machine lookups = %d, want 1 (cache only skips the lookup)", got)
	}
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	auth := newTestAuthenticator(t, newFakeAPI(key), noCacheOpts())

	var got Installation

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inst, ok := FromContext(r.Context())
		if !ok {
			t.Error("no installation in the handler context")
		}

		got = inst

		w.WriteHeader(http.StatusOK)
	})

	handler := auth.Middleware(next)

	// Authenticated: the handler sees the installation.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newSignedRequest(t, key))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if got.Fingerprint != key.Fingerprint() {
		t.Errorf("fingerprint = %q, want %q", got.Fingerprint, key.Fingerprint())
	}

	// Refused: a JSON error body with a stable code.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://example.com/api", http.NoBody))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	if body.Error.Code != "SIGNATURE_INVALID" {
		t.Errorf("error code = %q, want SIGNATURE_INVALID", body.Error.Code)
	}
}

// nonceParamRe matches the nonce parameter with its separator inside a
// Signature-Input header.
var nonceParamRe = regexp.MustCompile(`nonce="[^"]*";?`)

func BenchmarkVerify(b *testing.B) {
	key, err := installation.NewKey()
	if err != nil {
		b.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com/api", http.NoBody)
	signRequest(b, key, req)

	verifier, err := httpsign.NewEd25519Verifier(
		key.Public(),
		httpsign.NewVerifyConfig().
			SetVerifyCreated(true).
			SetNotNewerThan(30*time.Second).
			SetNotOlderThan(time.Hour).
			SetKeyID(key.Fingerprint()).
			SetAllowedAlgs([]string{algEd25519}),
		signedFields(),
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		if err := httpsign.VerifyRequest(sigLabel, *verifier, req); err != nil {
			b.Fatal(err)
		}
	}
}
