package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// KeysRepo adapts the key queries to the keys and policies scenarios'
// storage ports.
type KeysRepo struct {
	q *db.Queries
}

// KeyByID loads a key (with its policy's product) by id.
func (r KeysRepo) KeyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Key, error) {
	row, err := r.q.GetKeyByID(ctx, db.GetKeyByIDParams{AccountID: accountID, ID: id})

	return keyDomain(row.ID, row.AccountID, row.PolicyID, row.ProductID, row.Key,
		row.CreatedAt, row.UpdatedAt, err)
}

// InsertKey writes a new key row.
func (r KeysRepo) InsertKey(ctx context.Context, k domain.Key) error {
	policyID := k.PolicyID
	err := r.q.InsertKey(ctx, db.InsertKeyParams{ //nolint:wsl_v5 // dense by design
		ID: k.ID, AccountID: k.AccountID, PolicyID: &policyID, Key: k.Key,
	})
	if err != nil {
		return mapKeyWrite(k.ID, err)
	}

	return nil
}

// UpdateKey changes the key value.
func (r KeysRepo) UpdateKey(ctx context.Context, accountID, id uuid.UUID, key string) error {
	err := r.q.UpdateKey(ctx, db.UpdateKeyParams{AccountID: accountID, ID: id, Key: key})
	if err != nil {
		return mapKeyWrite(id, err)
	}

	return nil
}

// DeleteKey destroys the key; false when gone.
func (r KeysRepo) DeleteKey(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	n, err := r.q.DeleteKey(ctx, db.DeleteKeyParams{AccountID: accountID, ID: id})
	if err != nil {
		return false, fmt.Errorf("postgres: delete key %s: %w", id, err)
	}

	return n > 0, nil
}

// ListKeys pages the account's keys (the product bearer's post-check
// rejects pages holding anyone else's keys).
func (r KeysRepo) ListKeys(
	ctx context.Context, accountID uuid.UUID, filterPolicyID, filterProductID *uuid.UUID, page Page,
) ([]domain.Key, error) {
	args := db.ListKeysDescParams{
		AccountID: accountID, FilterPolicyID: filterPolicyID, FilterProductID: filterProductID,
		CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	}

	if page.Asc {
		rows, err := r.q.ListKeysAsc(ctx, db.ListKeysAscParams{ //nolint:staticcheck // mirrors the generated row types
			AccountID: args.AccountID, FilterPolicyID: args.FilterPolicyID,
			FilterProductID: args.FilterProductID, CursorID: args.CursorID,
			Limit: args.Limit, Offset: args.Offset,
		})

		return keyList(rows, func(row db.ListKeysAscRow) db.GetKeyByIDRow {
			return db.GetKeyByIDRow(row)
		}, err)
	}

	rows, err := r.q.ListKeysDesc(ctx, args)

	return keyList(rows, func(row db.ListKeysDescRow) db.GetKeyByIDRow {
		return db.GetKeyByIDRow(row)
	}, err)
}

