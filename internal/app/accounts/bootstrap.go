// Package accounts is the account scenario: the single account's
// bootstrap and (later) its settings.
package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// ErrNotFound: no account row with that id.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// CurrentAPIVersion is the only API version the clone serves.
const CurrentAPIVersion = "1.8"

// Store is the consumer-side storage port of the scenario.
type Store interface {
	// Account loads the account by id; ErrNotFound when there is none.
	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
	// InsertAccount writes a new account row.
	InsertAccount(ctx context.Context, account domain.Account) error
}

// Spec is the configured account: the row is created from it when missing.
type Spec struct {
	ID   uuid.UUID
	Name string
	Slug string
	// Ed25519 is the imported signing seed; zero: the key is generated.
	Ed25519 crypto.Ed25519Seed
}

// ErrEd25519Mismatch: the account exists with an ed25519 key other than
// the configured seed's; the key is never rotated silently.
var ErrEd25519Mismatch = errors.New(
	"accounts: the account's ed25519 key is not the configured account.ed25519_seed (it is never rotated)")

// Bootstrap makes sure the configured account exists, creating its row
// with a fresh set of generated keys (the ed25519 one from the configured
// seed, when there is one) when it does not. The account is immutable
// afterwards except through the API.
func Bootstrap(ctx context.Context, store Store, spec Spec) (domain.Account, error) {
	account, err := store.Account(ctx, spec.ID)
	switch {
	case err == nil:
		return spec.matches(account)
	case errors.Is(err, ErrNotFound):
	default:
		return domain.Account{}, fmt.Errorf("accounts: bootstrap: load: %w", err)
	}

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		return domain.Account{}, fmt.Errorf("accounts: bootstrap: %w", err)
	}

	if !spec.Ed25519.IsZero() {
		keys.Ed25519Private, keys.Ed25519Public = spec.Ed25519.Private(), spec.Ed25519.Public()
	}

	account = domain.Account{
		ID:         spec.ID,
		Name:       spec.Name,
		Slug:       spec.Slug,
		APIVersion: CurrentAPIVersion,
		Protected:  true,
		Keys:       keys,
	}

	if err := store.InsertAccount(ctx, account); err != nil {
		// Another replica may have won the race: fall back to its row.
		if existing, getErr := store.Account(ctx, spec.ID); getErr == nil {
			return spec.matches(existing)
		}

		return domain.Account{}, fmt.Errorf("accounts: bootstrap: insert: %w", err)
	}

	return account, nil
}

// matches answers the existing account when its ed25519 key is the
// configured seed's (or none is configured).
func (s Spec) matches(account domain.Account) (domain.Account, error) {
	if s.Ed25519.IsZero() {
		return account, nil
	}

	if s.Ed25519.Public() != account.Keys.Ed25519Public {
		return domain.Account{}, ErrEd25519Mismatch
	}

	return account, nil
}
