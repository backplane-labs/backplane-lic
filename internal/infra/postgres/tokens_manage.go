package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// tokenRow is the common shape of every token query row (the generated
// row structs share the columns; this adapter keeps one mapping).
type tokenRow struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	Digest           string
	BearerID         *uuid.UUID
	BearerType       *string
	BearerRole       *string
	Kind             *string
	Name             *string
	Expiry           *time.Time
	Permissions      []byte
	MaxActivations   *int32
	Activations      int32
	MaxDeactivations *int32
	Deactivations    int32
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TokenByID loads a token by its id.
func (r TokensRepo) TokenByID(ctx context.Context, accountID, id uuid.UUID) (domain.Token, error) {
	row, err := r.q.GetTokenByID(ctx, db.GetTokenByIDParams{AccountID: accountID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Token{}, notFoundToken(id)
	}

	if err != nil {
		return domain.Token{}, fmt.Errorf("postgres: token %s: %w", id, err)
	}

	return tokenFull(tokenRow{
		ID:               row.ID,
		AccountID:        row.AccountID,
		Digest:           row.Digest,
		BearerID:         row.BearerID,
		BearerType:       row.BearerType,
		BearerRole:       row.BearerRole,
		Kind:             row.Kind,
		Name:             row.Name,
		Expiry:           row.Expiry,
		Permissions:      row.Permissions,
		MaxActivations:   row.MaxActivations,
		Activations:      row.Activations,
		MaxDeactivations: row.MaxDeactivations,
		Deactivations:    row.Deactivations,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}), nil
}

// ListTokens answers the filtered, scoped page.
func (r TokensRepo) ListTokens(
	ctx context.Context, accountID uuid.UUID, f tokens.Filter, scopeKind string, scopeID uuid.UUID, p paging.Params,
) ([]domain.Token, error) {
	scope := (*string)(nil)
	if scopeKind != "" {
		scope = &scopeKind
	}

	rows, err := r.q.ListTokens(ctx, db.ListTokensParams{
		AccountID:   accountID,
		BearerType:  strPtr(f.BearerType),
		BearerID:    nullUUIDPtr(f.BearerID),
		BearerRoles: f.BearerRoles,
		ScopeKind:   scope,
		ScopeID:     scopeID,
		UserBearer:  nil,
		Cursor:      cursorPtr(p.Cursor),
		Asc:         p.Order == paging.OrderAsc,
		Limit:       int64(p.SizeOrDefault()),
		Offset:      int64(offsetOf(p)),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list tokens: %w", err)
	}

	out := make([]domain.Token, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, tokenFull(tokenRow{
			ID:               row.ID,
			AccountID:        row.AccountID,
			Digest:           row.Digest,
			BearerID:         row.BearerID,
			BearerType:       row.BearerType,
			BearerRole:       row.BearerRole,
			Kind:             row.Kind,
			Name:             row.Name,
			Expiry:           row.Expiry,
			Permissions:      row.Permissions,
			MaxActivations:   row.MaxActivations,
			Activations:      row.Activations,
			MaxDeactivations: row.MaxDeactivations,
			Deactivations:    row.Deactivations,
			CreatedAt:        row.CreatedAt,
			UpdatedAt:        row.UpdatedAt,
		}))
	}

	return out, nil
}

// CountTokens answers the filtered, scoped total.
func (r TokensRepo) CountTokens(
	ctx context.Context, accountID uuid.UUID, f tokens.Filter, scopeKind string, scopeID uuid.UUID,
) (int, error) {
	scope := (*string)(nil)
	if scopeKind != "" {
		scope = &scopeKind
	}

	row, err := r.q.CountTokens(ctx, db.CountTokensParams{
		AccountID:   accountID,
		BearerType:  strPtr(f.BearerType),
		BearerID:    nullUUIDPtr(f.BearerID),
		BearerRoles: f.BearerRoles,
		ScopeKind:   scope,
		ScopeID:     scopeID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count tokens: %w", err)
	}

	return int(row.Total), nil
}

// RegenerateToken writes the new digest (and the reset expiry).
func (r TokensRepo) RegenerateToken(
	ctx context.Context, accountID, id uuid.UUID, digest string, expiry *time.Time,
) error {
	err := r.q.RegenerateToken(ctx, db.RegenerateTokenParams{
		AccountID: accountID, ID: id, Digest: digest, Expiry: expiry,
	})
	if err != nil {
		return fmt.Errorf("postgres: regenerate token %s: %w", id, err)
	}

	return nil
}

// DeleteToken removes the row.
func (r TokensRepo) DeleteToken(ctx context.Context, accountID, id uuid.UUID) error {
	err := r.q.DeleteToken(ctx, db.DeleteTokenParams{AccountID: accountID, ID: id})
	if err != nil {
		return fmt.Errorf("postgres: delete token %s: %w", id, err)
	}

	return nil
}

// UserLicensedToProduct answers the product-scope membership check.
func (r TokensRepo) UserLicensedToProduct(
	ctx context.Context, accountID, userID, productID uuid.UUID,
) (bool, error) {
	row, err := r.q.UserLicensedToProduct(ctx, db.UserLicensedToProductParams{
		AccountID: accountID, UserID: userID, ProductID: productID,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: licensed check: %w", err)
	}

	return row.Licensed, nil
}

// LicenseOfProduct answers whether the license belongs to the product.
func (r TokensRepo) LicenseOfProduct(
	ctx context.Context, accountID, licenseID, productID uuid.UUID,
) (bool, error) {
	row, err := r.q.LicenseOfProduct(ctx, db.LicenseOfProductParams{
		AccountID: accountID, LicenseID: licenseID, ProductID: productID,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: product check: %w", err)
	}

	return row.Owned, nil
}

func tokenFull(row tokenRow) domain.Token {
	token := tokenDomain(row.ID, row.AccountID, row.Digest, row.BearerID, row.BearerType,
		row.BearerRole, row.Kind, row.Name, row.Expiry, row.Permissions,
		row.CreatedAt, row.UpdatedAt)
	token.MaxActivations = intPtr(row.MaxActivations)
	token.MaxDeactivations = intPtr(row.MaxDeactivations)
	token.Activations = int(row.Activations)
	token.Deactivations = int(row.Deactivations)

	return token
}

func notFoundToken(id uuid.UUID) error {
	return fmt.Errorf("postgres: token %s: %w", id, domain.ErrNotFound)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
