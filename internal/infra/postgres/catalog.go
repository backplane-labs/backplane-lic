package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// The declared catalog's storage port (internal/app/catalog).

// LockCatalog takes the account's catalog lock until the transaction ends.
func (s *Store) LockCatalog(ctx context.Context, accountID uuid.UUID) error {
	if err := s.Q.LockCatalog(ctx, accountID.String()); err != nil {
		return fmt.Errorf("postgres: catalog lock: %w", err)
	}

	return nil
}

// ProductIDs lists the account's product ids.
func (s *Store) ProductIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.Q.ListCatalogProductIds(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("postgres: catalog products: %w", err)
	}

	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}

	return ids, nil
}

// PolicyIDs lists the account's policy ids.
func (s *Store) PolicyIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.Q.ListCatalogPolicyIds(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("postgres: catalog policies: %w", err)
	}

	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}

	return ids, nil
}

// ProductTokens lists the account's product tokens, oldest first.
func (s *Store) ProductTokens(ctx context.Context, accountID uuid.UUID) ([]domain.Token, error) {
	rows, err := s.Q.ListCatalogProductTokens(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("postgres: catalog tokens: %w", err)
	}

	role := string(domain.RoleProduct)
	product, kind := "Product", crypto.TokenKind(role)
	tokens := make([]domain.Token, len(rows))

	for i, row := range rows {
		tokens[i] = tokenDomain(row.ID, accountID, row.Digest, row.BearerID, &product, &role, &kind,
			row.Name, nil, row.Permissions, row.CreatedAt, row.CreatedAt)
	}

	return tokens, nil
}

// SetDeclaredToken writes the token's digest and permissions.
func (s *Store) SetDeclaredToken(
	ctx context.Context, accountID, id uuid.UUID, digest string, permissions []string,
) error {
	if err := s.Q.SetDeclaredToken(ctx, db.SetDeclaredTokenParams{
		AccountID: accountID, ID: id, Digest: digest, Permissions: jsonList(permissions),
	}); err != nil {
		return fmt.Errorf("postgres: declared token: %w", err)
	}

	return nil
}
