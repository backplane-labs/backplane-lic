// Package e2e_test drives the finished service through its public HTTP
// port with the pkg/licensing clients: the in-process server boots over the
// shared PostgreSQL (LIC_TEST_DATABASE_URL) in an account of its own.
package e2e_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/service"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
	"github.com/backplane-labs/backplane-lic/pkg/licensing"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/request"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

const (
	origin      = "http://lic.test"
	usageDomain = "e2e-usage/v1"
)

// inProcess is the licensing clients' transport into the in-process server.
type inProcess struct{ target conformance.Target }

func (p inProcess) RoundTrip(req *http.Request) (*http.Response, error) { return p.target.Do(req) }

// env is the booted service and the fixture an admin builds in it.
type env struct {
	t        *testing.T
	target   *conformance.HandlerTarget
	slug     string
	admin    string
	publicHx string

	productID  string
	licenseID  string
	licenseKey string
	productTok string
}

func boot(t *testing.T) *env {
	t.Helper()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIC_TEST_DATABASE_URL (make up) to run the end-to-end tests")
	}

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	id := uuid.New()
	spec := accounts.Spec{ID: id, Slug: "e2e-" + id.String()[:8], Name: "e2e " + id.String()[:8]}

	if _, err := accounts.Bootstrap(t.Context(), store.Accounts, spec); err != nil {
		t.Fatal(err)
	}

	admin, err := testkit.SeedAdmin(t.Context(), store, spec.ID, "admin@e2e.example.com")
	if err != nil {
		t.Fatal(err)
	}

	handler, err := service.Handler(store, spec, service.HTTP{}, "keygen.test", "e2e")
	if err != nil {
		t.Fatal(err)
	}

	e := &env{
		t: t, target: &conformance.HandlerTarget{Handler: handler, AdminToken: admin}, slug: spec.Slug, admin: admin,
	}
	e.publicHx = e.accountKey(spec.ID.String())

	return e
}

// call sends an authenticated JSON:API request as the admin and answers the status and body.
func (e *env) call(method, path, body string) (int, []byte) {
	e.t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := e.target.NewRequest(e.t.Context(), method, path, reader)
	if err != nil {
		e.t.Fatal(err)
	}

	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Authorization", "Bearer "+e.admin)

	if body != "" {
		req.Header.Set("Content-Type", "application/vnd.api+json")
	}

	resp, err := e.target.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}

	raw, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err != nil || closeErr != nil {
		e.t.Fatal(err, closeErr)
	}

	return resp.StatusCode, raw
}

// document is the slice of a JSON:API document the fixture reads.
type document struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			Key   string `json:"key"`
			Token string `json:"token"`
		} `json:"attributes"`
		Meta struct {
			Keys struct {
				Ed25519 string `json:"ed25519"`
			} `json:"keys"`
		} `json:"meta"`
	} `json:"data"`
}

func (e *env) create(path, body string) document {
	e.t.Helper()

	status, raw := e.call(http.MethodPost, path, body)
	// Keygen answers a token with 200 and every other resource with 201.
	if status != http.StatusCreated && status != http.StatusOK {
		e.t.Fatalf("POST %s = %d: %s", path, status, raw)
	}

	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		e.t.Fatal(err)
	}

	return doc
}

// accountKey reads the account's Ed25519 public key (base64 of hex) as hex.
func (e *env) accountKey(accountID string) string {
	e.t.Helper()

	status, raw := e.call(http.MethodGet, "/v1/accounts/"+accountID, "")
	if status != http.StatusOK {
		e.t.Fatalf("account = %d: %s", status, raw)
	}

	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		e.t.Fatal(err)
	}

	decoded, err := base64.StdEncoding.DecodeString(doc.Data.Meta.Keys.Ed25519)
	if err != nil {
		e.t.Fatal(err)
	}

	return string(decoded)
}

// fixture creates the product, policy, license and product token as the admin.
func (e *env) fixture() {
	e.t.Helper()

	base := "/v1/accounts/" + e.slug

	product := e.create(base+"/products", `{"data":{"type":"product","attributes":{"name":"e2e product"}}}`)
	policy := e.create(base+"/policies", `{"data":{"type":"policy","attributes":{"name":"e2e policy",`+
		`"scheme":"ED25519_SIGN","authenticationStrategy":"LICENSE","floating":true,"maxMachines":3},`+
		`"relationships":{"product":{"data":{"type":"product","id":"`+product.Data.ID+`"}}}}}`)
	license := e.create(base+"/licenses", `{"data":{"type":"license","relationships":{"policy":{"data":`+
		`{"type":"policy","id":"`+policy.Data.ID+`"}}}}}`)
	token := e.create(base+"/products/"+product.Data.ID+"/tokens", `{"data":{"type":"tokens"}}`)

	e.productID, e.licenseID, e.licenseKey = product.Data.ID, license.Data.ID, license.Data.Attributes.Key
	e.productTok = token.Data.Attributes.Token

	if e.licenseKey == "" || e.productTok == "" {
		e.t.Fatalf("fixture lacks a license key or product token: %+v %+v", license, token)
	}
}

