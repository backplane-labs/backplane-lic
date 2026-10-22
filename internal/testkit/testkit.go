// Package testkit holds the helpers tests share: creating the
// bootstrap admin user and her token directly in the database. Tests do
// not spend password-login throttle budgets to bootstrap themselves.
package testkit

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// SeedAdmin creates the account's bootstrap admin user when her email
// is not taken (idempotent per email) and answers the raw secret of an
// admin token issued directly in the database, independently of Basic
// authentication or its throttle.
func SeedAdmin(ctx context.Context, db *postgres.Store, accountID uuid.UUID, email string) (string, error) {
	scenario := users.New(db, events.None{}, time.Now)

	if err := scenario.SeedAdmin(ctx, accountID, email); err != nil {
		return "", err //nolint:wrapcheck // the scenario's own error
	}

	account, err := db.Account(ctx, accountID)
	if err != nil {
		return "", err //nolint:wrapcheck // the store's own error
	}

	user, err := db.UserByEmail(ctx, accountID, email)
	if err != nil {
		return "", err //nolint:wrapcheck // the store's own error
	}

	raw, err := crypto.GenerateTokenSecret(rand.Reader, string(domain.RoleAdmin))
	if err != nil {
		return "", err //nolint:wrapcheck // the crypto's own error
	}

	now := time.Now()

	token := domain.Token{
		ID:          uuid.New(),
		AccountID:   accountID,
		Digest:      crypto.DigestV3(account.Keys.SecretKey, raw),
		BearerID:    user.ID,
		BearerType:  "User",
		BearerRole:  domain.RoleAdmin,
		Kind:        crypto.TokenKind(string(domain.RoleAdmin)),
		Permissions: []string{"*"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := db.InsertToken(ctx, token); err != nil {
		return "", err //nolint:wrapcheck // the store's own error
	}

	return raw, nil
}
