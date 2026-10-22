package automation //nolint:testpackage // verifies the credential projection shared by activities and mail

import (
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestVisibleLicenseKey(t *testing.T) {
	t.Parallel()

	plain := "one-time-plaintext"
	for _, tc := range []struct {
		name    string
		license domain.License
		want    string
	}{
		{name: "plain", license: domain.License{Key: plain}, want: plain},
		{name: "legacy digest", license: domain.License{Key: "bcrypt-digest", Policy: domain.Policy{Scheme: "LEGACY_ENCRYPT"}}},
		{name: "legacy create", license: domain.License{
			Key: "bcrypt-digest", RawKey: &plain,
			Policy: domain.Policy{Scheme: "LEGACY_ENCRYPT"},
		}, want: plain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := visibleLicenseKey(tc.license); got != tc.want {
				t.Fatalf("key = %q, want %q", got, tc.want)
			}
		})
	}
}
