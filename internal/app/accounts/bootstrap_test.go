package accounts_test

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// RFC 8032's first ed25519 test vector.
const (
	vendorSeed   = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	vendorPublic = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
)

// store keeps the account rows in memory.
type store struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.Account
}

func (s *store) Account(_ context.Context, id uuid.UUID) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.rows[id]
	if !ok {
		return domain.Account{}, accounts.ErrNotFound
	}

	return a, nil
}

func (s *store) InsertAccount(_ context.Context, a domain.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rows[a.ID] = a

	return nil
}

func seed(t *testing.T, hex string) crypto.Ed25519Seed {
	t.Helper()

	s, err := crypto.ParseEd25519Seed(hex)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestBootstrapEd25519Seed(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	other := seed(t, "0000000000000000000000000000000000000000000000000000000000000001")

	tests := []struct {
		name       string
		existing   *crypto.Ed25519Seed
		configured crypto.Ed25519Seed
		wantPublic string
		wantErr    error
	}{
		{name: "created from the seed", configured: seed(t, vendorSeed), wantPublic: vendorPublic},
		{
			name: "existing with the seed's key", existing: new(seed(t, vendorSeed)), configured: seed(t, vendorSeed),
			wantPublic: vendorPublic,
		},
		{
			name: "existing with another key", existing: &other, configured: seed(t, vendorSeed),
			wantErr: accounts.ErrEd25519Mismatch,
		},
		{name: "existing, no seed configured", existing: &other, wantPublic: other.Public()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &store{rows: map[uuid.UUID]domain.Account{}}
			if tc.existing != nil {
				s.rows[id] = domain.Account{ID: id, Keys: crypto.AccountKeys{
					Ed25519Private: tc.existing.Private(), Ed25519Public: tc.existing.Public(),
				}}
			}

			got, err := accounts.Bootstrap(t.Context(), s, accounts.Spec{ID: id, Ed25519: tc.configured})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if err == nil && got.Keys.Ed25519Public != tc.wantPublic {
				t.Fatalf("ed25519 public = %s, want %s", got.Keys.Ed25519Public, tc.wantPublic)
			}
		})
	}
}

// Without a seed every account gets its own generated key.
func TestBootstrapGeneratesKeys(t *testing.T) {
	t.Parallel()

	first, err := accounts.Bootstrap(t.Context(), &store{rows: map[uuid.UUID]domain.Account{}}, accounts.Spec{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}

	second, err := accounts.Bootstrap(t.Context(), &store{rows: map[uuid.UUID]domain.Account{}}, accounts.Spec{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}

	if first.Keys.Ed25519Public == "" || first.Keys.Ed25519Public == second.Keys.Ed25519Public {
		t.Fatal("generated ed25519 keys must be present and differ")
	}
}

// An account created with the vendor's seed signs machine files the
// vendor's public key verifies.
func TestSeededAccountSignsMachineFiles(t *testing.T) {
	t.Parallel()

	account, err := accounts.Bootstrap(t.Context(), &store{rows: map[uuid.UUID]domain.Account{}},
		accounts.Spec{ID: uuid.New(), Ed25519: seed(t, vendorSeed)})
	if err != nil {
		t.Fatal(err)
	}

	certificate, err := crypto.BuildFileCertificate(crypto.FileCertificateInput{
		Keys: account.Keys, Payload: []byte(`{"data":{"type":"machines","id":"m-1"}}`), RNG: rand.Reader,
		Algorithm: "base64+ed25519", Prefix: "machine/",
		Begin: "-----BEGIN MACHINE FILE-----", End: "-----END MACHINE FILE-----",
	})
	if err != nil {
		t.Fatal(err)
	}

	verifier, err := licensing.NewVerifier(vendorPublic)
	if err != nil {
		t.Fatal(err)
	}

	file := licensing.MachineFile{Certificate: certificate}
	if err := file.Verify(verifier); err != nil {
		t.Fatalf("the vendor key does not verify the machine file: %v", err)
	}
}
