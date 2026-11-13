package request

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
)

func TestTransport_RoundTrip(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify signature headers are present.
		if r.Header.Get("Signature-Input") == "" {
			t.Error("Signature-Input header missing")
		}

		if r.Header.Get("Signature") == "" {
			t.Error("Signature header missing")
		}

		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	transport := NewTransport(http.DefaultTransport, key)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q", string(body))
	}
}

func TestTransport_SignsWithBody(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	var (
		gotSigInput string
		gotSig      string
		gotCDigest  string
		gotBody     string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSigInput = r.Header.Get("Signature-Input")
		gotSig = r.Header.Get("Signature")
		gotCDigest = r.Header.Get("Content-Digest")

		bodyBytes, _ := io.ReadAll(r.Body)
		gotBody = string(bodyBytes)

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	transport := NewTransport(http.DefaultTransport, key)
	client := &http.Client{Transport: transport}

	body := strings.NewReader(`{"test":true}`)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/data", body)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if gotSigInput == "" {
		t.Error("Signature-Input not set")
	}

	if gotSig == "" {
		t.Error("Signature not set")
	}

	if gotCDigest == "" {
		t.Error("Content-Digest not set")
	}

	if gotBody != `{"test":true}` {
		t.Errorf("body = %q", gotBody)
	}

	// Verify content-digest contains the method in sig-input.
	if !strings.Contains(gotSigInput, `"@method"`) {
		t.Error("Signature-Input missing @method")
	}

	if !strings.Contains(gotSigInput, `"content-digest"`) {
		t.Error("Signature-Input missing content-digest")
	}

	if !strings.Contains(gotSigInput, "keyid=") {
		t.Error("Signature-Input missing keyid")
	}
}

func TestTransport_NoBody_NoContentDigest(t *testing.T) {
	t.Parallel()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	var gotSigInput string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSigInput = r.Header.Get("Signature-Input")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	transport := NewTransport(http.DefaultTransport, key)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(server.URL + "/get")
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if strings.Contains(gotSigInput, "content-digest") {
		t.Error("content-digest should not be covered for GET requests")
	}
}

func BenchmarkTransportSign(b *testing.B) {
	key, err := installation.NewKey()
	if err != nil {
		b.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	b.Cleanup(server.Close)

	transport := NewTransport(http.DefaultTransport, key)
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/test", http.NoBody)

	b.ResetTimer()

	for b.Loop() {
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}

		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
}
