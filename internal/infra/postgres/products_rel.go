package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// GetLicenseProductID answers the license bearer's product id.
func (r ProductsRepo) GetLicenseProductID(
	ctx context.Context, accountID, licenseID uuid.UUID,
) (uuid.UUID, error) {
	row, err := r.q.GetLicenseProductID(ctx, db.GetLicenseProductIDParams{
		AccountID: accountID, LicenseID: licenseID,
	})
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("postgres: license %s product: %w", licenseID, err)
	}

	return row.ProductID, nil
}

// ListProductTokens pages the product's tokens.
func (r ProductsRepo) ListProductTokens(
	ctx context.Context, accountID, productID uuid.UUID, page Page,
) ([]domain.Token, error) {
	pid := productID
	args := db.ListProductTokensDescParams{
		AccountID: accountID, ProductID: &pid,
		CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	}

	var (
		rows []db.GetProductTokenRow
		err  error
	)

	if page.Asc {
		var asc []db.ListProductTokensAscRow

		asc, err = r.q.ListProductTokensAsc(ctx, db.ListProductTokensAscParams{ //nolint:staticcheck,lll // mirrors the generated row types
			AccountID: args.AccountID, ProductID: args.ProductID,
			CursorID: args.CursorID, Limit: args.Limit, Offset: args.Offset,
		})

		for _, row := range asc { //nolint:gocritic // mirrors the reference's shape
			rows = append(rows, productTokenRow(row.ID, row.AccountID, row.Digest, row.BearerID,
				row.BearerType, row.BearerRole, row.Kind, row.Name, row.Expiry,
				row.Permissions, row.CreatedAt, row.UpdatedAt))
		}
	} else {
		var desc []db.ListProductTokensDescRow

		desc, err = r.q.ListProductTokensDesc(ctx, args)

		for _, row := range desc { //nolint:gocritic // mirrors the reference's shape
			rows = append(rows, productTokenRow(row.ID, row.AccountID, row.Digest, row.BearerID,
				row.BearerType, row.BearerRole, row.Kind, row.Name, row.Expiry,
				row.Permissions, row.CreatedAt, row.UpdatedAt))
		}
	}

	if err != nil {
		return nil, fmt.Errorf("postgres: product tokens: %w", err)
	}

	out := make([]domain.Token, 0, len(rows))
	for _, row := range rows { //nolint:gocritic // mirrors the reference's shape
		out = append(out, productTokenDomain(row))
	}

	return out, nil
}

