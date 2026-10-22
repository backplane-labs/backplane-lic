package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// PoliciesRepo adapts the policy queries to the policies scenario's
// storage port.
type PoliciesRepo struct {
	q *db.Queries
}

// PolicyByID loads a policy by id.
func (r PoliciesRepo) PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error) {
	row, err := r.q.GetPolicyByID(ctx, db.GetPolicyByIDParams{AccountID: accountID, ID: id})

	return policyDomain(policyRowFromGet(row), err)
}

// PolicyByLicense loads the license bearer's own policy.
func (r PoliciesRepo) PolicyByLicense(ctx context.Context, accountID, licenseID uuid.UUID) (domain.Policy, error) {
	row, err := r.q.GetPolicyByLicense(ctx, db.GetPolicyByLicenseParams{AccountID: accountID, LicenseID: licenseID})

	return policyDomain(policyRowFromGetLicense(row), err)
}

// InsertPolicy writes a new policy row.
func (r PoliciesRepo) InsertPolicy(ctx context.Context, p domain.Policy) error {
	return r.q.InsertPolicy(ctx, policyInsertParams(p)) //nolint:wrapcheck // the write's own error
}

// UpdatePolicy overwrites the mutable columns.
func (r PoliciesRepo) UpdatePolicy(ctx context.Context, p domain.Policy) error {
	return r.q.UpdatePolicy(ctx, policyUpdateParams(p)) //nolint:wrapcheck // the write's own error
}

// DeletePolicy destroys the policy; false when it was gone already.
func (r PoliciesRepo) DeletePolicy(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	n, err := r.q.DeletePolicy(ctx, db.DeletePolicyParams{AccountID: accountID, ID: id})
	if err != nil {
		return false, fmt.Errorf("postgres: delete policy %s: %w", id, err)
	}

	return n > 0, nil
}

// ListPolicies pages the admin-side scope (user and license bearers
// load the same page and fail the post-check); filterProductID narrows
// by product when set.
func (r PoliciesRepo) ListPolicies(
	ctx context.Context, accountID uuid.UUID, filterProductID *uuid.UUID, page Page,
) ([]domain.Policy, error) {
	args := db.ListPoliciesDescParams{
		AccountID: accountID, FilterProductID: filterProductID,
		CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	}

	if page.Asc {
		rows, err := r.q.ListPoliciesAsc(ctx, policyArgsAsc(args))

		return policyList(rows, policyRowFromListAsc, err)
	}

	rows, err := r.q.ListPoliciesDesc(ctx, args)

	return policyList(rows, policyRowFromListDesc, err)
}

// CountPolicies counts the admin-side scope.
func (r PoliciesRepo) CountPolicies(ctx context.Context, accountID uuid.UUID, filterProductID *uuid.UUID) (int, error) {
	row, err := r.q.CountPolicies(ctx, db.CountPoliciesParams{AccountID: accountID, FilterProductID: filterProductID})
	if err != nil {
		return 0, fmt.Errorf("postgres: count policies: %w", err)
	}

	return int(row.Count), nil
}

// ListProductPolicies pages the product-bearer scope.
func (r PoliciesRepo) ListProductPolicies(
	ctx context.Context, accountID, productID uuid.UUID, page Page,
) ([]domain.Policy, error) {
	args := db.ListPoliciesForProductDescParams{
		AccountID: accountID, BearerProductID: productID,
		CursorID: page.Cursor, Limit: page.Fetch, Offset: page.Offset,
	}

	if page.Asc {
		rows, err := r.q.ListPoliciesForProductAsc(ctx, policyForProductArgsAsc(args))

		return policyList(rows, policyRowFromForProductAsc, err)
	}

	rows, err := r.q.ListPoliciesForProductDesc(ctx, args)

	return policyList(rows, policyRowFromForProductDesc, err)
}

