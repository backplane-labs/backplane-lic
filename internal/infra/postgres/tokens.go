package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// TokensRepo adapts the generated token queries to the authn and tokens
// scenarios' storage ports.
type TokensRepo struct {
	q *db.Queries
}

// TokenByDigest loads a token by its digest.
func (r TokensRepo) TokenByDigest(ctx context.Context, accountID uuid.UUID, digest string) (domain.Token, error) {
	row, err := r.q.GetTokenByDigest(ctx, db.GetTokenByDigestParams{AccountID: accountID, Digest: digest})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Token{}, fmt.Errorf("postgres: token: %w", authn.ErrNotFound)
	}

	if err != nil {
		return domain.Token{}, fmt.Errorf("postgres: token: %w", err)
	}

	return tokenDomain(row), nil
}

// InsertToken writes a new token row.
func (r TokensRepo) InsertToken(ctx context.Context, token domain.Token) error {
	err := r.q.InsertToken(ctx, db.InsertTokenParams{
		ID:          token.ID,
		AccountID:   token.AccountID,
		Digest:      token.Digest,
		BearerID:    uuidPtr(token.BearerID),
		BearerType:  ref(token.BearerType),
		BearerRole:  ref(string(token.BearerRole)),
		Kind:        ref(token.Kind),
		Name:        token.Name,
		Expiry:      token.Expiry,
		Permissions: jsonList(token.Permissions),
	})
	if err != nil {
		return fmt.Errorf("postgres: insert token %s: %w", token.ID, err)
	}

	return nil
}

func tokenDomain(row db.GetTokenByDigestRow) domain.Token {
	role, _ := domain.ParseRole(deref(row.BearerRole))

	var perms []string

	_ = json.Unmarshal(row.Permissions, &perms)

	token := domain.Token{
		ID:          row.ID,
		AccountID:   row.AccountID,
		Digest:      row.Digest,
		BearerType:  deref(row.BearerType),
		BearerRole:  role,
		Kind:        deref(row.Kind),
		Name:        row.Name,
		Expiry:      row.Expiry,
		Permissions: perms,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}

	if row.BearerID != nil {
		token.BearerID = *row.BearerID
	}

	return token
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}

	return &id
}