func (e *env) client(cfg licensing.Config) *licensing.Client {
	e.t.Helper()

	cfg.APIURL, cfg.PublicKey = origin, e.publicHx
	cfg.HTTPClient = &http.Client{Transport: inProcess{target: e.target}}

	client, err := licensing.New(cfg)
	if err != nil {
		e.t.Fatal(err)
	}

	return client
}

func TestMachineLifecycleEndToEnd(t *testing.T) {
	t.Parallel()

	e := boot(t)
	e.fixture()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	machine := e.activate(key)
	e.checkOut(machine, key)
	e.updateMetadata(key)
	e.signedRequests(key, machine)
	reports := e.reportUsage(machine, key)
	e.readUsage(key, reports)
}

// activate puts the installation on the license (step 2).
func (e *env) activate(key installation.Key) *licensing.Machine {
	e.t.Helper()

	license, err := e.client(licensing.Config{LicenseKey: e.licenseKey}).Me(e.t.Context())
	if err != nil {
		e.t.Fatal(err)
	}

	machine, err := installation.Activate(e.t.Context(), license, key, licensing.WithMachineName("e2e installation"))
	if err != nil {
		e.t.Fatal(err)
	}

	if machine.Fingerprint != key.Fingerprint() || machine.LicenseID != e.licenseID || machine.Name != "e2e installation" {
		e.t.Fatalf("machine = %+v", machine)
	}

	public, err := installation.PublicKeyOf(machine)
	if err != nil || !public.Equal(key.Public()) {
		e.t.Fatalf("public key = %x, %v", public, err)
	}

	if machine.HeartbeatStatus != "NOT_STARTED" {
		e.t.Errorf("heartbeat status = %s", machine.HeartbeatStatus)
	}

	// The same installation cannot take a second seat of the license.
	if _, err := installation.Activate(e.t.Context(), license, key); err == nil {
		e.t.Error("a duplicate fingerprint was activated twice")
	}

	return machine
}

// checkOut gets the machine file and verifies and decrypts it (step 3).
func (e *env) checkOut(machine *licensing.Machine, key installation.Key) {
	e.t.Helper()

	verifier, err := licensing.NewVerifier(e.publicHx)
	if err != nil {
		e.t.Fatal(err)
	}

	encrypted, err := machine.Checkout(e.t.Context())
	if err != nil {
		e.t.Fatal(err)
	}

	if err := encrypted.Verify(verifier); err != nil {
		e.t.Fatalf("machine file signature: %v", err)
	}

	dataset, err := encrypted.Decrypt(e.licenseKey, key.Fingerprint())
	if err != nil {
		e.t.Fatal(err)
	}

	if dataset.Machine.ID != machine.ID || dataset.Machine.Fingerprint != key.Fingerprint() ||
		dataset.License.ID != e.licenseID || dataset.Expiry == nil {
		e.t.Errorf("dataset = machine %s fp %s license %s", dataset.Machine.ID, dataset.Machine.Fingerprint, dataset.License.ID)
	}

	if _, err := encrypted.Decrypt("not the license key", key.Fingerprint()); err == nil {
		e.t.Error("a machine file decrypted with the wrong key")
	}

	plain, err := machine.Checkout(e.t.Context(), licensing.CheckoutEncrypt(false))
	if err != nil {
		e.t.Fatal(err)
	}

	decoded, err := plain.Decode()
	if err != nil || decoded.Machine.ID != machine.ID {
		e.t.Errorf("plain dataset = %+v, %v", decoded, err)
	}

	// The ttl minimum is one hour: below it is refused, not stamped.
	var refused *licensing.APIError

	_, err = machine.Checkout(e.t.Context(), licensing.CheckoutTTL(time.Hour-time.Second))
	if !errors.As(err, &refused) || refused.Status != http.StatusBadRequest || refused.Code != "CHECKOUT_TTL_INVALID" {
		e.t.Errorf("a check-out below the minimum ttl: %v", err)
	}

	if _, err := machine.Checkout(e.t.Context(), licensing.CheckoutTTL(time.Hour)); err != nil {
		e.t.Errorf("a check-out at the minimum ttl: %v", err)
	}
}

