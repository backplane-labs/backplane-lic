package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// EntitlementsRepo adapts the entitlement and policy_entitlements
// queries to the entitlements and policies scenarios' storage ports.
type EntitlementsRepo struct {
	q *db.Queries
	txRunner
}

// EntitlementByID loads an entitlement by id.
func (r EntitlementsRepo) EntitlementByID(ctx context.Context, accountID, id uuid.UUID) (domain.Entitlement, error) {
	row, err := r.q.GetEntitlementByID(ctx, db.GetEntitlementByIDParams{AccountID: accountID, ID: id})

	return entitlementDomain(row, "id", err)
}

// EntitlementByCode loads an entitlement by its (case-insensitive) code.
func (r EntitlementsRepo) EntitlementByCode(ctx context.Context, accountID uuid.UUID, code string) (domain.Entitlement, error) { //nolint:lll // the wire name is long
	row, err := r.q.GetEntitlementByCode(ctx, db.GetEntitlementByCodeParams{AccountID: accountID, Code: code})

	return entitlementDomain(db.GetEntitlementByIDRow{ //nolint:staticcheck // mirrors the generated row types
		ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
		Metadata: row.Metadata, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, "code", err)
}

// InsertEntitlement writes a new entitlement row.
func (r EntitlementsRepo) InsertEntitlement(ctx context.Context, e domain.Entitlement) error {
	err := r.q.InsertEntitlement(ctx, db.InsertEntitlementParams{
		ID: e.ID, AccountID: e.AccountID, Name: e.Name, Code: e.Code, Metadata: jsonMap(e.Metadata),
	})
	if err != nil {
		return mapEntitlementWrite(e.ID, err)
	}

	return nil
}

// UpdateEntitlement overwrites the mutable columns.
func (r EntitlementsRepo) UpdateEntitlement(ctx context.Context, e domain.Entitlement) error {
	err := r.q.UpdateEntitlement(ctx, db.UpdateEntitlementParams{
		Name: e.Name, Code: e.Code, Metadata: jsonMap(e.Metadata), AccountID: e.AccountID, ID: e.ID,
	})
	if err != nil {
		return mapEntitlementWrite(e.ID, err)
	}

	return nil
}

// DeleteEntitlement destroys the entitlement; false when gone.
func (r EntitlementsRepo) DeleteEntitlement(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	n, err := r.q.DeleteEntitlement(ctx, db.DeleteEntitlementParams{AccountID: accountID, ID: id})
	if err != nil {
		return false, fmt.Errorf("postgres: delete entitlement %s: %w", id, err)
	}

	return n > 0, nil
}

// ListEntitlements pages the account-wide entitlement scope.
func (r EntitlementsRepo) ListEntitlements(ctx context.Context, accountID uuid.UUID, page Page) ([]domain.Entitlement, error) { //nolint:lll // the wire name is long
	if page.Asc {
		rows, err := r.q.ListEntitlementsAsc(ctx, db.ListEntitlementsAscParams{
			AccountID: accountID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
		})

		return entitlementList(rows, func(row db.ListEntitlementsAscRow) db.GetEntitlementByIDRow {
			return db.GetEntitlementByIDRow(row)
		}, err)
	}

	rows, err := r.q.ListEntitlementsDesc(ctx, db.ListEntitlementsDescParams{
		AccountID: accountID, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	})

	return entitlementList(rows, func(row db.ListEntitlementsDescRow) db.GetEntitlementByIDRow {
		return db.GetEntitlementByIDRow(row)
	}, err)
}

// CountEntitlements counts the account's entitlements.
func (r EntitlementsRepo) CountEntitlements(ctx context.Context, accountID uuid.UUID) (int, error) {
	row, err := r.q.CountEntitlements(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("postgres: count entitlements: %w", err)
	}

	return int(row.Count), nil
}

// ListEntitlementsByIDs pages only the authorized entitlement IDs.
func (r EntitlementsRepo) ListEntitlementsByIDs(
	ctx context.Context, accountID uuid.UUID, ids []uuid.UUID, page Page,
) ([]domain.Entitlement, error) {
	if page.Asc {
		rows, err := r.q.ListEntitlementsByIdsAsc(ctx, db.ListEntitlementsByIdsAscParams{
			AccountID: accountID, Ids: ids, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
		})

		return entitlementList(rows, func(row db.ListEntitlementsByIdsAscRow) db.GetEntitlementByIDRow {
			return db.GetEntitlementByIDRow(row)
		}, err)
	}

	rows, err := r.q.ListEntitlementsByIdsDesc(ctx, db.ListEntitlementsByIdsDescParams{
		AccountID: accountID, Ids: ids, CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	})

	return entitlementList(rows, func(row db.ListEntitlementsByIdsDescRow) db.GetEntitlementByIDRow {
		return db.GetEntitlementByIDRow(row)
	}, err)
}

// CountEntitlementsByIDs counts only the authorized entitlement IDs.
func (r EntitlementsRepo) CountEntitlementsByIDs(
	ctx context.Context, accountID uuid.UUID, ids []uuid.UUID,
) (int, error) {
	row, err := r.q.CountEntitlementsByIds(ctx, db.CountEntitlementsByIdsParams{AccountID: accountID, Ids: ids})
	if err != nil {
		return 0, fmt.Errorf("postgres: count scoped entitlements: %w", err)
	}

	return int(row.Count), nil
}

// UserEntitlementIDs answers the entitlements of the user's licenses.
func (r EntitlementsRepo) UserEntitlementIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.UserEntitlementIds(ctx, db.UserEntitlementIdsParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: user %s entitlements: %w", userID, err)
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.EntitlementID)
	}

	return ids, nil
}

