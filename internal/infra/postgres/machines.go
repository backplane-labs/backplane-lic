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

	"github.com/backplane-labs/backplane-lic/internal/app/machines"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// pgLockNotAvailable is SQLSTATE 55P03, a NOWAIT lock already held.
const pgLockNotAvailable = "55P03"

// MachinesRepo adapts the machine rows to the machines scenario's
// ports (machines, the license lock, activation-token counters).
type MachinesRepo struct {
	q *db.Queries
}

// ListMachines reads one page of the scoped, filtered index.
func (r MachinesRepo) ListMachines(
	ctx context.Context, accountID uuid.UUID, f machines.Filter, scope machines.Scope, page Page, now time.Time,
) ([]domain.Machine, error) {
	rows, err := r.q.ListMachines(ctx, listMachinesArgs(accountID, f, scope, "", page, now))
	if err != nil {
		return nil, fmt.Errorf("postgres: machines: %w", err)
	}

	out := make([]domain.Machine, 0, len(rows))
	for i := range rows {
		out = append(out, machineOf(rows[i], now))
	}

	return out, nil
}

// CountMachines counts the scoped, filtered index.
func (r MachinesRepo) CountMachines(
	ctx context.Context, accountID uuid.UUID, f machines.Filter, scope machines.Scope, now time.Time,
) (int, error) {
	rows, err := r.q.ListMachines(ctx, listMachinesArgs(accountID, f, scope, "", Page{Fetch: 1}, now))
	if err != nil {
		return 0, fmt.Errorf("postgres: count machines: %w", err)
	}

	if len(rows) == 0 {
		return 0, nil
	}

	return int(rows[0].Total), nil
}

// FindMachine resolves an id or a fingerprint within the scope, the
// oldest machine first (FindByAliasService).
func (r MachinesRepo) FindMachine(
	ctx context.Context, accountID uuid.UUID, scope machines.Scope, idOrFingerprint string, now time.Time,
) (domain.Machine, error) {
	if idOrFingerprint == "" {
		return domain.Machine{}, fmt.Errorf("postgres: machine: %w", domain.ErrNotFound)
	}

	rows, err := r.q.ListMachines(ctx,
		listMachinesArgs(accountID, machines.Filter{}, scope, idOrFingerprint, Page{Fetch: 1, Asc: true}, now))
	if err != nil {
		return domain.Machine{}, fmt.Errorf("postgres: machine: %w", err)
	}

	if len(rows) == 0 {
		return domain.Machine{}, fmt.Errorf("postgres: machine: %w", domain.ErrNotFound)
	}

	return machineOf(rows[0], now), nil
}