// CountKeys counts the account's keys.
func (r KeysRepo) CountKeys(
	ctx context.Context, accountID uuid.UUID, filterPolicyID, filterProductID *uuid.UUID,
) (int, error) {
	row, err := r.q.CountKeys(ctx, db.CountKeysParams{
		AccountID: accountID, FilterPolicyID: filterPolicyID, FilterProductID: filterProductID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count keys: %w", err)
	}

	return int(row.Count), nil
}

// ListPolicyPool pages one policy's pool.
func (r KeysRepo) ListPolicyPool(
	ctx context.Context, accountID, policyID uuid.UUID, page Page,
) ([]domain.Key, error) {
	pid := policyID
	args := db.ListPolicyPoolDescParams{
		AccountID: accountID, PolicyID: &pid, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	}

	if page.Asc {
		rows, err := r.q.ListPolicyPoolAsc(ctx, db.ListPolicyPoolAscParams{ //nolint:staticcheck,lll // mirrors the generated row types
			AccountID: args.AccountID, PolicyID: args.PolicyID,
			CursorID: args.CursorID, Limit: args.Limit, Offset: args.Offset,
		})

		return keyList(rows, func(row db.ListPolicyPoolAscRow) db.GetKeyByIDRow {
			return db.GetKeyByIDRow(row)
		}, err)
	}

	rows, err := r.q.ListPolicyPoolDesc(ctx, args)

	return keyList(rows, func(row db.ListPolicyPoolDescRow) db.GetKeyByIDRow {
		return db.GetKeyByIDRow(row)
	}, err)
}

// CountPolicyPool counts one policy's pool.
func (r KeysRepo) CountPolicyPool(ctx context.Context, accountID, policyID uuid.UUID) (int, error) {
	pid := policyID
	row, err := r.q.CountPolicyPool(ctx, db.CountPolicyPoolParams{AccountID: accountID, PolicyID: &pid}) //nolint:wsl_v5,lll // dense by design
	if err != nil {
		return 0, fmt.Errorf("postgres: count pool: %w", err)
	}

	return int(row.Count), nil
}

// PopPolicyKey destroys the newest pooled key and answers it.
func (r KeysRepo) PopPolicyKey(ctx context.Context, accountID, policyID uuid.UUID) (domain.Key, error) {
	pid := policyID
	row, err := r.q.PopPolicyKey(ctx, db.PopPolicyKeyParams{AccountID: accountID, PolicyID: &pid}) //nolint:wsl_v5,lll // dense by design
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Key{}, fmt.Errorf("postgres: pop: %w", keys.ErrPoolEmpty)
	}

	if err != nil {
		return domain.Key{}, fmt.Errorf("postgres: pop: %w", err)
	}

	// The popped key carries the policy's product (loaded by the
	// caller, which holds the policy row).
	return domain.Key{
		ID: row.ID, AccountID: row.AccountID, PolicyID: *row.PolicyID,
		Key: row.Key, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func keyList[T any](rows []T, conv func(T) db.GetKeyByIDRow, err error) ([]domain.Key, error) {
	if err != nil {
		return nil, fmt.Errorf("postgres: list keys: %w", err)
	}

	out := make([]domain.Key, 0, len(rows))
	for _, row := range rows {
		c := conv(row)

		k, err := keyDomain(c.ID, c.AccountID, c.PolicyID, c.ProductID, c.Key, c.CreatedAt, c.UpdatedAt, nil)
		if err != nil {
			return nil, err
		}

		out = append(out, k)
	}

	return out, nil
}

func keyDomain(
	id, accountID uuid.UUID, policyID *uuid.UUID, productID uuid.UUID, key string,
	createdAt, updatedAt time.Time, err error,
) (domain.Key, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Key{}, fmt.Errorf("postgres: key: %w", keys.ErrNotFound)
	}

	if err != nil {
		return domain.Key{}, fmt.Errorf("postgres: key: %w", err)
	}

	policy := uuid.Nil
	if policyID != nil {
		policy = *policyID
	}

	return domain.Key{
		ID: id, AccountID: accountID, PolicyID: policy, ProductID: productID,
		Key: key, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func mapKeyWrite(id uuid.UUID, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: write key %s: %w", id, keys.ErrTaken)
	}

	return fmt.Errorf("postgres: write key %s: %w", id, err)
}

// KeyIsLicenseID answers a collision with a license's identifier.
func (r KeysRepo) KeyIsLicenseID(ctx context.Context, accountID uuid.UUID, key string) (bool, error) {
	row, err := r.q.KeyIsLicenseID(ctx, db.KeyIsLicenseIDParams{AccountID: accountID, Key: key})
	if err != nil {
		return false, fmt.Errorf("postgres: key license-id conflict: %w", err)
	}

	return row.Column, nil
}

// KeyIsLicenseKey answers a collision with a license's key.
func (r KeysRepo) KeyIsLicenseKey(ctx context.Context, accountID uuid.UUID, key string) (bool, error) {
	row, err := r.q.KeyIsLicenseKey(ctx, db.KeyIsLicenseKeyParams{AccountID: accountID, Key: key})
	if err != nil {
		return false, fmt.Errorf("postgres: key license-key conflict: %w", err)
	}

	return row.Column, nil
}
