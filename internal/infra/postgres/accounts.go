package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// AccountsRepo adapts the generated account queries to the accounts
// scenario's storage port.
type AccountsRepo struct {
	q *db.Queries
}

// Account loads the account by id.
func (r AccountsRepo) Account(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	row, err := r.q.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Account{}, fmt.Errorf("postgres: account %s: %w", id, accounts.ErrNotFound)
		}

		return domain.Account{}, fmt.Errorf("postgres: account %s: %w", id, err)
	}

	return domain.Account{
		ID:         row.ID,
		Name:       deref(row.Name),
		Slug:       deref(row.Slug),
		APIVersion: deref(row.APIVersion),
		Protected:  row.Protected,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
		Keys:       accountKeys(row),
	}, nil
}

// InsertAccount writes a new account row.
func (r AccountsRepo) InsertAccount(ctx context.Context, account domain.Account) error {
	err := r.q.InsertAccount(ctx, db.InsertAccountParams{
		ID:                account.ID,
		Name:              ref(account.Name),
		Slug:              ref(account.Slug),
		APIVersion:        ref(account.APIVersion),
		Protected:         account.Protected,
		SecretKey:         account.Keys.SecretKey,
		PrivateKey:        account.Keys.RSAPrivate,
		PublicKey:         account.Keys.RSAPublic,
		Ed25519PrivateKey: account.Keys.Ed25519Private,
		Ed25519PublicKey:  account.Keys.Ed25519Public,
		EcdsaPrivateKey:   account.Keys.ECDSAPrivate,
		EcdsaPublicKey:    account.Keys.ECDSAPublic,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert account %s: %w", account.ID, err)
	}

	return nil
}

func accountKeys(row db.GetAccountRow) crypto.AccountKeys {
	return crypto.AccountKeys{
		SecretKey:      row.SecretKey,
		RSAPrivate:     row.PrivateKey,
		RSAPublic:      row.PublicKey,
		Ed25519Private: row.Ed25519PrivateKey,
		Ed25519Public:  row.Ed25519PublicKey,
		ECDSAPrivate:   row.EcdsaPrivateKey,
		ECDSAPublic:    row.EcdsaPublicKey,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func ref(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