// CountProductPolicies counts the product-bearer scope.
func (r PoliciesRepo) CountProductPolicies(ctx context.Context, accountID, productID uuid.UUID) (int, error) {
	row, err := r.q.CountPoliciesForProduct(ctx, db.CountPoliciesForProductParams{
		AccountID: accountID, BearerProductID: productID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count product policies: %w", err)
	}

	return int(row.Count), nil
}

// UserPolicyIDs answers the policies of the user's licenses.
func (r PoliciesRepo) UserPolicyIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.UserPolicyIds(ctx, db.UserPolicyIdsParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: user %s policies: %w", userID, err)
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	return ids, nil
}

// -- the column-plumbing conversions (every policy query shares one
// column set; ogen gives each its own row struct).

func policyRowFromGet(row db.GetPolicyByIDRow) db.GetPolicyByIDRow { return row }

func policyRowFromGetLicense(row db.GetPolicyByLicenseRow) db.GetPolicyByIDRow {
	return db.GetPolicyByIDRow(row)
}

func policyRowFromListDesc(row db.ListPoliciesDescRow) db.GetPolicyByIDRow {
	return db.GetPolicyByIDRow(row)
}

func policyRowFromListAsc(row db.ListPoliciesAscRow) db.GetPolicyByIDRow {
	return db.GetPolicyByIDRow(row)
}

func policyRowFromForProductDesc(row db.ListPoliciesForProductDescRow) db.GetPolicyByIDRow {
	return db.GetPolicyByIDRow(row)
}

func policyRowFromForProductAsc(row db.ListPoliciesForProductAscRow) db.GetPolicyByIDRow {
	return db.GetPolicyByIDRow(row)
}

func policyArgsAsc(args db.ListPoliciesDescParams) db.ListPoliciesAscParams {
	return db.ListPoliciesAscParams{ //nolint:staticcheck // mirrors the generated row types
		AccountID: args.AccountID, FilterProductID: args.FilterProductID,
		CursorID: args.CursorID, Limit: args.Limit, Offset: args.Offset,
	}
}

func policyForProductArgsAsc(args db.ListPoliciesForProductDescParams) db.ListPoliciesForProductAscParams {
	return db.ListPoliciesForProductAscParams{ //nolint:staticcheck // mirrors the generated row types
		AccountID: args.AccountID, BearerProductID: args.BearerProductID,
		CursorID: args.CursorID, Limit: args.Limit, Offset: args.Offset,
	}
}

func policyList[T any](rows []T, conv func(T) db.GetPolicyByIDRow, err error) ([]domain.Policy, error) {
	if err != nil {
		return nil, fmt.Errorf("postgres: list policies: %w", err)
	}

	out := make([]domain.Policy, 0, len(rows))
	for _, row := range rows {
		p, err := policyDomain(conv(row), nil)
		if err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, nil
}

func policyDomain(row db.GetPolicyByIDRow, err error) (domain.Policy, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Policy{}, fmt.Errorf("postgres: policy: %w", policies.ErrNotFound)
	}

	if err != nil {
		return domain.Policy{}, fmt.Errorf("postgres: policy: %w", err)
	}

	meta := map[string]any{}
	_ = json.Unmarshal(row.Metadata, &meta)

	return domain.Policy{ //nolint:dupl // the license repository mirrors these columns
		ID:                            row.ID,
		AccountID:                     row.AccountID,
		ProductID:                     row.ProductID,
		Name:                          row.Name,
		Duration:                      row.Duration,
		Strict:                        row.Strict,
		Floating:                      row.Floating,
		UsePool:                       row.UsePool,
		MaxMachines:                   row.MaxMachines,
		MaxProcesses:                  row.MaxProcesses,
		MaxUsers:                      row.MaxUsers,
		MaxCores:                      row.MaxCores,
		MaxMemory:                     row.MaxMemory,
		MaxDisk:                       row.MaxDisk,
		MaxUses:                       row.MaxUses,
		MachineUniquenessStrategy:     deref(row.MachineUniquenessStrategy),
		MachineMatchingStrategy:       deref(row.MachineMatchingStrategy),
		ComponentUniquenessStrategy:   deref(row.ComponentUniquenessStrategy),
		ComponentMatchingStrategy:     deref(row.ComponentMatchingStrategy),
		ExpirationStrategy:            deref(row.ExpirationStrategy),
		ExpirationBasis:               deref(row.ExpirationBasis),
		RenewalBasis:                  deref(row.RenewalBasis),
		TransferStrategy:              deref(row.TransferStrategy),
		AuthenticationStrategy:        deref(row.AuthenticationStrategy),
		MachineLeasingStrategy:        deref(row.MachineLeasingStrategy),
		ProcessLeasingStrategy:        deref(row.ProcessLeasingStrategy),
		OverageStrategy:               deref(row.OverageStrategy),
		Scheme:                        deref(row.Scheme),
		Encrypted:                     row.Encrypted,
		Protected:                     row.Protected,
		RequireProductScope:           row.RequireProductScope,
		RequirePolicyScope:            row.RequirePolicyScope,
		RequireMachineScope:           row.RequireMachineScope,
		RequireFingerprintScope:       row.RequireFingerprintScope,
		RequireComponentsScope:        row.RequireComponentsScope,
		RequireUserScope:              row.RequireUserScope,
		RequireChecksumScope:          row.RequireChecksumScope,
		RequireVersionScope:           row.RequireVersionScope,
		RequireCheckIn:                row.RequireCheckIn,
		CheckInInterval:               deref(row.CheckInInterval),
		CheckInIntervalCount:          row.CheckInIntervalCount,
		HeartbeatDuration:             row.HeartbeatDuration,
		HeartbeatCullStrategy:         deref(row.HeartbeatCullStrategy),
		HeartbeatResurrectionStrategy: deref(row.HeartbeatResurrectionStrategy),
		HeartbeatBasis:                deref(row.HeartbeatBasis),
		RequireHeartbeat:              row.RequireHeartbeat,
		Metadata:                      meta,
		CreatedAt:                     row.CreatedAt,
		UpdatedAt:                     row.UpdatedAt,
	}, nil
}

func policyInsertParams(p domain.Policy) db.InsertPolicyParams {
	return db.InsertPolicyParams{
		ID: p.ID, AccountID: p.AccountID, ProductID: p.ProductID, Name: p.Name,
		Duration: p.Duration, Strict: p.Strict, Floating: p.Floating, UsePool: p.UsePool,
		MaxMachines: p.MaxMachines, MaxProcesses: p.MaxProcesses, MaxUsers: p.MaxUsers,
		MaxCores: p.MaxCores, MaxMemory: p.MaxMemory, MaxDisk: p.MaxDisk, MaxUses: p.MaxUses,
		MachineUniquenessStrategy:     ref(p.MachineUniquenessStrategy),
		MachineMatchingStrategy:       ref(p.MachineMatchingStrategy),
		ComponentUniquenessStrategy:   ref(p.ComponentUniquenessStrategy),
		ComponentMatchingStrategy:     ref(p.ComponentMatchingStrategy),
		ExpirationStrategy:            ref(p.ExpirationStrategy),
		ExpirationBasis:               ref(p.ExpirationBasis),
		RenewalBasis:                  ref(p.RenewalBasis),
		TransferStrategy:              ref(p.TransferStrategy),
		AuthenticationStrategy:        ref(p.AuthenticationStrategy),
		MachineLeasingStrategy:        ref(p.MachineLeasingStrategy),
		ProcessLeasingStrategy:        ref(p.ProcessLeasingStrategy),
		OverageStrategy:               ref(p.OverageStrategy),
		Scheme:                        ref(p.Scheme),
		Encrypted:                     p.Encrypted,
		Protected:                     p.Protected,
		RequireProductScope:           p.RequireProductScope,
		RequirePolicyScope:            p.RequirePolicyScope,
		RequireMachineScope:           p.RequireMachineScope,
		RequireFingerprintScope:       p.RequireFingerprintScope,
		RequireComponentsScope:        p.RequireComponentsScope,
		RequireUserScope:              p.RequireUserScope,
		RequireChecksumScope:          p.RequireChecksumScope,
		RequireVersionScope:           p.RequireVersionScope,
		RequireCheckIn:                p.RequireCheckIn,
		CheckInInterval:               ref(p.CheckInInterval),
		CheckInIntervalCount:          p.CheckInIntervalCount,
		HeartbeatDuration:             p.HeartbeatDuration,
		HeartbeatCullStrategy:         ref(p.HeartbeatCullStrategy),
		HeartbeatResurrectionStrategy: ref(p.HeartbeatResurrectionStrategy),
		HeartbeatBasis:                ref(p.HeartbeatBasis),
		RequireHeartbeat:              p.RequireHeartbeat,
		Metadata:                      jsonMap(p.Metadata),
	}
}

func policyUpdateParams(p domain.Policy) db.UpdatePolicyParams {
	return db.UpdatePolicyParams{
		Name: p.Name, Duration: p.Duration, Strict: p.Strict, Floating: p.Floating,
		MaxMachines: p.MaxMachines, MaxProcesses: p.MaxProcesses, MaxUsers: p.MaxUsers,
		MaxCores: p.MaxCores, MaxMemory: p.MaxMemory, MaxDisk: p.MaxDisk, MaxUses: p.MaxUses,
		MachineUniquenessStrategy:     ref(p.MachineUniquenessStrategy),
		MachineMatchingStrategy:       ref(p.MachineMatchingStrategy),
		ComponentUniquenessStrategy:   ref(p.ComponentUniquenessStrategy),
		ComponentMatchingStrategy:     ref(p.ComponentMatchingStrategy),
		ExpirationStrategy:            ref(p.ExpirationStrategy),
		ExpirationBasis:               ref(p.ExpirationBasis),
		RenewalBasis:                  ref(p.RenewalBasis),
		TransferStrategy:              ref(p.TransferStrategy),
		AuthenticationStrategy:        ref(p.AuthenticationStrategy),
		MachineLeasingStrategy:        ref(p.MachineLeasingStrategy),
		ProcessLeasingStrategy:        ref(p.ProcessLeasingStrategy),
		OverageStrategy:               ref(p.OverageStrategy),
		Protected:                     p.Protected,
		RequireProductScope:           p.RequireProductScope,
		RequirePolicyScope:            p.RequirePolicyScope,
		RequireMachineScope:           p.RequireMachineScope,
		RequireFingerprintScope:       p.RequireFingerprintScope,
		RequireComponentsScope:        p.RequireComponentsScope,
		RequireUserScope:              p.RequireUserScope,
		RequireChecksumScope:          p.RequireChecksumScope,
		RequireVersionScope:           p.RequireVersionScope,
		RequireCheckIn:                p.RequireCheckIn,
		CheckInInterval:               ref(p.CheckInInterval),
		CheckInIntervalCount:          p.CheckInIntervalCount,
		HeartbeatDuration:             p.HeartbeatDuration,
		HeartbeatCullStrategy:         ref(p.HeartbeatCullStrategy),
		HeartbeatResurrectionStrategy: ref(p.HeartbeatResurrectionStrategy),
		HeartbeatBasis:                ref(p.HeartbeatBasis),
		RequireHeartbeat:              p.RequireHeartbeat,
		Metadata:                      jsonMap(p.Metadata),
		AccountID:                     p.AccountID,
		ID:                            p.ID,
	}
}
