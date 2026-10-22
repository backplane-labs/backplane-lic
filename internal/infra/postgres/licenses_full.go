// The full licenses repository: rows with their policy and owner
// context, the filtered index, the relationship rows and the actions'
// writes (internal/app/licenses).
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// ErrLicenseNotFound is the licenses port's not-found sentinel.
var errLicenseNotFound = licenses.ErrNotFound

// machineLimit caps the validation's machine scan (a license's
// machines are bounded well below this).
const machineLimit = 100000

// LicenseByID loads the license (with policy and owner context) by id.
func (r LicensesRepo) LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.License, error) {
	row, err := r.q.GetLicenseByID(ctx, db.GetLicenseByIDParams{AccountID: accountID, ID: id})

	return licenseDomain(row, err)
}

// LicenseByKey loads the license by raw key.
func (r LicensesRepo) LicenseByKey(ctx context.Context, accountID uuid.UUID, key string) (domain.License, error) {
	row, err := r.q.GetLicenseByKey(ctx, db.GetLicenseByKeyParams{AccountID: accountID, Key: key})

	return licenseDomain(db.GetLicenseByIDRow(row), err)
}

// InsertLicense writes the new row.
func (r LicensesRepo) InsertLicense(ctx context.Context, l domain.License) error {
	return r.q.InsertLicense( //nolint:wrapcheck // the write's own error
		ctx, db.InsertLicenseParams{
			ID:                   l.ID,
			AccountID:            l.AccountID,
			ProductID:            l.ProductID,
			PolicyID:             l.PolicyID,
			UserID:               nullUUIDPtr(l.OwnerID),
			GroupID:              nullUUIDPtr(l.GroupID),
			Name:                 l.Name,
			Key:                  l.Key,
			Expiry:               l.Expiry,
			Uses:                 l.Uses,
			Suspended:            l.Suspended,
			Protected:            l.Protected,
			Metadata:             jsonMap(l.Metadata),
			MaxMachinesOverride:  l.MaxMachinesOverride,
			MaxProcessesOverride: l.MaxProcessesOverride,
			MaxUsersOverride:     l.MaxUsersOverride,
			MaxCoresOverride:     l.MaxCoresOverride,
			MaxMemoryOverride:    l.MaxMemoryOverride,
			MaxDiskOverride:      l.MaxDiskOverride,
			MaxUsesOverride:      l.MaxUsesOverride,
			LastCheckInAt:        l.LastCheckInAt,
		})
}

// UpdateLicense writes every mutable column.
func (r LicensesRepo) UpdateLicense(ctx context.Context, l domain.License) error {
	return r.q.UpdateLicense( //nolint:wrapcheck // the write's own error
		ctx, db.UpdateLicenseParams{
			AccountID:            l.AccountID,
			ID:                   l.ID,
			Name:                 l.Name,
			Expiry:               l.Expiry,
			Uses:                 l.Uses,
			Suspended:            l.Suspended,
			Protected:            l.Protected,
			Metadata:             jsonMap(l.Metadata),
			MaxMachinesOverride:  l.MaxMachinesOverride,
			MaxProcessesOverride: l.MaxProcessesOverride,
			MaxUsersOverride:     l.MaxUsersOverride,
			MaxCoresOverride:     l.MaxCoresOverride,
			MaxMemoryOverride:    l.MaxMemoryOverride,
			MaxDiskOverride:      l.MaxDiskOverride,
			MaxUsesOverride:      l.MaxUsesOverride,
			UpdatedAt:            l.UpdatedAt,
		})
}

// DeleteLicense destroys the license and its tokens.
func (r LicensesRepo) DeleteLicense(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteTokensOfLicense(ctx, db.DeleteTokensOfLicenseParams{
		AccountID: accountID, BearerID: &id,
	}); err != nil {
		return fmt.Errorf("postgres: delete license tokens: %w", err)
	}

	if _, err := r.q.DeleteLicense(ctx, db.DeleteLicenseParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: delete license: %w", err)
	}

	return nil
}

// ListLicenses pages the filtered scope.
func (r LicensesRepo) ListLicenses(
	ctx context.Context, accountID uuid.UUID, f licenses.Filter, scopeKind string,
	scopeID uuid.UUID, page Page, now time.Time,
) ([]domain.License, error) {
	args := listLicensesArgs(accountID, f, scopeKind, scopeID, page, now)

	if page.Asc {
		ascRows, ascErr := r.q.ListLicensesAsc(ctx, listLicensesArgsAsc(args))

		return licenseList(ascRows, func(row db.ListLicensesAscRow) db.GetLicenseByIDRow {
			return db.GetLicenseByIDRow(row)
		}, ascErr)
	}

	rows, err := r.q.ListLicenses(ctx, args)

	return licenseList(rows, func(row db.ListLicensesRow) db.GetLicenseByIDRow {
		return db.GetLicenseByIDRow(row)
	}, err)
}