// updateMetadata sets vendor metadata with the product token and finds it
// in the signed machine file, the installation's own keys kept.
func (e *env) updateMetadata(key installation.Key) {
	e.t.Helper()

	ctx := e.t.Context()

	vendor, err := e.client(licensing.Config{Token: e.productTok}).Machine(ctx, key.Fingerprint())
	if err != nil {
		e.t.Fatal(err)
	}

	err = vendor.UpdateMetadata(ctx, licensing.Metadata{
		"paymentDeadline": json.RawMessage(`"2026-12-01T00:00:00Z"`),
		"mode":            json.RawMessage(`"online"`),
		"ack":             json.RawMessage(`{"epoch":7,"seq":2,"hash":"abcd"}`),
	})
	if err != nil {
		e.t.Fatal(err)
	}

	if err := vendor.UpdateMetadata(ctx, licensing.Metadata{"mode": json.RawMessage(`null`)}); err != nil {
		e.t.Fatal(err)
	}

	if _, ok := vendor.Metadata["mode"]; ok {
		e.t.Errorf("a null value kept its key: %s", vendor.Metadata["mode"])
	}

	file, err := vendor.Checkout(ctx, licensing.CheckoutEncrypt(false))
	if err != nil {
		e.t.Fatal(err)
	}

	dataset, err := file.Decode()
	if err != nil {
		e.t.Fatal(err)
	}

	signed := dataset.Machine.Metadata
	if deadline, _ := signed.String("paymentDeadline"); deadline != "2026-12-01T00:00:00Z" {
		e.t.Errorf("signed metadata = %s", signed)
	}

	var ack struct {
		Epoch int    `json:"epoch"`
		Seq   int    `json:"seq"`
		Hash  string `json:"hash"`
	}

	if err := json.Unmarshal(signed["ack"], &ack); err != nil || ack.Epoch != 7 || ack.Seq != 2 || ack.Hash != "abcd" {
		e.t.Errorf("signed ack = %s, %v", signed["ack"], err)
	}

	if fp, _ := signed.String("fingerprint"); fp != key.Fingerprint() {
		e.t.Errorf("fingerprint metadata lost: %q", fp)
	}

	if err := vendor.UpdateMetadata(ctx, licensing.Metadata{"publicKey": json.RawMessage(`"x"`)}); !errors.Is(
		err, licensing.ErrReservedMetadata) {
		e.t.Errorf("publicKey update: %v", err)
	}
}

// signedRequests passes requests signed with the installation key through
// the authenticator wired to the in-process licensing API (step 4).
// The machine never pinged: it is in the alive scope and authenticates.
func (e *env) signedRequests(key installation.Key, machine *licensing.Machine) {
	e.t.Helper()

	auth, err := request.NewAuthenticator(e.client(licensing.Config{Token: e.productTok}), request.AuthOptions{})
	if err != nil {
		e.t.Fatal(err)
	}

	app := httptest.NewServer(auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inst, ok := request.FromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"machine": inst.MachineID, "license": inst.LicenseID, "fingerprint": inst.Fingerprint, "code": inst.LicenseCode,
		})
	})))
	e.t.Cleanup(app.Close)

	signed := &http.Client{Transport: request.NewTransport(nil, key)}

	for _, test := range []struct {
		name   string
		client *http.Client
		method string
		body   string
		status int
	}{
		{"signed GET", signed, http.MethodGet, "", http.StatusOK},
		{"signed POST with a body", signed, http.MethodPost, `{"hello":"world"}`, http.StatusOK},
		{"unsigned", http.DefaultClient, http.MethodGet, "", http.StatusUnauthorized},
	} {
		req, err := http.NewRequestWithContext(e.t.Context(), test.method, app.URL+"/work", strings.NewReader(test.body))
		if err != nil {
			e.t.Fatal(err)
		}

		resp, err := test.client.Do(req)
		if err != nil {
			e.t.Fatal(err)
		}

		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != test.status {
			e.t.Fatalf("%s = %d: %s", test.name, resp.StatusCode, raw)
		}

		if test.status != http.StatusOK {
			continue
		}

		var got map[string]string
		if err := json.Unmarshal(raw, &got); err != nil {
			e.t.Fatal(err)
		}

		if got["machine"] != machine.ID || got["license"] != e.licenseID || got["fingerprint"] != key.Fingerprint() ||
			got["code"] != "VALID" {
			e.t.Errorf("%s resolved %v", test.name, got)
		}
	}

	// A key the licensing API does not know is refused.
	stranger, err := installation.NewKey()
	if err != nil {
		e.t.Fatal(err)
	}

	req, err := http.NewRequestWithContext(e.t.Context(), http.MethodGet, app.URL+"/work", http.NoBody)
	if err != nil {
		e.t.Fatal(err)
	}

	resp, err := (&http.Client{Transport: request.NewTransport(nil, stranger)}).Do(req)
	if err != nil {
		e.t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		e.t.Errorf("unknown installation = %d", resp.StatusCode)
	}
}

