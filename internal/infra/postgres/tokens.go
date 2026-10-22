package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

	return tokenDomain(row.ID, row.AccountID, row.Digest, row.BearerID, row.BearerType,
		row.BearerRole, row.Kind, row.Name, row.Expiry, row.Permissions,
		row.CreatedAt, row.UpdatedAt), nil
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

func tokenDomain(
	id, accountID uuid.UUID, digest string,
	bearerID *uuid.UUID, bearerType, bearerRole, kind, name *string,
	expiry *time.Time, permissions []byte,
	createdAt, updatedAt time.Time,
) domain.Token {
	role, _ := domain.ParseRole(deref(bearerRole))

	var perms []string

	_ = json.Unmarshal(permissions, &perms)

	token := domain.Token{
		ID:          id,
		AccountID:   accountID,
		Digest:      digest,
		BearerType:  deref(bearerType),
		BearerRole:  role,
		Kind:        deref(kind),
		Name:        name,
		Expiry:      expiry,
		Permissions: perms,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	if bearerID != nil {
		token.BearerID = *bearerID
	}

	return token
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}

	return &id
}
