package installation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

func TestPublicKeyOf_ValidKey(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	pubEnc := base64.StdEncoding.EncodeToString(key.Public())

	pubRaw, err := json.Marshal(pubEnc)
	if err != nil {
		t.Fatal(err)
	}

	machine := &licensing.Machine{
		Fingerprint: key.Fingerprint(),
		Metadata:    licensing.Metadata{"publicKey": pubRaw},
	}

	pub, err := PublicKeyOf(machine)
	if err != nil {
		t.Fatal(err)
	}

	if !ed25519Equal(pub, key.Public()) {
		t.Error("public key mismatch")
	}
}

func TestPublicKeyOf_Missing(t *testing.T) {
	t.Parallel()

	machine := &licensing.Machine{Metadata: licensing.Metadata{}}

	_, err := PublicKeyOf(machine)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPublicKeyOf_BadBase64(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	badRaw, err := json.Marshal("!!!not-base64!!!")
	if err != nil {
		t.Fatal(err)
	}

	machine := &licensing.Machine{
		Fingerprint: key.Fingerprint(),
		Metadata:    licensing.Metadata{"publicKey": badRaw},
	}

	_, err = PublicKeyOf(machine)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPublicKeyOf_WrongSize(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	shortKey := base64.StdEncoding.EncodeToString(make([]byte, 16))

	shortRaw, err := json.Marshal(shortKey)
	if err != nil {
		t.Fatal(err)
	}

	machine := &licensing.Machine{
		Fingerprint: key.Fingerprint(),
		Metadata:    licensing.Metadata{"publicKey": shortRaw},
	}

	_, err = PublicKeyOf(machine)
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestPublicKeyOf_FingerprintMismatch(t *testing.T) {
	t.Parallel()

	key1, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	key2, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	pubEnc := base64.StdEncoding.EncodeToString(key2.Public())

	pubRaw, err := json.Marshal(pubEnc)
	if err != nil {
		t.Fatal(err)
	}

	// Machine has key1's fingerprint but key2's public key.
	machine := &licensing.Machine{
		Fingerprint: key1.Fingerprint(),
		Metadata:    licensing.Metadata{"publicKey": pubRaw},
	}

	_, err = PublicKeyOf(machine)
	if err == nil {
		t.Fatal("expected ErrKeyMismatch")
	}
}

func TestParseActivationRequest_RoundTrip(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	original := ActivationRequest{
		Fingerprint: key.Fingerprint(),
		PublicKey:   base64.StdEncoding.EncodeToString(key.Public()),
		LicenseID:   "550e8400-e29b-41d4-a716-446655440000",
		Version:     "2.0.0",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseActivationRequest(data)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Fingerprint != original.Fingerprint {
		t.Errorf("fingerprint mismatch")
	}

	if parsed.PublicKey != original.PublicKey {
		t.Errorf("publicKey mismatch")
	}

	if parsed.LicenseID != original.LicenseID {
		t.Errorf("licenseId mismatch")
	}

	if parsed.Version != original.Version {
		t.Errorf("version mismatch")
	}
}

func TestParseActivationRequest_MissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		js   string
	}{
		{"no fingerprint", `{"license_id":"uuid","version":"1"}`},
		{"no license_id", `{"fingerprint":"abc123"}`},
		{"empty object", `{}`},
		{"empty fingerprint", `{"fingerprint":"","license_id":"uuid"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseActivationRequest([]byte(tc.js))
			if err == nil {
				t.Error("expected error for invalid request")
			}
		})
	}
}

func TestActivationMetadata(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	md := activationMetadata(key, map[string]string{"custom": "value"})

	if fp := md["fingerprint"]; fp != key.Fingerprint() {
		t.Errorf("fingerprint not in metadata")
	}

	expPub := base64.StdEncoding.EncodeToString(key.Public())

	if pk := md["publicKey"]; pk != expPub {
		t.Errorf("publicKey not in metadata")
	}

	if v := md["custom"]; v != "value" {
		t.Errorf("custom metadata not merged")
	}
}

func TestActivationMetadata_PublicKeyWins(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	// Caller tries to override publicKey and fingerprint — they should be overwritten.
	md := activationMetadata(key, map[string]string{
		"publicKey":   "evil-key",
		"fingerprint": "evil-fp",
	})

	if pk := md["publicKey"]; pk == "evil-key" {
		t.Error("caller's publicKey was not overwritten")
	}

	if fp := md["fingerprint"]; fp == "evil-fp" {
		t.Error("caller's fingerprint was not overwritten")
	}
}

func TestActivate_Integration(t *testing.T) {
	t.Parallel()

	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/me":
			_, _ = w.Write([]byte(`{
				"data": {
					"type": "licenses",
					"id": "license-uuid",
					"attributes": {
						"name": "Test",
						"key": "TEST-KEY",
						"status": "ACTIVE",
						"suspended": false,
						"strict": false,
						"floating": false,
						"protected": false,
						"requireHeartbeat": false,
						"requireCheckIn": false,
						"uses": 0,
						"created": "2024-01-01T00:00:00.000Z",
						"updated": "2024-01-01T00:00:00.000Z"
					},
					"relationships": {
						"policy": {"data": null},
						"product": {"data": null},
						"owner": {"data": null}
					}
				}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/licenses/license-uuid/actions/validate":
			_, _ = w.Write([]byte(`{
				"meta": {
					"code": "VALID",
					"valid": true,
					"detail": "ok",
					"ts": "2024-01-01T00:00:00.000Z"
				}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/machines":
			w.WriteHeader(http.StatusCreated)

			_, _ = w.Write([]byte(`{
				"data": {
					"type": "machines",
					"id": "machine-uuid",
					"attributes": {
						"fingerprint": "` + key.Fingerprint() + `",
						"metadata": {}
					},
					"relationships": {
						"license": {"data": {"type": "licenses", "id": "license-uuid"}}
					}
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := licensing.New(licensing.Config{
		APIURL:     server.URL,
		Account:    "test",
		LicenseKey: "test-license-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	license, err := client.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	machine, err := Activate(context.Background(), license, key, licensing.WithMachineName("test-machine"))
	if err != nil {
		t.Fatal(err)
	}

	if machine.Fingerprint != key.Fingerprint() {
		t.Errorf("fingerprint = %s, want %s", machine.Fingerprint, key.Fingerprint())
	}
}

// ed25519Equal compares two Ed25519 public keys.
func ed25519Equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