// LicenseEntitlementIDs answers the entitlements of one license.
func (r EntitlementsRepo) LicenseEntitlementIDs(ctx context.Context, accountID, licenseID uuid.UUID) ([]uuid.UUID, error) { //nolint:lll // the wire name is long
	rows, err := r.q.LicenseEntitlementIds(ctx, db.LicenseEntitlementIdsParams{
		AccountID: accountID, LicenseID: licenseID,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: license %s entitlements: %w", licenseID, err)
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.EntitlementID)
	}

	return ids, nil
}

// InsertPolicyEntitlements writes the join rows of one attach call.
func (r EntitlementsRepo) InsertPolicyEntitlements(
	ctx context.Context, accountID, policyID, joinID uuid.UUID, entitlementIDs []uuid.UUID,
) error {
	_, err := r.q.InsertPolicyEntitlements(ctx, db.InsertPolicyEntitlementsParams{
		JoinID: joinID, AccountID: accountID, PolicyID: policyID, EntitlementIds: entitlementIDs,
	})
	if err != nil {
		return mapJoinWrite(err)
	}

	return nil
}

// ListPolicyEntitlements answers a policy's join rows (attach order).
func (r EntitlementsRepo) ListPolicyEntitlements(
	ctx context.Context, accountID, policyID uuid.UUID,
) ([]domain.PolicyEntitlement, error) {
	rows, err := r.q.ListPolicyEntitlements(ctx, db.ListPolicyEntitlementsParams{
		AccountID: accountID, PolicyID: policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: policy %s entitlements: %w", policyID, err)
	}

	out := make([]domain.PolicyEntitlement, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.PolicyEntitlement{
			ID: row.ID, AccountID: row.AccountID, PolicyID: row.PolicyID,
			EntitlementID: row.EntitlementID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}

	return out, nil
}

// PolicyEntitlement loads one join row.
func (r EntitlementsRepo) PolicyEntitlement(
	ctx context.Context, accountID, policyID, entitlementID uuid.UUID,
) (domain.PolicyEntitlement, error) {
	row, err := r.q.GetPolicyEntitlement(ctx, db.GetPolicyEntitlementParams{
		AccountID: accountID, PolicyID: policyID, EntitlementID: entitlementID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PolicyEntitlement{}, fmt.Errorf("postgres: policy entitlement: %w", entitlements.ErrNotFound)
	}

	if err != nil {
		return domain.PolicyEntitlement{}, fmt.Errorf("postgres: policy entitlement: %w", err)
	}

	return domain.PolicyEntitlement{
		ID: row.ID, AccountID: row.AccountID, PolicyID: row.PolicyID,
		EntitlementID: row.EntitlementID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

// DeletePolicyEntitlements removes the join rows of one detach call;
// the answer is the entitlement ids that were actually attached.
func (r EntitlementsRepo) DeletePolicyEntitlements(
	ctx context.Context, accountID, policyID uuid.UUID, entitlementIDs []uuid.UUID,
) ([]uuid.UUID, error) {
	attached := map[uuid.UUID]struct{}{}
	for _, id := range entitlementIDs { //nolint:wsl_v5 // dense by design
		if _, err := r.q.GetPolicyEntitlement(ctx, db.GetPolicyEntitlementParams{
			AccountID: accountID, PolicyID: policyID, EntitlementID: id,
		}); err == nil {
			attached[id] = struct{}{}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: policy entitlement: %w", err)
		}
	}

	if len(attached) == 0 {
		return nil, nil
	}

	ids := make([]uuid.UUID, 0, len(attached))
	for _, id := range entitlementIDs {
		if _, ok := attached[id]; ok {
			ids = append(ids, id)
		}
	}

	if _, err := r.q.DeletePolicyEntitlements(ctx, db.DeletePolicyEntitlementsParams{
		AccountID: accountID, PolicyID: policyID, EntitlementIds: ids,
	}); err != nil {
		return nil, fmt.Errorf("postgres: delete policy entitlements: %w", err)
	}

	return ids, nil
}

func entitlementList[T db.ListEntitlementsDescRow | db.ListEntitlementsAscRow |
	db.ListEntitlementsByIdsDescRow | db.ListEntitlementsByIdsAscRow](
	rows []T, conv func(T) db.GetEntitlementByIDRow, err error,
) ([]domain.Entitlement, error) {
	if err != nil {
		return nil, fmt.Errorf("postgres: list entitlements: %w", err)
	}

	out := make([]domain.Entitlement, 0, len(rows))
	for _, row := range rows {
		e, err := entitlementDomain(conv(row), "", nil)
		if err != nil {
			return nil, err
		}

		out = append(out, e)
	}

	return out, nil
}

func entitlementDomain(
	row db.GetEntitlementByIDRow, what string, err error,
) (domain.Entitlement, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Entitlement{}, fmt.Errorf("postgres: entitlement %s: %w", what, entitlements.ErrNotFound)
	}

	if err != nil {
		return domain.Entitlement{}, fmt.Errorf("postgres: entitlement %s: %w", what, err)
	}

	meta := map[string]any{}
	_ = json.Unmarshal(row.Metadata, &meta)

	return domain.Entitlement{
		ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
		Metadata: meta, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func mapEntitlementWrite(id uuid.UUID, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: write entitlement %s: %w", id, entitlements.ErrCodeTaken)
	}

	return fmt.Errorf("postgres: write entitlement %s: %w", id, err)
}

func mapJoinWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: policy entitlement: %w", policies.ErrEntitlementTaken)
	}

	return fmt.Errorf("postgres: policy entitlement: %w", err)
}