// InsertMachine writes a new machine.
func (r MachinesRepo) InsertMachine(ctx context.Context, m domain.Machine) error {
	err := r.q.InsertMachine(ctx, db.InsertMachineParams{
		ID: m.ID, AccountID: m.AccountID, LicenseID: m.LicenseID, PolicyID: m.PolicyID,
		OwnerID: nullUUIDPtr(m.OwnerID), GroupID: nullUUIDPtr(m.GroupID), Name: m.Name, Fingerprint: m.Fingerprint,
		IP: m.IP, Hostname: m.Hostname, Platform: m.Platform, Cores: m.Cores, Memory: m.Memory, Disk: m.Disk,
		MaxProcessesOverride: m.MaxProcessesOverride, Metadata: jsonMap(m.Metadata),
		LastHeartbeatAt: m.LastHeartbeatAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert machine: %w", err)
	}

	return nil
}

// UpdateMachine writes every mutable column.
func (r MachinesRepo) UpdateMachine(ctx context.Context, m domain.Machine) error {
	err := r.q.UpdateMachine(ctx, db.UpdateMachineParams{
		OwnerID: nullUUIDPtr(m.OwnerID), GroupID: nullUUIDPtr(m.GroupID), Name: m.Name, IP: m.IP,
		Hostname: m.Hostname, Platform: m.Platform, Cores: m.Cores, Memory: m.Memory, Disk: m.Disk,
		MaxProcessesOverride: m.MaxProcessesOverride, Metadata: jsonMap(m.Metadata),
		LastHeartbeatAt: m.LastHeartbeatAt, LastCheckOutAt: m.LastCheckOutAt,
		LastDeathEventSentAt: m.LastDeathEventSentAt, UpdatedAt: m.UpdatedAt, AccountID: m.AccountID, ID: m.ID,
	})
	if err != nil {
		return fmt.Errorf("postgres: update machine: %w", err)
	}

	return nil
}

// DeleteMachine removes the machine (its components and processes
// cascade).
func (r MachinesRepo) DeleteMachine(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteMachine(ctx, db.DeleteMachineParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: delete machine: %w", err)
	}

	return nil
}

// MachineExists answers whether a machine has the id.
func (r MachinesRepo) MachineExists(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	row, err := r.q.MachineExists(ctx, db.MachineExistsParams{AccountID: accountID, ID: id})
	if err != nil {
		return false, fmt.Errorf("postgres: machine exists: %w", err)
	}

	return row.Column, nil
}

// FingerprintTaken answers whether the fingerprint exists in the space.
func (r MachinesRepo) FingerprintTaken(
	ctx context.Context, accountID uuid.UUID, in machines.Uniqueness, fingerprint string,
) (bool, error) {
	row, err := r.q.FingerprintTaken(ctx, db.FingerprintTakenParams{
		AccountID: accountID, Fingerprint: fingerprint, Kind: in.Kind, ScopeID: in.ID,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: fingerprint taken: %w", err)
	}

	return row.Column, nil
}

// MachineTotals sums the license's machines other than except.
func (r MachinesRepo) MachineTotals(
	ctx context.Context, licenseID uuid.UUID, perUser bool, owner machines.Owner, except uuid.UUID,
) (machines.Totals, error) {
	row, err := r.q.MachineTotals(ctx, db.MachineTotalsParams{
		LicenseID: licenseID, Except: except, PerUser: perUser, OwnerID: nullUUIDPtr(owner),
	})
	if err != nil {
		return machines.Totals{}, fmt.Errorf("postgres: machine totals: %w", err)
	}

	return machines.Totals{Count: row.Count, Cores: row.Cores, Memory: row.Memory, Disk: row.Disk}, nil
}

// RefreshLicenseCounters rewrites the license's machine counter cache.
func (r MachinesRepo) RefreshLicenseCounters(ctx context.Context, accountID, licenseID uuid.UUID, now time.Time) error {
	err := r.q.RefreshLicenseMachineCounters(ctx, db.RefreshLicenseMachineCountersParams{
		UpdatedAt: now, LicenseID: licenseID, AccountID: accountID,
	})
	if err != nil {
		return fmt.Errorf("postgres: license machine counters: %w", err)
	}

	return nil
}

// LicenseByID loads the license with its policy.
func (r MachinesRepo) LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.License, error) {
	return LicensesRepo(r).LicenseByID(ctx, accountID, id)
}

// LockLicense takes the license row lock for the transaction.
func (r MachinesRepo) LockLicense(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.LockLicense(ctx, db.LockLicenseParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: lock license: %w", err)
	}

	return nil
}

// LicenseHasUser answers whether the user owns or uses the license.
func (r MachinesRepo) LicenseHasUser(ctx context.Context, licenseID, userID uuid.UUID) (bool, error) {
	row, err := r.q.LicenseHasUser(ctx, db.LicenseHasUserParams{LicenseID: licenseID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("postgres: license user: %w", err)
	}

	return row.Column, nil
}

// GroupMachineCount counts the group's machines.
func (r MachinesRepo) GroupMachineCount(ctx context.Context, groupID uuid.UUID) (int, error) {
	row, err := r.q.GroupMachineCount(ctx, &groupID)
	if err != nil {
		return 0, fmt.Errorf("postgres: group machines: %w", err)
	}

	return int(row.Column), nil
}

// InsertComponent writes a component.
func (r MachinesRepo) InsertComponent(ctx context.Context, c domain.MachineComponent) error {
	err := r.q.InsertMachineComponent(ctx, db.InsertMachineComponentParams{
		ID: c.ID, AccountID: c.AccountID, MachineID: c.MachineID, Name: c.Name, Fingerprint: c.Fingerprint,
		Metadata: jsonMap(c.Metadata), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert component: %w", err)
	}

	return nil
}

// TokenByID loads the token.
func (r MachinesRepo) TokenByID(ctx context.Context, accountID, id uuid.UUID) (domain.Token, error) {
	return TokensRepo(r).TokenByID(ctx, accountID, id)
}

// LockToken locks the token row NOWAIT or SKIP LOCKED; locked is false
// when another transaction holds it.
func (r MachinesRepo) LockToken(ctx context.Context, accountID, id uuid.UUID, nowait bool) (domain.Token, bool, error) {
	var (
		row db.TokenForUpdateNowaitRow
		err error
	)

	if nowait {
		row, err = r.q.TokenForUpdateNowait(ctx, db.TokenForUpdateNowaitParams{AccountID: accountID, ID: id})
	} else {
		var skip db.TokenForUpdateSkipLockedRow

		skip, err = r.q.TokenForUpdateSkipLocked(ctx, db.TokenForUpdateSkipLockedParams{AccountID: accountID, ID: id})
		row = db.TokenForUpdateNowaitRow(skip)
	}

	var pgErr *pgconn.PgError

	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.As(err, &pgErr) && pgErr.Code == pgLockNotAvailable:
		return domain.Token{}, false, nil
	case err != nil:
		return domain.Token{}, false, fmt.Errorf("postgres: lock token: %w", err)
	}

	return domain.Token{
		ID: row.ID, AccountID: accountID, MaxActivations: intPtr(row.MaxActivations), Activations: int(row.Activations),
		MaxDeactivations: intPtr(row.MaxDeactivations), Deactivations: int(row.Deactivations),
		BearerType: "License",
	}, true, nil
}

// SetTokenCounters writes the token's activation counters.
func (r MachinesRepo) SetTokenCounters(
	ctx context.Context, accountID, id uuid.UUID, activations, deactivations int, now time.Time,
) error {
	err := r.q.SetTokenActivationCounters(ctx, db.SetTokenActivationCountersParams{
		Activations:   clampInt32(activations),
		Deactivations: clampInt32(deactivations),
		UpdatedAt:     now, AccountID: accountID, ID: id,
	})
	if err != nil {
		return fmt.Errorf("postgres: token counters: %w", err)
	}

	return nil
}

const maxInt32 = 1<<31 - 1

// clampInt32 narrows a counter to the column's int32 range.
func clampInt32(v int) int32 {
	if v > maxInt32 {
		return maxInt32
	}

	if v < 0 {
		return 0
	}

	return int32(v)
}

func listMachinesArgs(
	accountID uuid.UUID, f machines.Filter, scope machines.Scope, idOrFingerprint string, page Page, now time.Time,
) db.ListMachinesParams {
	args := db.ListMachinesParams{
		AccountID: accountID, FilterIds: f.IDs, IDOrFingerprint: refOrNil(idOrFingerprint),
		FilterFingerprint: refOrNil(f.Fingerprint), FilterIP: refOrNil(f.IP), FilterHostname: refOrNil(f.Hostname),
		FilterProductID: nullUUIDPtr(f.ProductID), FilterPolicyID: nullUUIDPtr(f.PolicyID),
		FilterLicenseID: nullUUIDPtr(f.LicenseID), FilterKey: refOrNil(f.Key), FilterGroupID: nullUUIDPtr(f.GroupID),
		FilterOwnerNull: f.OwnerNull, FilterOwner: refOrNil(f.Owner), FilterUser: refOrNil(f.User),
		FilterStatus: refOrNil(f.Status), Now: now, Cursor: page.Cursor, Asc: page.Asc,
		Fetch: page.Fetch, PageOffset: page.Offset,
	}

	if f.Metadata != nil {
		args.FilterMetadata, args.FilterMetadataCast = metadataJSON(f.Metadata)
	}

	if scope.Kind != "" {
		args.ScopeKind, args.ScopeID = &scope.Kind, &scope.ID
	}

	return args
}

// machineOf maps a row; the status is computed at now.
func machineOf(row db.ListMachinesRow, now time.Time) domain.Machine {
	m := domain.Machine{
		ID: row.ID, AccountID: row.AccountID, LicenseID: row.LicenseID, PolicyID: row.PolicyID,
		OwnerID: nullUUID(row.OwnerID), GroupID: nullUUID(row.GroupID), Name: row.Name, Fingerprint: row.Fingerprint,
		IP: row.IP, Hostname: row.Hostname, Platform: row.Platform, Cores: row.Cores, Memory: row.Memory,
		Disk: row.Disk, MaxProcessesOverride: row.MaxProcessesOverride, Metadata: metaOf(row.Metadata),
		LastHeartbeatAt: row.LastHeartbeatAt, LastCheckOutAt: row.LastCheckOutAt,
		LastDeathEventSentAt: row.LastDeathEventSentAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		ProductID: row.ProductID, LicenseOwnerID: nullUUID(row.LicenseOwnerID), LicenseProtected: row.LicenseProtected,
		LicenseMaxProcesses: row.LicenseMaxProcesses,
		Heartbeat: domain.HeartbeatPolicy{
			Require: row.RequireHeartbeat, Duration: row.HeartbeatDuration, Basis: deref(row.HeartbeatBasis),
			Resurrect: deref(row.HeartbeatResurrectionStrategy), CullOnDeath: deref(row.HeartbeatCullStrategy),
		},
	}

	m.HeartbeatStatus = m.Status(now)

	return m
}

func metaOf(raw json.RawMessage) map[string]any {
	meta := map[string]any{}
	_ = json.Unmarshal(raw, &meta)

	return meta
}

func refOrNil(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
