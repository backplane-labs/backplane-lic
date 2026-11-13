package request

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
)

func TestGenerateNonce(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)

	for range 1000 {
		nonce, err := generateNonce()
		if err != nil {
			t.Fatal(err)
		}

		if len(nonce) != 22 { // base64url of 16 bytes without padding
			t.Fatalf("nonce length = %d, want 22", len(nonce))
		}

		if seen[nonce] {
			t.Fatalf("nonce %q repeated", nonce)
		}

		seen[nonce] = true
	}
}

func TestReadBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     io.Reader
		maxBytes int64
		wantLen  int
		wantErr  error
	}{
		{"nil body", nil, 16, 0, nil},
		{"no body", http.NoBody, 16, 0, nil},
		{"empty body", strings.NewReader(""), 16, 0, nil},
		{"body within limit", strings.NewReader("hello"), 16, 5, nil},
		{"body over limit", strings.NewReader("hello world, too large"), 8, 0, ErrBodyTooLarge},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var body io.ReadCloser
			switch b := tc.body.(type) {
			case nil:
				body = nil
			case io.ReadCloser:
				body = b
			default:
				body = io.NopCloser(b)
			}

			req := httptest.NewRequest(http.MethodPost, "https://example.com/api", body)

			got, err := readBody(req, tc.maxBytes)

			if !errorMatches(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}

			if err == nil && got != tc.wantLen {
				t.Errorf("length = %d, want %d", got, tc.wantLen)
			}
		})
	}
}

func TestReadBody_ReplacedBodyRereadable(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api", io.NopCloser(strings.NewReader("payload")))

	if _, err := readBody(req, 64); err != nil {
		t.Fatal(err)
	}

	first, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != "payload" {
		t.Fatalf("first read = %q, want payload", string(first))
	}
}

// errorMatches reports whether got is nil (want is nil) or wraps want.
func errorMatches(got, want error) bool {
	if want == nil {
		return got == nil
	}

	return got != nil && strings.Contains(got.Error(), want.Error())
}

func BenchmarkSign(b *testing.B) {
	key, err := installation.NewKey()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "https://example.com/api", http.NoBody)
		signRequest(b, key, req)
	}
}

func BenchmarkSignWithBody(b *testing.B) {
	key, err := installation.NewKey()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for b.Loop() {
		req := httptest.NewRequest(
			http.MethodPost,
			"https://example.com/api",
			io.NopCloser(strings.NewReader(`{"report":{"events":[]}}`)),
		)
		signRequest(b, key, req)
	}
}