// CountLicenses counts the filtered scope.
func (r LicensesRepo) CountLicenses(
	ctx context.Context, accountID uuid.UUID, f licenses.Filter, scopeKind string,
	scopeID uuid.UUID, now time.Time,
) (int, error) {
	args := listLicensesArgs(accountID, f, scopeKind, scopeID, pageNone(), now)

	row, err := r.q.CountLicenses(ctx, countLicensesArgs(args))
	if err != nil {
		return 0, fmt.Errorf("postgres: count licenses: %w", err)
	}

	return int(row.Count), nil
}

// LicenseKeyTaken answers whether some license already holds the key.
func (r LicensesRepo) LicenseKeyTaken(ctx context.Context, accountID uuid.UUID, key string) (bool, error) {
	_, err := r.q.LicenseKeyTaken(ctx, db.LicenseKeyTakenParams{AccountID: accountID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("postgres: license key taken: %w", err)
	}

	return true, nil
}

// LicenseIDExists answers whether a license with the id exists.
func (r LicensesRepo) LicenseIDExists(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	_, err := r.q.LicenseIDExists(ctx, db.LicenseIDExistsParams{AccountID: accountID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("postgres: license id exists: %w", err)
	}

	return true, nil
}

// SetLicenseExpiry writes the expiry.
func (r LicensesRepo) SetLicenseExpiry(
	ctx context.Context, accountID, id uuid.UUID, expiry *time.Time, now time.Time,
) error {
	return r.q.SetLicenseExpiry( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseExpiryParams{
			AccountID: accountID, ID: id, Expiry: expiry, UpdatedAt: now,
		})
}

// LicenseUsesForUpdate locks the counter without waiting, like Keygen's
// usage actions. The caller owns the pgtx boundary.
func (r LicensesRepo) LicenseUsesForUpdate(ctx context.Context, accountID, id uuid.UUID) (int32, error) {
	uses, err := r.q.LicenseUsesForUpdate(ctx, db.LicenseUsesForUpdateParams{AccountID: accountID, ID: id})

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return 0, fmt.Errorf("postgres: usage row lock: %w", licenses.ErrUsageConflict)
	}

	if err != nil {
		return 0, fmt.Errorf("postgres: usage row lock: %w", err)
	}

	return uses.Uses, nil
}

// SetLicenseUses writes the usage counter.
func (r LicensesRepo) SetLicenseUses(ctx context.Context, accountID, id uuid.UUID, uses int32, now time.Time) error {
	return r.q.SetLicenseUses( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseUsesParams{
			AccountID: accountID, ID: id, Uses: uses, UpdatedAt: now,
		})
}

// SetLicenseSuspended writes the suspension flag.
func (r LicensesRepo) SetLicenseSuspended(
	ctx context.Context, accountID, id uuid.UUID, suspended bool, now time.Time,
) error {
	return r.q.SetLicenseSuspended( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseSuspendedParams{
			AccountID: accountID, ID: id, Suspended: suspended, UpdatedAt: now,
		})
}

// TouchLicenseValidated writes the validation touches.
func (r LicensesRepo) TouchLicenseValidated(
	ctx context.Context, accountID, id uuid.UUID, at time.Time, checksum, version *string,
) error {
	return r.q.TouchLicenseValidated( //nolint:wrapcheck // the write's own error
		ctx, db.TouchLicenseValidatedParams{
			AccountID: accountID, ID: id, LastValidatedAt: at,
			LastValidatedChecksum: checksum, LastValidatedVersion: version,
		})
}

// SetLicenseLastCheckIn writes the check-in timestamp.
func (r LicensesRepo) SetLicenseLastCheckIn(
	ctx context.Context, accountID, id uuid.UUID, at, now time.Time,
) error {
	return r.q.SetLicenseLastCheckIn( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseLastCheckInParams{
			AccountID: accountID, ID: id, LastCheckInAt: &at, UpdatedAt: now,
		})
}

// SetLicenseLastCheckOut writes the check-out timestamp.
func (r LicensesRepo) SetLicenseLastCheckOut(
	ctx context.Context, accountID, id uuid.UUID, at, now time.Time,
) error {
	return r.q.SetLicenseLastCheckOut( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseLastCheckOutParams{
			AccountID: accountID, ID: id, LastCheckOutAt: &at, UpdatedAt: now,
		})
}

// TransferLicense moves the license to another policy.
func (r LicensesRepo) TransferLicense(
	ctx context.Context, accountID, id, policyID, productID uuid.UUID, expiry *time.Time, now time.Time,
) error {
	return r.q.TransferLicense( //nolint:wrapcheck // the write's own error
		ctx, db.TransferLicenseParams{
			AccountID: accountID, ID: id, PolicyID: policyID, ProductID: productID,
			Expiry: expiry, UpdatedAt: now,
		})
}

// SetLicenseOwner writes the owner (and the inherited group).
func (r LicensesRepo) SetLicenseOwner(
	ctx context.Context, accountID, id uuid.UUID, owner uuid.NullUUID, group uuid.NullUUID, now time.Time,
) error {
	return r.q.SetLicenseOwner( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseOwnerParams{
			AccountID: accountID, ID: id, UserID: nullUUIDPtr(owner), GroupID: nullUUIDPtr(group),
			UpdatedAt: now,
		})
}

// SetLicenseGroup writes the group.
func (r LicensesRepo) SetLicenseGroup(
	ctx context.Context, accountID, id uuid.UUID, group uuid.NullUUID, now time.Time,
) error {
	return r.q.SetLicenseGroup( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseGroupParams{
			AccountID: accountID, ID: id, GroupID: nullUUIDPtr(group), UpdatedAt: now,
		})
}

// CountLicensesOfGroup answers the group's license count.
func (r LicensesRepo) CountLicensesOfGroup(ctx context.Context, accountID, groupID uuid.UUID) (int, error) {
	row, err := r.q.CountLicensesOfGroup(ctx, db.CountLicensesOfGroupParams{
		AccountID: accountID, GroupID: &groupID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count group licenses: %w", err)
	}

	return int(row.Count), nil
}

// InsertLicenseUsers attaches licensees.
func (r LicensesRepo) InsertLicenseUsers(
	ctx context.Context, joinID, accountID, licenseID uuid.UUID, userIDs []uuid.UUID,
) error {
	return r.q.InsertLicenseUsers( //nolint:wrapcheck // the write's own error
		ctx, db.InsertLicenseUsersParams{
			ID: joinID, AccountID: accountID, LicenseID: licenseID, UserIds: userIDs,
		})
}

// DeleteLicenseUsers detaches licensees, answering how many went.
func (r LicensesRepo) DeleteLicenseUsers(
	ctx context.Context, accountID, licenseID uuid.UUID, userIDs []uuid.UUID,
) (int, error) {
	n, err := r.q.DeleteLicenseUsers(ctx, db.DeleteLicenseUsersParams{
		AccountID: accountID, LicenseID: licenseID, UserIds: userIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: delete license users: %w", err)
	}

	return int(n), nil
}

// CountLicenseUsersOf answers how many of the users are attached.
func (r LicensesRepo) CountLicenseUsersOf(
	ctx context.Context, accountID, licenseID uuid.UUID, userIDs []uuid.UUID,
) (int, error) {
	row, err := r.q.CountLicenseUsersOf(ctx, db.CountLicenseUsersOfParams{
		AccountID: accountID, LicenseID: licenseID, UserIds: userIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count license users: %w", err)
	}

	return int(row.Count), nil
}

// SetLicenseUsersCount writes the denormalized licensee count.
func (r LicensesRepo) SetLicenseUsersCount(ctx context.Context, accountID, licenseID uuid.UUID, count int32) error {
	return r.q.SetLicenseUsersCount( //nolint:wrapcheck // the write's own error
		ctx, db.SetLicenseUsersCountParams{
			AccountID: accountID, ID: licenseID, Count: count,
		})
}

// ListLicenseUserRows answers the join rows of the attached users.
func (r LicensesRepo) ListLicenseUserRows(
	ctx context.Context, accountID, licenseID uuid.UUID, userIDs []uuid.UUID,
) ([]domain.LicenseUser, error) {
	rows, err := r.q.ListLicenseUserRows(ctx, db.ListLicenseUserRowsParams{
		AccountID: accountID, LicenseID: licenseID, UserIds: userIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list license users: %w", err)
	}

	out := make([]domain.LicenseUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.LicenseUser{
			ID: row.ID, AccountID: row.AccountID, LicenseID: row.LicenseID,
			UserID: row.UserID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}

	return out, nil
}

// LicenseeIsUser answers whether the user is a licensee.
func (r LicensesRepo) LicenseeIsUser(ctx context.Context, accountID, licenseID, userID uuid.UUID) (bool, error) {
	_, err := r.q.LicenseeIsUser(ctx, db.LicenseeIsUserParams{
		AccountID: accountID, LicenseID: licenseID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("postgres: licensee is user: %w", err)
	}

	return true, nil
}

// InsertLicenseEntitlements attaches entitlements.
func (r LicensesRepo) InsertLicenseEntitlements(
	ctx context.Context, joinID, accountID, licenseID uuid.UUID, entitlementIDs []uuid.UUID,
) error {
	return r.q.InsertLicenseEntitlements( //nolint:wrapcheck // the write's own error
		ctx, db.InsertLicenseEntitlementsParams{
			ID: joinID, AccountID: accountID, LicenseID: licenseID, EntitlementIds: entitlementIDs,
		})
}

// DeleteLicenseEntitlements detaches entitlements, answering how many.
func (r LicensesRepo) DeleteLicenseEntitlements(
	ctx context.Context, accountID, licenseID uuid.UUID, entitlementIDs []uuid.UUID,
) (int, error) {
	n, err := r.q.DeleteLicenseEntitlements(ctx, db.DeleteLicenseEntitlementsParams{
		AccountID: accountID, LicenseID: licenseID, EntitlementIds: entitlementIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: delete license entitlements: %w", err)
	}

	return int(n), nil
}

// CountLicenseEntitlementsOf answers how many of the entitlements are
// attached to the license.
func (r LicensesRepo) CountLicenseEntitlementsOf(
	ctx context.Context, accountID, licenseID uuid.UUID, entitlementIDs []uuid.UUID,
) (int, error) {
	row, err := r.q.CountLicenseEntitlementsOf(ctx, db.CountLicenseEntitlementsOfParams{
		AccountID: accountID, LicenseID: licenseID, EntitlementIds: entitlementIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count license entitlements: %w", err)
	}

	return int(row.Count), nil
}

// ListLicenseEntitlementRows answers the join rows of the attached
// entitlements.
func (r LicensesRepo) ListLicenseEntitlementRows(
	ctx context.Context, accountID, licenseID uuid.UUID, entitlementIDs []uuid.UUID,
) ([]domain.LicenseEntitlement, error) {
	rows, err := r.q.ListLicenseEntitlementRows(ctx, db.ListLicenseEntitlementRowsParams{
		AccountID: accountID, LicenseID: licenseID, EntitlementIds: entitlementIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list license entitlements: %w", err)
	}

	out := make([]domain.LicenseEntitlement, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.LicenseEntitlement{
			ID: row.ID, AccountID: row.AccountID, LicenseID: row.LicenseID,
			EntitlementID: row.EntitlementID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}

	return out, nil
}

// CountPolicyEntitlementsOf answers how many of the entitlements the
// policy already attaches.
func (r LicensesRepo) CountPolicyEntitlementsOf(
	ctx context.Context, accountID, policyID uuid.UUID, entitlementIDs []uuid.UUID,
) (int, error) {
	row, err := r.q.CountPolicyEntitlementsOf(ctx, db.CountPolicyEntitlementsOfParams{
		AccountID: accountID, PolicyID: policyID, EntitlementIds: entitlementIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count policy entitlements: %w", err)
	}

	return int(row.Count), nil
}

// ListLicenseEntitlementCodes answers the union of the policy- and
// license-attached entitlement codes.
func (r LicensesRepo) ListLicenseEntitlementCodes(
	ctx context.Context, accountID, policyID, licenseID uuid.UUID,
) ([]string, error) {
	rows, err := r.q.ListLicenseEntitlementCodes(ctx, db.ListLicenseEntitlementCodesParams{
		AccountID: accountID, PolicyID: policyID, LicenseID: licenseID,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: license entitlement codes: %w", err)
	}

	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Code)
	}

	return out, nil
}

// LicenseUserMatches answers whether the user (by id or email) is the
// owner or a licensee of the license.
func (r LicensesRepo) LicenseUserMatches(
	ctx context.Context, accountID, licenseID, userID uuid.UUID, email *string,
) (bool, error) {
	_, err := r.q.LicenseUserMatches(ctx, db.LicenseUserMatchesParams{
		AccountID: accountID, LicenseID: licenseID, UserID: userID, Email: email,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("postgres: license user matches: %w", err)
	}

	return true, nil
}

// LicenseMachines answers every machine of the license with its
// heartbeat status (the index and the validation share the rows).
func (r LicensesRepo) LicenseMachines(
	ctx context.Context, accountID, licenseID uuid.UUID, f licenses.MachineFilter, now time.Time,
) ([]domain.Machine, error) {
	var fingerprint *string
	if f.Fingerprint != "" {
		fingerprint = &f.Fingerprint
	}

	var productID, userID *uuid.UUID
	if f.ProductID.Valid {
		productID = &f.ProductID.UUID
	}

	if f.UserID.Valid {
		userID = &f.UserID.UUID
	}

	rows, err := r.q.ListLicenseMachines(ctx, db.ListLicenseMachinesParams{
		AccountID: accountID, LicenseID: licenseID, Fingerprint: fingerprint,
		ProductID: productID, UserID: userID,
		Now: now, HeartbeatTtl: ttlPtr(domain.HeartbeatTTLSeconds), Limit: machineLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: license machines: %w", err)
	}

	out := make([]domain.Machine, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, machineDomain(row.ID, row.AccountID, row.LicenseID, row.PolicyID,
			row.OwnerID, row.GroupID, row.Name, row.Fingerprint, row.IP, row.Hostname,
			row.Platform, row.Cores, row.Memory, row.Disk, row.MaxProcessesOverride,
			row.Metadata, row.LastHeartbeatAt, row.LastCheckOutAt, row.HeartbeatStatus,
			row.RequireHeartbeat, row.HeartbeatDuration, row.CreatedAt, row.UpdatedAt))
	}

	return out, nil
}

// LicenseMachineByID loads one of the license's machines by id.
func (r LicensesRepo) LicenseMachineByID(
	ctx context.Context, accountID, licenseID, id uuid.UUID, now time.Time,
) (domain.Machine, error) {
	row, err := r.q.GetLicenseMachineByID(ctx, db.GetLicenseMachineByIDParams{
		AccountID: accountID, LicenseID: licenseID, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Machine{}, fmt.Errorf("postgres: machine: %w", errLicenseNotFound)
	}

	if err != nil {
		return domain.Machine{}, fmt.Errorf("postgres: license machine: %w", err)
	}

	return machineDomain(row.ID, row.AccountID, row.LicenseID, row.PolicyID, row.OwnerID,
		row.GroupID, row.Name, row.Fingerprint, row.IP, row.Hostname, row.Platform,
		row.Cores, row.Memory, row.Disk, row.MaxProcessesOverride, row.Metadata,
		row.LastHeartbeatAt, row.LastCheckOutAt,
		machineHeartbeatStatus(now, row.CreatedAt, row.LastHeartbeatAt, row.HeartbeatDuration),
		row.RequireHeartbeat, row.HeartbeatDuration, row.CreatedAt, row.UpdatedAt), nil
}

// LicenseMachineByFingerprint loads one of the license's machines by
// fingerprint.
func (r LicensesRepo) LicenseMachineByFingerprint(
	ctx context.Context, accountID, licenseID uuid.UUID, fingerprint string, now time.Time,
) (domain.Machine, error) {
	row, err := r.q.GetLicenseMachineByFingerprint(ctx, db.GetLicenseMachineByFingerprintParams{
		AccountID: accountID, LicenseID: licenseID, Fingerprint: fingerprint,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Machine{}, fmt.Errorf("postgres: machine: %w", errLicenseNotFound)
	}

	if err != nil {
		return domain.Machine{}, fmt.Errorf("postgres: license machine: %w", err)
	}

	return machineDomain(row.ID, row.AccountID, row.LicenseID, row.PolicyID, row.OwnerID,
		row.GroupID, row.Name, row.Fingerprint, row.IP, row.Hostname, row.Platform,
		row.Cores, row.Memory, row.Disk, row.MaxProcessesOverride, row.Metadata,
		row.LastHeartbeatAt, row.LastCheckOutAt,
		machineHeartbeatStatus(now, row.CreatedAt, row.LastHeartbeatAt, row.HeartbeatDuration),
		row.RequireHeartbeat, row.HeartbeatDuration, row.CreatedAt, row.UpdatedAt), nil
}

// SumLicenseMachinesForOwner answers the owner-scoped machine count and
// resource sums (the per-user lease math).
func (r LicensesRepo) SumLicenseMachinesForOwner(
	ctx context.Context, accountID, licenseID uuid.UUID, owner *uuid.UUID,
) (licenses.MachineSums, error) {
	row, err := r.q.SumLicenseMachinesForOwner(ctx, db.SumLicenseMachinesForOwnerParams{
		AccountID: accountID, LicenseID: licenseID, OwnerID: owner,
	})
	if err != nil {
		return licenses.MachineSums{}, fmt.Errorf("postgres: sum license machines: %w", err)
	}

	return licenses.MachineSums{
		Count: int(row.Count), Cores: int(row.Cores), Memory: row.Memory, Disk: row.Disk,
	}, nil
}

// CountMachineComponentsOf answers how many of the component
// fingerprints the machine holds.
func (r LicensesRepo) CountMachineComponentsOf(
	ctx context.Context, machineID uuid.UUID, fingerprints []string,
) (int, error) {
	row, err := r.q.CountMachineComponentsOf(ctx, db.CountMachineComponentsOfParams{
		MachineID: machineID, Fingerprints: fingerprints,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count machine components: %w", err)
	}

	return int(row.Count), nil
}

// CountLicenseEntitlementCodes answers how many of the codes the
// license is entitled to.
func (r LicensesRepo) CountLicenseEntitlementCodes(
	ctx context.Context, accountID, policyID, licenseID uuid.UUID, codes []string,
) (int, error) {
	row, err := r.q.CountLicenseEntitlementCodes(ctx, db.CountLicenseEntitlementCodesParams{
		AccountID: accountID, PolicyID: policyID, LicenseID: licenseID, Codes: codes,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count entitlement codes: %w", err)
	}

	return int(row.Count), nil
}

// CountLicenseProcesses answers the license's process count (the
// owner-scoped per-user lease when owner is set).
func (r LicensesRepo) CountLicenseProcesses(
	ctx context.Context, accountID, licenseID uuid.UUID, owner *uuid.UUID,
) (int, error) {
	row, err := r.q.CountLicenseProcesses(ctx, db.CountLicenseProcessesParams{
		AccountID: accountID, LicenseID: licenseID, OwnerID: owner,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count license processes: %w", err)
	}

	return int(row.Count), nil
}

// CountMachineProcesses answers the machine's process count.
func (r LicensesRepo) CountMachineProcesses(ctx context.Context, accountID, machineID uuid.UUID) (int, error) {
	row, err := r.q.CountMachineProcesses(ctx, db.CountMachineProcessesParams{
		AccountID: accountID, MachineID: machineID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count machine processes: %w", err)
	}

	return int(row.Count), nil
}

// --- the column plumbing ---

func licenseList[T any](rows []T, conv func(T) db.GetLicenseByIDRow, err error) ([]domain.License, error) {
	if err != nil {
		return nil, fmt.Errorf("postgres: list licenses: %w", err)
	}

	out := make([]domain.License, 0, len(rows))
	for _, row := range rows {
		l, err := licenseDomain(conv(row), nil)
		if err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, nil
}

func licenseDomain( //nolint:funlen // the policy columns, one field each
	row db.GetLicenseByIDRow, err error,
) (domain.License, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.License{}, fmt.Errorf("postgres: license: %w", errLicenseNotFound)
	}

	if err != nil {
		return domain.License{}, fmt.Errorf("postgres: license: %w", err)
	}

	meta := map[string]any{}
	_ = json.Unmarshal(row.Metadata, &meta)

	policyMeta := map[string]any{}
	_ = json.Unmarshal(row.PolicyMetadata, &policyMeta)

	return domain.License{
		ID:                    row.ID,
		AccountID:             row.AccountID,
		ProductID:             row.ProductID,
		PolicyID:              row.PolicyID,
		OwnerID:               nullUUID(row.UserID),
		GroupID:               nullUUID(row.GroupID),
		Name:                  row.Name,
		Key:                   row.Key,
		Expiry:                row.Expiry,
		Uses:                  row.Uses,
		Suspended:             row.Suspended,
		Protected:             row.Protected,
		Metadata:              meta,
		MaxMachinesOverride:   row.MaxMachinesOverride,
		MaxProcessesOverride:  row.MaxProcessesOverride,
		MaxUsersOverride:      row.MaxUsersOverride,
		MaxCoresOverride:      row.MaxCoresOverride,
		MaxMemoryOverride:     row.MaxMemoryOverride,
		MaxDiskOverride:       row.MaxDiskOverride,
		MaxUsesOverride:       row.MaxUsesOverride,
		MachinesCount:         row.MachinesCount,
		MachinesCoreCount:     row.MachinesCoreCount,
		MachinesMemoryCount:   row.MachinesMemoryCount,
		MachinesDiskCount:     row.MachinesDiskCount,
		LicenseUsersCount:     row.LicenseUsersCount,
		LastValidatedAt:       row.LastValidatedAt,
		LastValidatedChecksum: row.LastValidatedChecksum,
		LastValidatedVersion:  row.LastValidatedVersion,
		LastCheckInAt:         row.LastCheckInAt,
		LastCheckOutAt:        row.LastCheckOutAt,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		OwnerBanned:           row.OwnerBanned,
		OwnerEmail:            row.OwnerEmail,
		Policy: domain.Policy{ //nolint:dupl // the policy columns mirror the policies repository
			ID:                            row.PolicyId2,
			AccountID:                     row.PolicyAccountID,
			ProductID:                     row.PolicyProductID,
			Name:                          row.PolicyName,
			Duration:                      row.PolicyDuration,
			Strict:                        row.PolicyStrict,
			Floating:                      row.PolicyFloating,
			UsePool:                       row.PolicyUsePool,
			MaxMachines:                   row.PolicyMaxMachines,
			MaxProcesses:                  row.PolicyMaxProcesses,
			MaxUsers:                      row.PolicyMaxUsers,
			MaxCores:                      row.PolicyMaxCores,
			MaxMemory:                     row.PolicyMaxMemory,
			MaxDisk:                       row.PolicyMaxDisk,
			MaxUses:                       row.PolicyMaxUses,
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
			Scheme:                        deref(row.PolicyScheme),
			Encrypted:                     row.PolicyEncrypted,
			Protected:                     row.PolicyProtected,
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
			Metadata:                      policyMeta,
			CreatedAt:                     row.PolicyCreatedAt,
			UpdatedAt:                     row.PolicyUpdatedAt,
		},
	}, nil
}

func listLicensesArgs(
	accountID uuid.UUID, f licenses.Filter, scopeKind string, scopeID uuid.UUID,
	page Page, now time.Time,
) db.ListLicensesParams {
	kind := (*string)(nil)
	if scopeKind != "" {
		kind = &scopeKind
	}

	meta, metaCast := json.RawMessage(nil), json.RawMessage(nil)
	if f.Metadata != nil {
		meta, metaCast = metadataJSON(f.Metadata)
	}

	return db.ListLicensesParams{
		AccountID:             accountID,
		FilterProductID:       nullUUIDPtr(f.ProductID),
		FilterPolicyID:        nullUUIDPtr(f.PolicyID),
		FilterOwnerID:         nullUUIDPtr(f.OwnerID),
		FilterOwnerEmail:      ref(f.OwnerEmail),
		FilterOwnerEmailLike:  ref(f.OwnerEmailLike),
		FilterUserID:          nullUUIDPtr(f.UserID),
		FilterUserEmail:       ref(f.UserEmail),
		FilterUserEmailLike:   ref(f.UserEmailLike),
		FilterMachineID:       nullUUIDPtr(f.MachineID),
		FilterGroupID:         nullUUIDPtr(f.GroupID),
		FilterStatus:          ref(f.Status),
		Now:                   &now,
		ExpiringSoon:          timePtr(now.Add(3 * 24 * time.Hour)),
		ActivityCutoff:        now.Add(-90 * 24 * time.Hour),
		FilterSuspended:       f.Suspended,
		FilterExpiring:        f.Expiring,
		FilterExpired:         f.Expired,
		FilterUnexpired:       f.Unexpired,
		ExpiryGte1:            f.ExpiryGte1,
		ExpiryGte2:            f.ExpiryGte2,
		ExpiryLte:             f.ExpiryLte,
		ActivityActiveSince:   f.ActivityActiveSince,
		ActivityInactiveSince: f.ActivityInactiveSince,
		FilterMetadata:        meta,
		FilterMetadataCast:    metaCast,
		FilterAssigned:        f.Assigned,
		FilterUnassigned:      f.Unassigned,
		FilterActivated:       f.Activated,
		ActivationsEq:         f.ActivationsEq,
		ActivationsGt:         f.ActivationsGt,
		ActivationsGte:        f.ActivationsGte,
		ActivationsLt:         f.ActivationsLt,
		ActivationsLte:        f.ActivationsLte,
		ScopeKind:             kind,
		ScopeID:               scopeID,
		Cursor:                page.Cursor,
		Limit:                 page.Fetch,
		Offset:                page.Offset,
	}
}

func listLicensesArgsAsc(args db.ListLicensesParams) db.ListLicensesAscParams {
	return db.ListLicensesAscParams{ //nolint:staticcheck // mirrors the generated row types
		AccountID:             args.AccountID,
		FilterProductID:       args.FilterProductID,
		FilterPolicyID:        args.FilterPolicyID,
		FilterOwnerID:         args.FilterOwnerID,
		FilterOwnerEmail:      args.FilterOwnerEmail,
		FilterOwnerEmailLike:  args.FilterOwnerEmailLike,
		FilterUserID:          args.FilterUserID,
		FilterUserEmail:       args.FilterUserEmail,
		FilterUserEmailLike:   args.FilterUserEmailLike,
		FilterMachineID:       args.FilterMachineID,
		FilterGroupID:         args.FilterGroupID,
		FilterStatus:          args.FilterStatus,
		Now:                   args.Now,
		ExpiringSoon:          args.ExpiringSoon,
		ActivityCutoff:        args.ActivityCutoff,
		FilterSuspended:       args.FilterSuspended,
		FilterExpiring:        args.FilterExpiring,
		FilterExpired:         args.FilterExpired,
		FilterUnexpired:       args.FilterUnexpired,
		ExpiryGte1:            args.ExpiryGte1,
		ExpiryGte2:            args.ExpiryGte2,
		ExpiryLte:             args.ExpiryLte,
		ActivityActiveSince:   args.ActivityActiveSince,
		ActivityInactiveSince: args.ActivityInactiveSince,
		FilterMetadata:        args.FilterMetadata,
		FilterMetadataCast:    args.FilterMetadataCast,
		FilterAssigned:        args.FilterAssigned,
		FilterUnassigned:      args.FilterUnassigned,
		FilterActivated:       args.FilterActivated,
		ActivationsEq:         args.ActivationsEq,
		ActivationsGt:         args.ActivationsGt,
		ActivationsGte:        args.ActivationsGte,
		ActivationsLt:         args.ActivationsLt,
		ActivationsLte:        args.ActivationsLte,
		ScopeKind:             args.ScopeKind,
		ScopeID:               args.ScopeID,
		Cursor:                args.Cursor,
		Limit:                 args.Limit,
		Offset:                args.Offset,
	}
}

func countLicensesArgs(args db.ListLicensesParams) db.CountLicensesParams {
	return db.CountLicensesParams{
		AccountID:             args.AccountID,
		FilterProductID:       args.FilterProductID,
		FilterPolicyID:        args.FilterPolicyID,
		FilterOwnerID:         args.FilterOwnerID,
		FilterOwnerEmail:      args.FilterOwnerEmail,
		FilterOwnerEmailLike:  args.FilterOwnerEmailLike,
		FilterUserID:          args.FilterUserID,
		FilterUserEmail:       args.FilterUserEmail,
		FilterUserEmailLike:   args.FilterUserEmailLike,
		FilterMachineID:       args.FilterMachineID,
		FilterGroupID:         args.FilterGroupID,
		FilterStatus:          args.FilterStatus,
		Now:                   args.Now,
		ExpiringSoon:          args.ExpiringSoon,
		ActivityCutoff:        args.ActivityCutoff,
		FilterSuspended:       args.FilterSuspended,
		FilterExpiring:        args.FilterExpiring,
		FilterExpired:         args.FilterExpired,
		FilterUnexpired:       args.FilterUnexpired,
		ExpiryGte1:            args.ExpiryGte1,
		ExpiryGte2:            args.ExpiryGte2,
		ExpiryLte:             args.ExpiryLte,
		ActivityActiveSince:   args.ActivityActiveSince,
		ActivityInactiveSince: args.ActivityInactiveSince,
		FilterMetadata:        args.FilterMetadata,
		FilterMetadataCast:    args.FilterMetadataCast,
		FilterAssigned:        args.FilterAssigned,
		FilterUnassigned:      args.FilterUnassigned,
		FilterActivated:       args.FilterActivated,
		ActivationsEq:         args.ActivationsEq,
		ActivationsGt:         args.ActivationsGt,
		ActivationsGte:        args.ActivationsGte,
		ActivationsLt:         args.ActivationsLt,
		ActivationsLte:        args.ActivationsLte,
		ScopeKind:             args.ScopeKind,
		ScopeID:               args.ScopeID,
	}
}

func metadataJSON(m map[string]any) (json.RawMessage, json.RawMessage) {
	raw, cast := map[string]any{}, map[string]any{}
	for k, v := range m {
		raw[k] = v
		cast[k] = castMetaValue(v)
	}

	return jsonMap(raw), jsonMap(cast)
}

func pageNone() Page { return Page{Limit: 1, Fetch: 1} }

func timePtr(t time.Time) *time.Time { return &t }

func ttlPtr(ttl int32) *int32 { return &ttl }

// machineHeartbeatStatus computes NOT_STARTED/ALIVE/DEAD (the default
// TTL applies when the policy sets none).
func machineHeartbeatStatus(now, createdAt time.Time, lastHeartbeat *time.Time, duration *int32) string {
	ttl := domain.HeartbeatTTLSeconds
	if duration != nil {
		ttl = *duration
	}

	if lastHeartbeat == nil {
		if now.Sub(createdAt) < time.Duration(ttl)*time.Second {
			return domain.MachineNotStarted
		}

		return domain.MachineDead
	}

	if now.Sub(*lastHeartbeat) < time.Duration(ttl)*time.Second {
		return domain.MachineAlive
	}

	return domain.MachineDead
}

// machineDomain assembles a machine domain value from a row's columns.
func machineDomain(
	id, accountID, licenseID, policyID uuid.UUID,
	ownerID, groupID *uuid.UUID,
	name *string, fingerprint string,
	ipAddr, hostname, platform *string,
	cores *int32, memory, disk *int64,
	maxProcesses *int32,
	metadata json.RawMessage,
	lastHeartbeat, lastCheckOut *time.Time,
	heartbeatStatus string,
	requireHeartbeat bool, heartbeatDuration *int32,
	createdAt, updatedAt time.Time,
) domain.Machine {
	meta := map[string]any{}
	_ = json.Unmarshal(metadata, &meta)

	return domain.Machine{
		ID:                   id,
		AccountID:            accountID,
		LicenseID:            licenseID,
		PolicyID:             policyID,
		OwnerID:              nullUUID(ownerID),
		GroupID:              nullUUID(groupID),
		Name:                 name,
		Fingerprint:          fingerprint,
		IP:                   ipAddr,
		Hostname:             hostname,
		Platform:             platform,
		Cores:                cores,
		Memory:               memory,
		Disk:                 disk,
		MaxProcessesOverride: maxProcesses,
		Metadata:             meta,
		LastHeartbeatAt:      lastHeartbeat,
		LastCheckOutAt:       lastCheckOut,
		HeartbeatStatus:      heartbeatStatus,
		RequireHeartbeat:     requireHeartbeat,
		HeartbeatDuration:    heartbeatDuration,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
	}
}

// castMetaValue mirrors the reference's metadata type-cast attempt for
// querystring values.
func castMetaValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}

	switch s {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	default:
		if i, err := strconv.Atoi(s); err == nil {
			return i
		}

		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}

		return s
	}
}

// --- the union entitlement documents of a license ---

// ListLicenseEntitlementsOf pages the union of the policy- and
// license-attached entitlements.
func (r LicensesRepo) ListLicenseEntitlementsOf(
	ctx context.Context, accountID, policyID, licenseID uuid.UUID, page Page,
) ([]domain.Entitlement, error) {
	rows, err := r.q.ListLicenseEntitlementsOf(ctx, db.ListLicenseEntitlementsOfParams{
		AccountID: accountID, PolicyID: policyID, LicenseID: licenseID,
		Limit: page.Fetch, Offset: page.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: license entitlements: %w", err)
	}

	out := make([]domain.Entitlement, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		meta := map[string]any{}
		_ = json.Unmarshal(row.Metadata, &meta)

		out = append(out, domain.Entitlement{
			ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
			Metadata: meta, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}

	return out, nil
}

// CountLicenseEntitlementsOfUnion counts the union.
func (r LicensesRepo) CountLicenseEntitlementsOfUnion(
	ctx context.Context, accountID, policyID, licenseID uuid.UUID,
) (int, error) {
	row, err := r.q.CountLicenseEntitlementsOfUnion(ctx, db.CountLicenseEntitlementsOfUnionParams{
		AccountID: accountID, PolicyID: policyID, LicenseID: licenseID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count license entitlements: %w", err)
	}

	return int(row.Count), nil
}

// LicenseEntitlementOf loads one of the license's entitlements.
func (r LicensesRepo) LicenseEntitlementOf(
	ctx context.Context, accountID, policyID, licenseID, id uuid.UUID,
) (domain.Entitlement, error) {
	row, err := r.q.GetLicenseEntitlementOf(ctx, db.GetLicenseEntitlementOfParams{
		AccountID: accountID, PolicyID: policyID, LicenseID: licenseID, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Entitlement{}, fmt.Errorf("postgres: entitlement: %w", errLicenseNotFound)
	}

	if err != nil {
		return domain.Entitlement{}, fmt.Errorf("postgres: license entitlement: %w", err)
	}

	meta := map[string]any{}
	_ = json.Unmarshal(row.Metadata, &meta)

	return domain.Entitlement{
		ID: row.ID, AccountID: row.AccountID, Name: row.Name, Code: row.Code,
		Metadata: meta, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}
