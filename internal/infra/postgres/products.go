package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// Page is the storage-layer list window (paging.Page).
type Page = paging.Page

// ProductsRepo adapts the product queries to the products scenario's
// storage port.
type ProductsRepo struct {
	q *db.Queries
}

// ProductByID loads a product by id.
func (r ProductsRepo) ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error) {
	row, err := r.q.GetProductByID(ctx, db.GetProductByIDParams{AccountID: accountID, ID: id})

	return productDomain(row.ID, row.AccountID, row.Name, row.Code, row.DistributionStrategy,
		row.URL, row.Platforms, row.Metadata, row.CreatedAt, row.UpdatedAt, "id", err)
}

// ProductByCode loads a product by its (case-insensitive) code alias.
func (r ProductsRepo) ProductByCode(ctx context.Context, accountID uuid.UUID, code string) (domain.Product, error) {
	row, err := r.q.GetProductByCode(ctx, db.GetProductByCodeParams{AccountID: accountID, Code: &code})

	return productDomain(row.ID, row.AccountID, row.Name, row.Code, row.DistributionStrategy,
		row.URL, row.Platforms, row.Metadata, row.CreatedAt, row.UpdatedAt, "code", err)
}

// InsertProduct writes a new product row.
func (r ProductsRepo) InsertProduct(ctx context.Context, p domain.Product) error {
	err := r.q.InsertProduct(ctx, db.InsertProductParams{
		ID:                   p.ID,
		AccountID:            p.AccountID,
		Name:                 ref(p.Name),
		Code:                 ref(p.Code),
		DistributionStrategy: ref(p.DistributionStrategy),
		URL:                  ref(p.URL),
		Platforms:            platformsJSON(p.Platforms),
		Metadata:             jsonMap(p.Metadata),
	})
	if err != nil {
		return mapProductWrite(p.ID, err)
	}

	return nil
}

// UpdateProduct overwrites the mutable columns.
func (r ProductsRepo) UpdateProduct(ctx context.Context, p domain.Product) error {
	err := r.q.UpdateProduct(ctx, db.UpdateProductParams{
		Name:                 ref(p.Name),
		Code:                 ref(p.Code),
		DistributionStrategy: ref(p.DistributionStrategy),
		URL:                  ref(p.URL),
		Platforms:            platformsJSON(p.Platforms),
		Metadata:             jsonMap(p.Metadata),
		AccountID:            p.AccountID,
		ID:                   p.ID,
	})
	if err != nil {
		return mapProductWrite(p.ID, err)
	}

	return nil
}

// DeleteProduct destroys the product; false when it was gone already.
func (r ProductsRepo) DeleteProduct(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	n, err := r.q.DeleteProduct(ctx, db.DeleteProductParams{AccountID: accountID, ID: id})
	if err != nil {
		return false, fmt.Errorf("postgres: delete product %s: %w", id, err)
	}

	return n > 0, nil
}

// ListAllProducts pages the account's products (the admin-side scope).
func (r ProductsRepo) ListAllProducts(ctx context.Context, accountID uuid.UUID, page Page) ([]domain.Product, error) {
	if page.Asc {
		rows, err := r.q.ListProductsAsc(ctx, db.ListProductsAscParams{
			AccountID: accountID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
		})

		return productList(rows, func(row db.ListProductsAscRow) db.GetProductByIDRow {
			return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
				ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
				DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
				Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}
		}, err)
	}

	rows, err := r.q.ListProductsDesc(ctx, db.ListProductsDescParams{
		AccountID: accountID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	})

	return productListDesc(rows, err)
}

// CountAllProducts counts the admin-side scope.
func (r ProductsRepo) CountAllProducts(ctx context.Context, accountID uuid.UUID) (int, error) {
	row, err := r.q.CountProducts(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("postgres: count products: %w", err)
	}

	return int(row.Count), nil
}

// ListUserProducts pages the user scope: open products UNION the
// products of the user's licenses.
func (r ProductsRepo) ListUserProducts( //nolint:dupl // the scope queries, verbatim
	ctx context.Context, accountID, userID uuid.UUID, page Page,
) ([]domain.Product, error) {
	if page.Asc {
		rows, err := r.q.ListProductsForUserAsc(ctx, db.ListProductsForUserAscParams{
			AccountID: accountID, UserID: userID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
		})

		return productList(rows, func(row db.ListProductsForUserAscRow) db.GetProductByIDRow {
			return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
				ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
				DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
				Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}
		}, err)
	}

	rows, err := r.q.ListProductsForUserDesc(ctx, db.ListProductsForUserDescParams{
		AccountID: accountID, UserID: userID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	})

	return productList(rows, func(row db.ListProductsForUserDescRow) db.GetProductByIDRow {
		return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
			ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
			DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
			Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}, err)
}

// CountUserProducts counts the user scope.
func (r ProductsRepo) CountUserProducts(ctx context.Context, accountID, userID uuid.UUID) (int, error) {
	row, err := r.q.CountProductsForUser(ctx, db.CountProductsForUserParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return 0, fmt.Errorf("postgres: count user products: %w", err)
	}

	return int(row.Count), nil
}

