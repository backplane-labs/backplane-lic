package licensing_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	licensing "github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// fakeKeygen is a minimal Keygen API double: it signs every response
// with a generated ed25519 account key and serves canned documents.
type fakeKeygen struct {
	t       *testing.T
	account testAccount
	server  *httptest.Server
	pings   atomic.Int64
	corrupt bool
	handler func(w http.ResponseWriter, r *http.Request, fake *fakeKeygen) bool
}

// newFakeKeygen starts the double; handler handles paths the canned
// logic does not.
func newFakeKeygen(
	t *testing.T,
	handler func(w http.ResponseWriter, r *http.Request, fake *fakeKeygen) bool,
) *fakeKeygen {
	t.Helper()

	fake := &fakeKeygen{t: t, account: newTestAccount(t), handler: handler}

	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler != nil && handler(w, r, fake) {
			return
		}

		fake.canned(w, r)
	}))

	t.Cleanup(fake.server.Close)

	return fake
}

// canned serves the happy-path documents the client tests need.
func (f *fakeKeygen) canned(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/me":
		f.write(w, r, http.StatusOK, licenseDocument())
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/actions/validate"):
		f.write(w, r, http.StatusOK, licenseDocument()[:len(licenseDocument())-1]+
			`,"meta":{"ts":"`+time.Now().UTC().Format(time.RFC3339)+`","valid":true,"code":"VALID","detail":"is valid"}}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/machines":
		f.write(w, r, http.StatusCreated, machineDocument(30))
	case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/actions/ping"):
		f.pings.Add(1)
		f.write(w, r, http.StatusOK, machineDocument(2))
	default:
		f.write(w, r, http.StatusNotFound, `{"meta":{"id":"req-1"},"errors":[{"title":"Not found","detail":"nope","code":"NOT_FOUND"}]}`)
	}
}

// write renders a signed JSON:API response over the real request's
// method, path, and host.
func (f *fakeKeygen) write(w http.ResponseWriter, r *http.Request, status int, body string) {
	raw := []byte(body)

	shasum := sha256.Sum256(raw)
	digest := "sha-256=" + base64.StdEncoding.EncodeToString(shasum[:])
	date := time.Now().UTC().Format(http.TimeFormat)

	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}

	signingData := fmt.Sprintf(
		"(request-target): %s %s\nhost: %s\ndate: %s\ndigest: %s",
		strings.ToLower(r.Method), r.URL.RequestURI(), host, date, digest,
	)

	w.Header().Set("Digest", digest)
	w.Header().Set("Date", date)
	w.Header().Set("X-Request-Id", "req-1")
	w.Header().Set("Keygen-Signature", fmt.Sprintf(
		`keyid="acc-1", algorithm="ed25519", signature=%q, headers="(request-target) host date digest"`,
		base64.StdEncoding.EncodeToString(ed25519.Sign(f.account.ed25519Priv, []byte(signingData))),
	))
	w.Header().Set("Content-Type", "application/vnd.api+json")

	if f.corrupt {
		sig := w.Header().Get("Keygen-Signature")
		w.Header().Set("Keygen-Signature", strings.Replace(sig, "signature=", "signature=\"Zm9v", 1))
	}

	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// licenseDocument renders a license JSON:API document.
func licenseDocument() string {
	return `{"data":{"id":"lic-1","type":"licenses","attributes":{"name":"n","key":"key/x.y","scheme":"ED25519_SIGN","requireHeartbeat":true,"strict":true,"floating":true,"created":"2026-01-01T00:00:00.000Z","updated":"2026-01-01T00:00:00.000Z","metadata":{}},"relationships":{"policy":{"data":{"type":"policies","id":"pol-1"}}}}}`
}

// machineDocument renders a machine JSON:API document with the given
// heartbeat duration.
func machineDocument(duration int) string {
	return `{"data":{"id":"mach-1","type":"machines","attributes":{"fingerprint":"fp-1","hostname":"h","platform":"linux/amd64","cores":4,"requireHeartbeat":true,"heartbeatStatus":"ALIVE","heartbeatDuration":` +
		strconv.Itoa(duration) + `,"created":"2026-01-01T00:00:00.000Z","updated":"2026-01-01T00:00:00.000Z","metadata":{}},"relationships":{"license":{"data":{"type":"licenses","id":"lic-1"}}}}}`
}

// newTestClient builds a client against the double.
func newTestClient(t *testing.T, fake *fakeKeygen) *licensing.Client {
	t.Helper()

	client, err := licensing.New(licensing.Config{
		APIURL:     fake.server.URL,
		LicenseKey: "key/x.y",
		PublicKey:  fmt.Sprintf("%x", fake.account.ed25519Priv.Public()),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return client
}

func TestClientConfigErrors(t *testing.T) {
	t.Parallel()

	if _, err := licensing.New(licensing.Config{}); !errors.Is(err, licensing.ErrConfig) {
		t.Fatalf("empty config err = %v", err)
	}

	if _, err := licensing.New(licensing.Config{LicenseKey: "k"}); !errors.Is(err, licensing.ErrConfig) {
		t.Fatalf("missing account err = %v", err)
	}

	if _, err := licensing.New(licensing.Config{APIURL: "http://localhost:3900", LicenseKey: "k", PublicKey: "zz"}); !errors.Is(err, licensing.ErrPublicKeyInvalid) {
		t.Fatalf("bad key err = %v", err)
	}
}

func TestClientValidateFlow(t *testing.T) {
	t.Parallel()

	fake := newFakeKeygen(t, nil)
	client := newTestClient(t, fake)

	license, err := client.Validate(context.Background(), licensing.WithFingerprint("fp-1"))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if license.ID != "lic-1" || license.PolicyID != "pol-1" || !license.RequireHeartbeat {
		t.Fatalf("license = %+v", license)
	}

	if license.Validation == nil || license.Validation.Code != licensing.CodeValid {
		t.Fatalf("validation = %+v", license.Validation)
	}

	machine, err := license.Activate(context.Background(), "fp-1")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if machine.ID != "mach-1" || machine.LicenseID != "lic-1" || machine.HeartbeatDuration != 30 {
		t.Fatalf("machine = %+v", machine)
	}
}

func TestClientErrorMapping(t *testing.T) {
	t.Parallel()

	var mode atomic.Int64

	fake := newFakeKeygen(t, func(w http.ResponseWriter, r *http.Request, fake *fakeKeygen) bool {
		switch mode.Add(1) {
		case 1:
			fake.write(w, r, http.StatusUnauthorized,
				`{"meta":{"id":"req-1"},"errors":[{"title":"Unauthorized","detail":"is unauthorized","code":"LICENSE_INVALID","source":{"header":"Authorization"}}]}`)

			return true
		case 2:
			w.Header().Set("Retry-After", "30")
			w.Header().Set("X-Ratelimit-Window", "1m")
			w.Header().Set("X-Ratelimit-Count", "101")
			w.Header().Set("X-Ratelimit-Limit", "100")
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", "1790000000")
			w.WriteHeader(http.StatusTooManyRequests)

			return true
		default:
			return false
		}
	})

	client := newTestClient(t, fake)

	ctx := context.Background()

	_, err := client.Validate(ctx)
	if !errors.Is(err, licensing.ErrLicenseInvalidKey) {
		t.Fatalf("401 err = %v", err)
	}

	var apiErr *licensing.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "LICENSE_INVALID" || apiErr.Status != 401 {
		t.Fatalf("api error = %+v", apiErr)
	}

	_, err = client.Me(ctx)
	if err == nil {
		t.Fatal("expected error")
	}

	var rateErr *licensing.RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("rate err = %v", err)
	}

	if rateErr.RetryAfter != 30 || rateErr.Limit != 100 || rateErr.Window != "1m" || rateErr.Remaining != 0 {
		t.Fatalf("rate limit = %+v", rateErr)
	}

	if !rateErr.Reset.Equal(time.Unix(1790000000, 0)) {
		t.Fatalf("reset = %v", rateErr.Reset)
	}
}

func TestClientRejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	fake := newFakeKeygen(t, func(w http.ResponseWriter, r *http.Request, fake *fakeKeygen) bool {
		fake.corrupt = true
		fake.write(w, r, http.StatusOK, licenseDocument())

		return true
	})

	client := newTestClient(t, fake)

	_, err := client.Validate(context.Background())
	if !errors.Is(err, licensing.ErrSignatureInvalid) {
		t.Fatalf("tampered err = %v", err)
	}
}

func TestMonitorPingsAndStops(t *testing.T) {
	t.Parallel()

	fake := newFakeKeygen(t, func(w http.ResponseWriter, r *http.Request, fake *fakeKeygen) bool {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/machines" {
			fake.write(w, r, http.StatusCreated, machineDocument(2))

			return true
		}

		return false
	})
	client := newTestClient(t, fake)

	license, err := client.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	machine, err := license.Activate(context.Background(), "fp-1")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	// The canned ping answers with heartbeatDuration=2, so the monitor
	// waits ~1.3s between pings; cancel mid-wait.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)

	go func() { done <- machine.Monitor(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Monitor: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Monitor did not stop")
	}

	if pings := fake.pings.Load(); pings < 2 {
		t.Fatalf("pings = %d", pings)
	}
}