// chain is what the installation reported and what the server acknowledged.
type chain struct {
	first, second usage.Report
	ack1, ack2    usage.Acked
	events        []string
}

// reportUsage reports twice, re-sends the first and checks the idempotent ack
// (step 5).
func (e *env) reportUsage(machine *licensing.Machine, key installation.Key) chain {
	e.t.Helper()

	ctx := e.t.Context()
	journal := usage.NewJournal(usageDomain, key.Fingerprint(), 1, usage.Acked{})
	out := chain{events: []string{"e1", "e2", "e3"}}

	build := func() usage.Report {
		report, err := journal.Report("e2e-build")
		if err != nil {
			e.t.Fatal(err)
		}

		report.Sign(key.PrivateKey())

		return *report
	}
	add := func(events ...string) {
		for _, event := range events {
			if _, _, err := journal.Append([]byte(event)); err != nil {
				e.t.Fatal(err)
			}
		}
	}

	add("e1", "e2")

	out.first = build()

	ack, err := machine.ReportUsage(ctx, &out.first)
	if err != nil {
		e.t.Fatal(err)
	}

	out.ack1 = *ack
	if out.ack1.Epoch != 1 || out.ack1.Seq != 2 || out.ack1.Hash != out.first.Head {
		e.t.Fatalf("first ack = %+v, head %s", out.ack1, out.first.Head)
	}

	// The re-send of the accepted report answers the same ack.
	again, err := machine.ReportUsage(ctx, &out.first)
	if err != nil || *again != out.ack1 {
		e.t.Fatalf("re-send = %+v, %v, want %+v", again, err, out.ack1)
	}

	// The second report continues the chain.
	journal.Ack(out.ack1)
	add("e3")

	out.second = build()
	if out.second.SeqFrom != 3 || out.second.PrevHash != out.ack1.Hash {
		e.t.Fatalf("second report does not continue the first: %+v", out.second)
	}

	ack, err = machine.ReportUsage(ctx, &out.second)
	if err != nil {
		e.t.Fatal(err)
	}

	out.ack2 = *ack
	if out.ack2.Seq != 3 || out.ack2.Hash != out.second.Head {
		e.t.Fatalf("second ack = %+v", out.ack2)
	}

	if again, err = machine.ReportUsage(ctx, &out.second); err != nil || *again != out.ack2 {
		e.t.Fatalf("re-send of the second = %+v, %v", again, err)
	}

	// Once the chain moved on, the first report is a fork.
	_, err = machine.ReportUsage(ctx, &out.first)

	var apiErr *licensing.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != "USAGE_FORK" {
		e.t.Fatalf("stale re-send = %v", err)
	}

	// A report signed by another key is refused.
	other, err := installation.NewKey()
	if err != nil {
		e.t.Fatal(err)
	}

	forged := out.second
	forged.Sign(other.PrivateKey())

	if _, err = machine.ReportUsage(ctx, &forged); !errors.As(err, &apiErr) || apiErr.Code != "USAGE_SIGNATURE_INVALID" {
		e.t.Fatalf("forged report = %v", err)
	}

	return out
}