// ListLicenseProducts pages the license scope: open products UNION the
// license's own product.
func (r ProductsRepo) ListLicenseProducts( //nolint:dupl // the scope queries, verbatim
	ctx context.Context, accountID, productID uuid.UUID, page Page,
) ([]domain.Product, error) {
	if page.Asc {
		rows, err := r.q.ListProductsForLicenseAsc(ctx, db.ListProductsForLicenseAscParams{
			AccountID: accountID, ProductID: productID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
		})

		return productList(rows, func(row db.ListProductsForLicenseAscRow) db.GetProductByIDRow {
			return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
				ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
				DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
				Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}
		}, err)
	}

	rows, err := r.q.ListProductsForLicenseDesc(ctx, db.ListProductsForLicenseDescParams{
		AccountID: accountID, ProductID: productID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	})

	return productList(rows, func(row db.ListProductsForLicenseDescRow) db.GetProductByIDRow {
		return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
			ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
			DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
			Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}, err)
}

// CountLicenseProducts counts the license scope.
func (r ProductsRepo) CountLicenseProducts(ctx context.Context, accountID, productID uuid.UUID) (int, error) {
	row, err := r.q.CountProductsForLicense(ctx, db.CountProductsForLicenseParams{
		AccountID: accountID, ProductID: productID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count license products: %w", err)
	}

	return int(row.Count), nil
}

// ListOwnProducts pages the product-bearer scope: the bearer itself.
func (r ProductsRepo) ListOwnProducts( //nolint:dupl // the scope queries, verbatim
	ctx context.Context, accountID, bearerID uuid.UUID, page Page,
) ([]domain.Product, error) {
	if page.Asc {
		rows, err := r.q.ListProductsForProductAsc(ctx, db.ListProductsForProductAscParams{
			AccountID: accountID, BearerID: bearerID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
		})

		return productList(rows, func(row db.ListProductsForProductAscRow) db.GetProductByIDRow {
			return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
				ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
				DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
				Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}
		}, err)
	}

	rows, err := r.q.ListProductsForProductDesc(ctx, db.ListProductsForProductDescParams{
		AccountID: accountID, BearerID: bearerID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	})

	return productList(rows, func(row db.ListProductsForProductDescRow) db.GetProductByIDRow {
		return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
			ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
			DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
			Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}, err)
}

// CountOwnProducts counts the product-bearer scope.
func (r ProductsRepo) CountOwnProducts(ctx context.Context, accountID, bearerID uuid.UUID) (int, error) {
	row, err := r.q.CountProductsForProduct(ctx, db.CountProductsForProductParams{
		AccountID: accountID, BearerID: bearerID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count own products: %w", err)
	}

	return int(row.Count), nil
}

// UserProductIDs answers the user's licensed products.
func (r ProductsRepo) UserProductIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.UserProductIds(ctx, db.UserProductIdsParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: user %s products: %w", userID, err)
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	return ids, nil
}

func productList[T any](rows []T, conv func(T) db.GetProductByIDRow, err error) ([]domain.Product, error) {
	if err != nil {
		return nil, fmt.Errorf("postgres: list products: %w", err)
	}

	out := make([]domain.Product, 0, len(rows))
	for _, row := range rows {
		c := conv(row)

		p, err := productDomain(c.ID, c.AccountID, c.Name, c.Code, c.DistributionStrategy,
			c.URL, c.Platforms, c.Metadata, c.CreatedAt, c.UpdatedAt, "", nil)
		if err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, nil
}

// productListDesc maps the desc rows (the common case) without a
// converter by reusing productList with the identity conversion.
func productListDesc(rows []db.ListProductsDescRow, err error) ([]domain.Product, error) {
	return productList(rows, func(row db.ListProductsDescRow) db.GetProductByIDRow {
		return db.GetProductByIDRow{ //nolint:staticcheck // mirrors the generated row types
			ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
			DistributionStrategy: row.DistributionStrategy, URL: row.URL, Platforms: row.Platforms,
			Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}, err)
}

// productDomain maps the shared product column set; what names the
// lookup in the not-found error.
func productDomain(
	id, accountID uuid.UUID,
	name, code, distributionStrategy, url *string,
	platforms, metadata json.RawMessage,
	createdAt, updatedAt time.Time,
	what string, err error,
) (domain.Product, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, fmt.Errorf("postgres: product %s: %w", what, products.ErrNotFound)
	}

	if err != nil {
		return domain.Product{}, fmt.Errorf("postgres: product %s: %w", what, err)
	}

	var wire []string
	if len(platforms) > 0 {
		_ = json.Unmarshal(platforms, &wire)
	}

	meta := map[string]any{}
	_ = json.Unmarshal(metadata, &meta)

	return domain.Product{
		ID:                   id,
		AccountID:            accountID,
		Name:                 deref(name),
		Code:                 deref(code),
		DistributionStrategy: deref(distributionStrategy),
		URL:                  deref(url),
		Platforms:            wire,
		Metadata:             meta,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
	}, nil
}

func mapProductWrite(id uuid.UUID, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: write product %s: %w", id, products.ErrCodeTaken)
	}

	return fmt.Errorf("postgres: write product %s: %w", id, err)
}

// platformsJSON renders the platforms column: NULL when nil (the wire's
// null), an array otherwise.
func platformsJSON(platforms []string) json.RawMessage {
	if platforms == nil {
		return nil
	}

	raw, err := json.Marshal(platforms)
	if err != nil {
		return json.RawMessage("[]")
	}

	return raw
}