// CountProductTokens counts the product's tokens.
func (r ProductsRepo) CountProductTokens(ctx context.Context, accountID, productID uuid.UUID) (int, error) {
	pid := productID
	row, err := r.q.CountProductTokens(ctx, db.CountProductTokensParams{ //nolint:wsl_v5 // dense by design
		AccountID: accountID, ProductID: &pid,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count product tokens: %w", err)
	}

	return int(row.Count), nil
}

// GetProductToken loads one of the product's tokens.
func (r ProductsRepo) GetProductToken(
	ctx context.Context, accountID, productID, tokenID uuid.UUID,
) (domain.Token, error) {
	pid := productID
	row, err := r.q.GetProductToken(ctx, db.GetProductTokenParams{ //nolint:wsl_v5 // dense by design
		AccountID: accountID, ProductID: &pid, TokenID: tokenID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Token{}, fmt.Errorf("postgres: token: %w", products.ErrNotFound)
	}

	if err != nil {
		return domain.Token{}, fmt.Errorf("postgres: token: %w", err)
	}

	return productTokenDomain(productTokenRow(row.ID, row.AccountID, row.Digest, row.BearerID,
		row.BearerType, row.BearerRole, row.Kind, row.Name, row.Expiry,
		row.Permissions, row.CreatedAt, row.UpdatedAt)), nil
}

// ListProductUsers pages the product's users (via its licenses).
func (r ProductsRepo) ListProductUsers(
	ctx context.Context, accountID, productID uuid.UUID, roles []string, page Page,
) ([]domain.User, error) {
	args := db.ListProductUsersDescParams{
		AccountID: accountID, ProductID: productID, Roles: roles,
		CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	}

	var rows []db.GetProductUserByIDRow

	var err error

	if page.Asc {
		var asc []db.ListProductUsersAscRow

		asc, err = r.q.ListProductUsersAsc(ctx, db.ListProductUsersAscParams{ //nolint:staticcheck,lll // mirrors the generated row types
			AccountID: args.AccountID, ProductID: args.ProductID, Roles: args.Roles,
			CursorID: args.CursorID, Limit: args.Limit, Offset: args.Offset,
		})

		for _, row := range asc { //nolint:gocritic // mirrors the reference's shape
			rows = append(rows, db.GetProductUserByIDRow{ //nolint:staticcheck // mirrors the generated row types
				ID: row.ID, AccountID: row.AccountID, FirstName: row.FirstName,
				LastName: row.LastName, Email: row.Email, PasswordDigest: row.PasswordDigest,
				Role: row.Role, Permissions: row.Permissions, GroupID: row.GroupID,
				Metadata: row.Metadata, BannedAt: row.BannedAt,
				CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
		}
	} else {
		var desc []db.ListProductUsersDescRow

		desc, err = r.q.ListProductUsersDesc(ctx, args)

		for _, row := range desc { //nolint:gocritic // mirrors the reference's shape
			rows = append(rows, db.GetProductUserByIDRow{ //nolint:staticcheck // mirrors the generated row types
				ID: row.ID, AccountID: row.AccountID, FirstName: row.FirstName,
				LastName: row.LastName, Email: row.Email, PasswordDigest: row.PasswordDigest,
				Role: row.Role, Permissions: row.Permissions, GroupID: row.GroupID,
				Metadata: row.Metadata, BannedAt: row.BannedAt,
				CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
		}
	}

	if err != nil {
		return nil, fmt.Errorf("postgres: product users: %w", err)
	}

	out := make([]domain.User, 0, len(rows))
	for _, row := range rows { //nolint:gocritic // mirrors the reference's shape
		out = append(out, userDomain(row.ID, row.AccountID, row.FirstName, row.LastName,
			row.Email, row.PasswordDigest, ref(row.Role), row.Permissions, row.GroupID,
			row.Metadata, nil, nil, row.BannedAt, row.CreatedAt, row.UpdatedAt))
	}

	return out, nil
}

// CountProductUsers counts the product's users.
func (r ProductsRepo) CountProductUsers(
	ctx context.Context, accountID, productID uuid.UUID, roles []string,
) (int, error) {
	row, err := r.q.CountProductUsers(ctx, db.CountProductUsersParams{
		AccountID: accountID, ProductID: productID, Roles: roles,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count product users: %w", err)
	}

	return int(row.Count), nil
}

// GetProductUserByID loads one of the product's users by id.
func (r ProductsRepo) GetProductUserByID(
	ctx context.Context, accountID, productID uuid.UUID, roles []string, id uuid.UUID,
) (domain.User, error) {
	row, err := r.q.GetProductUserByID(ctx, db.GetProductUserByIDParams{
		AccountID: accountID, ProductID: productID, Roles: roles, UserID: id,
	})

	return productUser(row, id.String(), err)
}

// GetProductUserByEmail loads one of the product's users by email.
func (r ProductsRepo) GetProductUserByEmail(
	ctx context.Context, accountID, productID uuid.UUID, roles []string, email string,
) (domain.User, error) {
	row, err := r.q.GetProductUserByEmail(ctx, db.GetProductUserByEmailParams{
		AccountID: accountID, ProductID: productID, Roles: roles, Email: &email,
	})

	return productUser(db.GetProductUserByIDRow{ //nolint:staticcheck // mirrors the generated row types
		ID: row.ID, AccountID: row.AccountID, FirstName: row.FirstName,
		LastName: row.LastName, Email: row.Email, PasswordDigest: row.PasswordDigest,
		Role: row.Role, Permissions: row.Permissions, GroupID: row.GroupID,
		Metadata: row.Metadata, BannedAt: row.BannedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, email, err)
}

func productUser(row db.GetProductUserByIDRow, what string, err error) (domain.User, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, fmt.Errorf("postgres: user %s: %w", what, products.ErrNotFound)
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("postgres: user %s: %w", what, err)
	}

	return userDomain(row.ID, row.AccountID, row.FirstName, row.LastName, row.Email,
		row.PasswordDigest, ref(row.Role), row.Permissions, row.GroupID, row.Metadata,
		nil, nil, row.BannedAt, row.CreatedAt, row.UpdatedAt), nil
}

// tokenRow assembles the token row shape shared by the token queries.
func productTokenRow(
	id, accountID uuid.UUID,
	digest string,
	bearerID *uuid.UUID,
	bearerType, bearerRole, kind, name *string,
	expiry *time.Time,
	permissions json.RawMessage,
	createdAt, updatedAt time.Time,
) db.GetProductTokenRow {
	return db.GetProductTokenRow{
		ID: id, AccountID: accountID, Digest: digest, BearerID: bearerID,
		BearerType: bearerType, BearerRole: bearerRole, Kind: kind, Name: name,
		Expiry: expiry, Permissions: permissions, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

// tokenDomain maps the token row to the domain value.
func productTokenDomain(row db.GetProductTokenRow) domain.Token {
	role, _ := domain.ParseRole(deref(row.BearerRole))

	var perms []string
	_ = json.Unmarshal(row.Permissions, &perms) //nolint:wsl_v5 // dense by design

	bearer := uuid.Nil
	if row.BearerID != nil {
		bearer = *row.BearerID
	}

	return domain.Token{
		ID: row.ID, AccountID: row.AccountID, Digest: row.Digest,
		BearerID: bearer, BearerType: deref(row.BearerType), BearerRole: role,
		Kind: deref(row.Kind), Name: row.Name, Expiry: row.Expiry,
		Permissions: perms, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