// readUsage reads the accepted chain as the product (step 6).
func (e *env) readUsage(key installation.Key, reports chain) {
	e.t.Helper()

	ctx := e.t.Context()

	machine, err := e.client(licensing.Config{Token: e.productTok}).Machine(ctx, key.Fingerprint())
	if err != nil {
		e.t.Fatal(err)
	}

	got, err := machine.Usage(ctx, 0, 100)
	if err != nil {
		e.t.Fatal(err)
	}

	if got.Epoch != 1 || got.Head.Seq != 3 || got.Head.Hash != reports.ack2.Hash {
		e.t.Fatalf("usage = %+v", got)
	}

	if len(got.Events) != 3 || len(got.Acks) != 2 || got.Acks[0] != reports.ack1 || got.Acks[1] != reports.ack2 {
		e.t.Fatalf("events = %+v acks = %+v", got.Events, got.Acks)
	}

	first, err := reports.first.IterEvents()
	if err != nil {
		e.t.Fatal(err)
	}

	second, err := reports.second.IterEvents()
	if err != nil {
		e.t.Fatal(err)
	}

	for i, want := range append(first, second...) {
		have := got.Events[i]
		if have.Seq != uint64(i+1) || have.Hash != want.Hash ||
			have.Event != base64.StdEncoding.EncodeToString([]byte(reports.events[i])) {
			e.t.Errorf("event %d = %+v, want seq %d %s", i, have, i+1, want.Hash)
		}
	}

	// The window: after a sequence number, and a limit.
	after, err := machine.Usage(ctx, 2, 100)
	if err != nil || len(after.Events) != 1 || after.Events[0].Seq != 3 || len(after.Acks) != 1 || after.Acks[0] != reports.ack2 {
		e.t.Errorf("after 2 = %+v, %v", after, err)
	}

	limited, err := machine.Usage(ctx, 0, 1)
	if err != nil || len(limited.Events) != 1 || limited.Events[0].Seq != 1 || len(limited.Acks) != 1 {
		e.t.Errorf("limit 1 = %+v, %v", limited, err)
	}

	// The head is hex of the chain: the account never sees raw keys.
	if _, err := hex.DecodeString(got.Head.Hash); err != nil {
		e.t.Errorf("head hash %q: %v", got.Head.Hash, err)
	}
}

// TestFreshActivationValidatesByFingerprint is the documented SDK flow: a license
// that has just activated a machine validates with that machine's fingerprint
// scope, with no heartbeat required by the policy.
func TestFreshActivationValidatesByFingerprint(t *testing.T) {
	t.Parallel()

	e := boot(t)
	e.fixture()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	e.activate(key)

	client := e.client(licensing.Config{LicenseKey: e.licenseKey})
	if _, err := client.Validate(t.Context(), licensing.WithFingerprint(key.Fingerprint())); err != nil {
		t.Fatalf("validate with the fresh machine's fingerprint: %v", err)
	}
}

func TestUsageIsVendorSideOnly(t *testing.T) {
	t.Parallel()

	e := boot(t)
	e.fixture()

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	machine := e.activate(key)

	// The installation (a license bearer) reports but cannot read the journal back.
	_, err = machine.Usage(t.Context(), 0, 10)

	var apiErr *licensing.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("license-bearer read = %v", err)
	}

	// A product of another account has no machine to read.
	foreign := boot(t)
	foreign.fixture()

	stranger, err := foreign.client(licensing.Config{Token: foreign.productTok}).Machine(t.Context(), key.Fingerprint())
	if err == nil {
		t.Fatalf("a foreign account's product found the machine: %+v", stranger)
	}

	// An empty journal reads empty, not as an error.
	product, err := e.client(licensing.Config{Token: e.productTok}).Machine(t.Context(), key.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}

	empty, err := product.Usage(t.Context(), 0, 10)
	if err != nil || len(empty.Events) != 0 || len(empty.Acks) != 0 || empty.Head.Seq != 0 {
		t.Errorf("empty journal = %+v, %v", empty, err)
	}

	// A report for a machine without a key in its metadata is refused with a code.
	plain := e.create("/v1/accounts/"+e.slug+"/machines", `{"data":{"type":"machines","attributes":{"fingerprint":"keyless-`+
		uuid.NewString()+`"},"relationships":{"license":{"data":{"type":"licenses","id":"`+e.licenseID+`"}}}}}`)

	keyless, err := e.client(licensing.Config{Token: e.productTok}).Machine(t.Context(), plain.Data.ID)
	if err != nil {
		t.Fatal(err)
	}

	journal := usage.NewJournal(usageDomain, keyless.Fingerprint, 1, usage.Acked{})
	if _, _, err := journal.Append([]byte("x")); err != nil {
		t.Fatal(err)
	}

	report, err := journal.Report("b")
	if err != nil {
		t.Fatal(err)
	}

	report.Sign(key.PrivateKey())

	if _, err = keyless.ReportUsage(t.Context(), report); !errors.As(err, &apiErr) || apiErr.Code != "USAGE_KEY_MISSING" {
		t.Errorf("keyless report = %v", err)
	}
}
