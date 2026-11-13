package lictest_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/testkit"
	"github.com/backplane-labs/backplane-lic/pkg/licensing"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/lictest"
)

// A vendor's flow on a fresh database: the installation activates with the
// license key, the vendor sets machine metadata with a product token, and
// the signed machine file carries it and verifies with the account key.
func TestVendorFlow(t *testing.T) {
	t.Parallel()

	srv := lictest.Start(t, testkit.Database(t.Context(), t))

	product := srv.Product(t, "app")
	policy := srv.Policy(t, product, lictest.Attributes{"maxMachines": 1, "authenticationStrategy": "LICENSE"})
	license := srv.License(t, policy, lictest.Attributes{"metadata": map[string]any{"plan": "pro"}})

	installed, err := srv.Client(t, licensing.Config{LicenseKey: license.Key}).Me(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if plan, _ := installed.Metadata.String("plan"); plan != "pro" || installed.ID != license.ID {
		t.Fatalf("license = %s %v", installed.ID, installed.Metadata)
	}

	key, err := installation.NewKey()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := installation.Activate(t.Context(), installed, key); err != nil {
		t.Fatal(err)
	}

	vendor := srv.Client(t, licensing.Config{Token: srv.ProductToken(t, product)})

	machine, err := vendor.Machine(t.Context(), key.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}

	if err := machine.UpdateMetadata(t.Context(), licensing.Metadata{"mode": json.RawMessage(`"online"`)}); err != nil {
		t.Fatal(err)
	}

	file, err := machine.Checkout(t.Context(), licensing.CheckoutEncrypt(false))
	if err != nil {
		t.Fatal(err)
	}

	verifier, err := licensing.NewVerifier(srv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	if err := file.Verify(verifier); err != nil {
		t.Fatalf("machine file signature: %v", err)
	}

	dataset, err := file.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if mode, _ := dataset.Machine.Metadata.String("mode"); mode != "online" {
		t.Fatalf("signed machine metadata = %v", dataset.Machine.Metadata)
	}
}

// Servers on one database live in accounts of their own.
func TestServersShareADatabase(t *testing.T) {
	t.Parallel()

	dsn := testkit.Database(t.Context(), t)
	first, second := lictest.Start(t, dsn), lictest.Start(t, dsn)

	if first.AccountID == second.AccountID || first.PublicKey == second.PublicKey {
		t.Fatal("two servers share an account")
	}

	first.Product(t, "only in the first")

	status, raw := second.Do(t, http.MethodGet, "/products", nil)

	var doc struct {
		Data []json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(raw, &doc); status != http.StatusOK || err != nil || len(doc.Data) != 0 {
		t.Fatalf("second account's products = %d %s %v", status, raw, err)
	}
}
