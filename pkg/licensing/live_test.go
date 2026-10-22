package licensing_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	licensing "github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// Live reference wiring. LIC_LICENSING_REFERENCE is the reference
// origin (e.g. http://localhost:3900); the account is the compose
// fixture's example-com singleplayer account.
const (
	liveAccountID   = "2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f"
	liveAccountSlug = "example-com"
	liveAdminEmail  = "admin@example.com"
	liveAdminPass   = "adminadmin123"
	liveHost        = "api.keygen.test"
)

// liveBase resolves the reference origin or skips the test.
func liveBase(t *testing.T) string {
	t.Helper()

	base := os.Getenv("LIC_LICENSING_REFERENCE")
	if base == "" {
		t.Skip("LIC_LICENSING_REFERENCE is not set")
	}

	return strings.TrimSuffix(base, "/")
}

// liveAdmin performs an authenticated admin request against the
// reference.
func liveAdmin(t *testing.T, method, path string, body []byte) []byte {
	t.Helper()

	request, err := http.NewRequest(method, liveBase(t)+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}

	request.Host = liveHost
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Accept", "application/vnd.api+json")
	request.Header.Set("Authorization", "Bearer "+liveAdminToken(t))

	if body != nil {
		request.Header.Set("Content-Type", "application/vnd.api+json")
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("admin request: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("admin read: %v", err)
	}

	if response.StatusCode >= 300 {
		t.Fatalf("admin %s %s: %d %s", method, path, response.StatusCode, raw)
	}

	return raw
}

// liveAdminToken fetches an admin token once per test run: password
// authentication is throttled to 5 attempts per minute.
func liveAdminToken(t *testing.T) string {
	t.Helper()

	token, err := liveTokenOnce()
	if err != nil {
		t.Fatalf("admin token: %v", err)
	}

	return token
}

var liveTokenOnce = sync.OnceValues(func() (string, error) {
	request, err := http.NewRequest(
		http.MethodPost,
		os.Getenv("LIC_LICENSING_REFERENCE")+"/v1/accounts/"+liveAccountSlug+"/tokens",
		http.NoBody,
	)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}

	request.Host = liveHost
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Accept", "application/vnd.api+json")
	request.SetBasicAuth(liveAdminEmail, liveAdminPass)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("token read: %w", err)
	}

	if response.StatusCode >= 300 {
		return "", fmt.Errorf("token: %d %s", response.StatusCode, raw)
	}

	var doc struct {
		Data struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("token decode: %w", err)
	}

	if doc.Data.Attributes.Token == "" {
		return "", errors.New("token: empty")
	}

	return doc.Data.Attributes.Token, nil
})

// liveAccountEd25519 fetches the account's ed25519 public key as hex.
func liveAccountEd25519(t *testing.T) string {
	t.Helper()

	raw := liveAdmin(t, http.MethodGet, "/v1/accounts/"+liveAccountSlug, nil)

	var doc struct {
		Data struct {
			Meta struct {
				Keys struct {
					Ed25519 string `json:"ed25519"`
				} `json:"keys"`
			} `json:"meta"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("account decode: %v", err)
	}

	// The wire value is base64 of the hex string.
	hexKey, err := base64.StdEncoding.DecodeString(doc.Data.Meta.Keys.Ed25519)
	if err != nil {
		t.Fatalf("ed25519 key b64: %v", err)
	}

	return string(hexKey)
}

// liveFixture is the license setup created per test run.
type liveFixture struct {
	productID    string
	policyID     string
	licenseID    string
	licenseKey   string
	publicKeyHex string
}

// liveSetup creates a product, policy, entitlement, and license through
// the admin API.
func liveSetup(t *testing.T) liveFixture {
	t.Helper()

	suffix := strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)

	product := liveAdmin(t, http.MethodPost, "/v1/products", []byte(`{"data":{"type":"products","attributes":{"name":"licensing-live-`+suffix+`","platforms":["macos"]}}}`))

	productID := jsonID(t, product)

	policy := liveAdmin(t, http.MethodPost, "/v1/policies", []byte(`{"data":{"type":"policies","attributes":{"name":"licensing-live-`+suffix+`","scheme":"ED25519_SIGN","authenticationStrategy":"LICENSE","floating":true,"maxMachines":2,"strict":true,"requireHeartbeat":true,"heartbeatDuration":60},"relationships":{"product":{"data":{"type":"products","id":"`+productID+`"}}}}}`))

	policyID := jsonID(t, policy)

	entitlement := liveAdmin(t, http.MethodPost, "/v1/entitlements", []byte(`{"data":{"type":"entitlements","attributes":{"code":"licensing-live-`+suffix+`","name":"Licensing Live"}}}`))

	entitlementID := jsonID(t, entitlement)

	license := liveAdmin(t, http.MethodPost, "/v1/licenses", []byte(`{"data":{"type":"licenses","relationships":{"policy":{"data":{"type":"policies","id":"`+policyID+`"}}}}}`))

	licenseID := jsonID(t, license)

	liveAdmin(t, http.MethodPost, "/v1/licenses/"+licenseID+"/entitlements",
		[]byte(`{"data":[{"type":"entitlements","id":"`+entitlementID+`"}]}`))

	var licenseDoc struct {
		Data struct {
			Attributes struct {
				Key string `json:"key"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(license, &licenseDoc); err != nil {
		t.Fatalf("license decode: %v", err)
	}

	return liveFixture{
		productID:    productID,
		policyID:     policyID,
		licenseID:    licenseID,
		licenseKey:   licenseDoc.Data.Attributes.Key,
		publicKeyHex: liveAccountEd25519(t),
	}
}

// jsonID extracts data.id from a JSON:API document.
func jsonID(t *testing.T, raw []byte) string {
	t.Helper()

	var doc struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("id decode: %v", err)
	}

	if doc.Data.ID == "" {
		t.Fatalf("empty id in %s", raw)
	}

	return doc.Data.ID
}

// liveClient builds the licensing client under test.
func liveClient(t *testing.T, fixture liveFixture) *licensing.Client {
	t.Helper()

	client, err := licensing.New(licensing.Config{
		APIURL:     liveBase(t),
		LicenseKey: fixture.licenseKey,
		PublicKey:  fixture.publicKeyHex,
		Headers: map[string]string{
			"Host":              liveHost,
			"X-Forwarded-Proto": "https",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return client
}

func TestLiveLicensing(t *testing.T) {
	t.Parallel()

	_ = liveBase(t)
	fixture := liveSetup(t)
	client := liveClient(t, fixture)

	verifier, err := licensing.NewVerifier(fixture.publicKeyHex)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	const fingerprint = "licensing-live-fp-1"

	// 1. Validation before activation: strict floating policy, zero
	// machines => NO_MACHINES => ErrNotActivated.
	license, err := client.Validate(ctx)
	if !errors.Is(err, licensing.ErrNotActivated) {
		t.Fatalf("initial validate err = %v", err)
	}

	if license.Validation == nil || license.Validation.Code != licensing.CodeNoMachines {
		t.Fatalf("initial code = %+v", license.Validation)
	}

	if license.Scheme != licensing.SchemeEd25519 || !license.RequireHeartbeat {
		t.Fatalf("license = %+v", license)
	}

	// 2. Signed license key verification: ED25519_SIGN embeds the seed
	// dataset, signed with the account key.
	keyDataset, err := license.VerifyKey()
	if err != nil {
		t.Fatalf("VerifyKey: %v", err)
	}

	if !strings.Contains(string(keyDataset), liveAccountID) {
		t.Fatalf("key dataset = %s", keyDataset)
	}

	// 3. Activation.
	machine, err := license.Activate(ctx, fingerprint,
		licensing.WithMachineName("live-test"),
		licensing.WithMachinePlatform("linux/amd64"),
		licensing.WithMachineCores(4),
	)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if machine.Fingerprint != fingerprint || machine.HeartbeatDuration != 60 {
		t.Fatalf("machine = %+v", machine)
	}

	if machine.HeartbeatStatus != licensing.HeartbeatAlive {
		t.Fatalf("heartbeat status = %s", machine.HeartbeatStatus)
	}

	// 4. Validation with the fingerprint now succeeds.
	if _, err := client.Validate(ctx, licensing.WithFingerprint(fingerprint)); err != nil {
		t.Fatalf("scoped validate: %v", err)
	}

	// 5. Re-activating the same fingerprint fails with FINGERPRINT_TAKEN.
	if _, err := license.Activate(ctx, fingerprint); !errors.Is(err, licensing.ErrMachineAlreadyActivated) {
		t.Fatalf("re-activate err = %v", err)
	}

	// 6. Filling the seat limit (maxMachines=2) then exceeding it.
	second, err := license.Activate(ctx, "licensing-live-fp-2")
	if err != nil {
		t.Fatalf("second activate: %v", err)
	}

	if _, err := license.Activate(ctx, "licensing-live-fp-3"); !errors.Is(err, licensing.ErrMachineLimitExceeded) {
		t.Fatalf("third activate err = %v", err)
	}

	// 7. License file checkout, signature verification, decryption.
	file, err := license.Checkout(ctx)
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	if err := file.Verify(verifier); err != nil {
		t.Fatalf("license file Verify: %v", err)
	}

	dataset, err := file.Decrypt(fixture.licenseKey)
	if err != nil {
		t.Fatalf("license file Decrypt: %v", err)
	}

	if dataset.License.ID != fixture.licenseID {
		t.Fatalf("dataset license id = %s", dataset.License.ID)
	}

	if len(dataset.Entitlements) != 1 {
		t.Fatalf("dataset entitlements = %+v", dataset.Entitlements)
	}

	// 8. Machine file checkout, signature verification, decryption with
	// key + fingerprint.
	machineFile, err := machine.Checkout(ctx)
	if err != nil {
		t.Fatalf("machine Checkout: %v", err)
	}

	if err := machineFile.Verify(verifier); err != nil {
		t.Fatalf("machine file Verify: %v", err)
	}

	machineDataset, err := machineFile.Decrypt(fixture.licenseKey, fingerprint)
	if err != nil {
		t.Fatalf("machine file Decrypt: %v", err)
	}

	if machineDataset.Machine.ID != machine.ID {
		t.Fatalf("machine dataset id = %s", machineDataset.Machine.ID)
	}

	if machineDataset.License.ID != fixture.licenseID {
		t.Fatalf("machine dataset license id = %s", machineDataset.License.ID)
	}

	// 9. Heartbeat ping and a brief monitor run.
	if err := machine.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	monitorCtx, stopMonitor := context.WithCancel(ctx)

	monitored := make(chan error, 1)

	go func() { monitored <- machine.Monitor(monitorCtx) }()

	time.Sleep(1500 * time.Millisecond)
	stopMonitor()

	select {
	case err := <-monitored:
		if err != nil {
			t.Fatalf("Monitor: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Monitor did not stop")
	}

	// 10. Processes: spawn, monitor briefly, kill.
	process, err := machine.Spawn(ctx, fmt.Sprintf("pid-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if process.Status != licensing.ProcessAlive || process.Interval != 60 {
		t.Fatalf("process = %+v", process)
	}

	processCtx, stopProcess := context.WithCancel(ctx)

	monitoredProcess := make(chan error, 1)

	go func() { monitoredProcess <- process.Monitor(processCtx) }()

	time.Sleep(1500 * time.Millisecond)
	stopProcess()

	select {
	case err := <-monitoredProcess:
		if err != nil {
			t.Fatalf("process Monitor: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("process Monitor did not stop")
	}

	if err := process.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// 11. Listing: machines, entitlements, machine by id.
	machines, err := license.Machines(ctx)
	if err != nil {
		t.Fatalf("Machines: %v", err)
	}

	if len(machines) != 2 {
		t.Fatalf("machines = %d", len(machines))
	}

	entitlements, err := license.Entitlements(ctx)
	if err != nil {
		t.Fatalf("Entitlements: %v", err)
	}

	if len(entitlements) != 1 {
		t.Fatalf("entitlements = %d", len(entitlements))
	}

	fetched, err := license.Machine(ctx, machine.ID)
	if err != nil {
		t.Fatalf("Machine: %v", err)
	}

	if fetched.Fingerprint != fingerprint {
		t.Fatalf("fetched fingerprint = %s", fetched.Fingerprint)
	}

	// 12. Deactivation.
	if err := second.Deactivate(ctx); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
}
