package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/machines"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// ListComponents reads a page of the scoped components and the total.
func (r MachinesRepo) ListComponents(
	ctx context.Context, accountID uuid.UUID, f machines.SubFilter, scope machines.Scope, page Page,
) ([]domain.MachineComponent, int, error) {
	return r.components(ctx, accountID, f, scope, nil, page)
}

// FindComponent loads a component within the scope.
func (r MachinesRepo) FindComponent(
	ctx context.Context, accountID uuid.UUID, scope machines.Scope, id uuid.UUID,
) (domain.MachineComponent, error) {
	found, _, err := r.components(ctx, accountID, machines.SubFilter{}, scope, &id, Page{Fetch: 1})
	if err != nil {
		return domain.MachineComponent{}, err
	}

	if len(found) == 0 {
		return domain.MachineComponent{}, fmt.Errorf("postgres: component: %w", domain.ErrNotFound)
	}

	return found[0], nil
}

func (r MachinesRepo) components(
	ctx context.Context, accountID uuid.UUID, f machines.SubFilter, scope machines.Scope, id *uuid.UUID, page Page,
) ([]domain.MachineComponent, int, error) {
	args := db.ListComponentsParams{
		AccountID: accountID, ComponentID: id, FilterMachineID: nullUUIDPtr(f.MachineID),
		FilterProductID: nullUUIDPtr(f.ProductID), FilterLicenseID: nullUUIDPtr(f.LicenseID),
		FilterOwnerID: nullUUIDPtr(f.OwnerID), FilterUser: refOrNil(f.User),
		Cursor: page.Cursor, Asc: page.Asc, Fetch: page.Fetch, PageOffset: page.Offset,
	}

	if scope.Kind != "" {
		args.ScopeKind, args.ScopeID = &scope.Kind, &scope.ID
	}

	rows, err := r.q.ListComponents(ctx, args)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: components: %w", err)
	}

	out := make([]domain.MachineComponent, 0, len(rows))
	total := 0

	for i := range rows {
		row := &rows[i]
		total = int(row.Total)
		out = append(out, domain.MachineComponent{
			ID: row.ID, AccountID: row.AccountID, MachineID: row.MachineID, Name: row.Name, Fingerprint: row.Fingerprint,
			Metadata: metaOf(row.Metadata), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			ProductID: row.ProductID, LicenseID: row.LicenseID,
		})
	}

	return out, total, nil
}

// UpdateComponent writes the mutable columns.
func (r MachinesRepo) UpdateComponent(ctx context.Context, c domain.MachineComponent) error {
	err := r.q.UpdateMachineComponent(ctx, db.UpdateMachineComponentParams{
		Name: c.Name, Metadata: jsonMap(c.Metadata), UpdatedAt: c.UpdatedAt, AccountID: c.AccountID, ID: c.ID,
	})
	if err != nil {
		return fmt.Errorf("postgres: update component: %w", err)
	}

	return nil
}

// DeleteComponent removes the component.
func (r MachinesRepo) DeleteComponent(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteMachineComponent(ctx, db.DeleteMachineComponentParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: delete component: %w", err)
	}

	return nil
}

// ComponentFingerprintTaken answers whether the fingerprint exists in the
// space.
func (r MachinesRepo) ComponentFingerprintTaken(
	ctx context.Context, accountID uuid.UUID, in machines.Uniqueness, fingerprint string,
) (bool, error) {
	row, err := r.q.ComponentFingerprintTaken(ctx, db.ComponentFingerprintTakenParams{
		AccountID: accountID, Fingerprint: fingerprint, Kind: in.Kind, ScopeID: in.ID,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: component fingerprint taken: %w", err)
	}

	return row.Column, nil
}

// ListProcesses reads a page of the scoped processes and the total.
func (r MachinesRepo) ListProcesses(
	ctx context.Context, accountID uuid.UUID, f machines.SubFilter, scope machines.Scope, page Page, now time.Time,
) ([]domain.MachineProcess, int, error) {
	return r.processes(ctx, accountID, f, scope, nil, page, now)
}

// FindProcess loads a process within the scope.
func (r MachinesRepo) FindProcess(
	ctx context.Context, accountID uuid.UUID, scope machines.Scope, id uuid.UUID, now time.Time,
) (domain.MachineProcess, error) {
	found, _, err := r.processes(ctx, accountID, machines.SubFilter{}, scope, &id, Page{Fetch: 1}, now)
	if err != nil {
		return domain.MachineProcess{}, err
	}

	if len(found) == 0 {
		return domain.MachineProcess{}, fmt.Errorf("postgres: process: %w", domain.ErrNotFound)
	}

	return found[0], nil
}

func (r MachinesRepo) processes(
	ctx context.Context, accountID uuid.UUID, f machines.SubFilter, scope machines.Scope, id *uuid.UUID, page Page,
	now time.Time,
) ([]domain.MachineProcess, int, error) {
	args := db.ListProcessesParams{
		AccountID: accountID, ProcessID: id, FilterMachineID: nullUUIDPtr(f.MachineID),
		FilterProductID: nullUUIDPtr(f.ProductID), FilterLicenseID: nullUUIDPtr(f.LicenseID),
		FilterOwnerID: nullUUIDPtr(f.OwnerID), FilterUser: refOrNil(f.User), FilterStatus: refOrNil(f.Status),
		Now: now, Cursor: page.Cursor, Asc: page.Asc, Fetch: page.Fetch, PageOffset: page.Offset,
	}

	if scope.Kind != "" {
		args.ScopeKind, args.ScopeID = &scope.Kind, &scope.ID
	}

	rows, err := r.q.ListProcesses(ctx, args)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: processes: %w", err)
	}

	out := make([]domain.MachineProcess, 0, len(rows))
	total := 0

	for i := range rows {
		row := &rows[i]
		total = int(row.Total)
		p := domain.MachineProcess{
			ID: row.ID, AccountID: row.AccountID, MachineID: row.MachineID, PID: row.Pid, Metadata: metaOf(row.Metadata),
			LastHeartbeatAt: row.LastHeartbeatAt, LastDeathEventSentAt: row.LastDeathEventSentAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ProductID: row.ProductID, LicenseID: row.LicenseID,
			Heartbeat: domain.HeartbeatPolicy{
				Duration: row.HeartbeatDuration, Resurrect: deref(row.HeartbeatResurrectionStrategy),
				CullOnDeath: deref(row.HeartbeatCullStrategy),
			},
		}
		p.HeartbeatStatus = p.Status(now)
		out = append(out, p)
	}

	return out, total, nil
}

// InsertProcess writes a new process.
func (r MachinesRepo) InsertProcess(ctx context.Context, p domain.MachineProcess) error {
	err := r.q.InsertMachineProcess(ctx, db.InsertMachineProcessParams{
		ID: p.ID, AccountID: p.AccountID, MachineID: p.MachineID, Pid: p.PID, Metadata: jsonMap(p.Metadata),
		LastHeartbeatAt: p.LastHeartbeatAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert process: %w", err)
	}

	return nil
}

// UpdateProcess writes the mutable columns.
func (r MachinesRepo) UpdateProcess(ctx context.Context, p domain.MachineProcess) error {
	err := r.q.UpdateMachineProcess(ctx, db.UpdateMachineProcessParams{
		Metadata: jsonMap(p.Metadata), LastHeartbeatAt: p.LastHeartbeatAt, LastDeathEventSentAt: p.LastDeathEventSentAt,
		UpdatedAt: p.UpdatedAt, AccountID: p.AccountID, ID: p.ID,
	})
	if err != nil {
		return fmt.Errorf("postgres: update process: %w", err)
	}

	return nil
}

// DeleteProcess removes the process.
func (r MachinesRepo) DeleteProcess(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteMachineProcess(ctx, db.DeleteMachineProcessParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: delete process: %w", err)
	}

	return nil
}

// ProcessExists answers whether a process has the id.
func (r MachinesRepo) ProcessExists(ctx context.Context, id uuid.UUID) (bool, error) {
	row, err := r.q.ProcessExists(ctx, id)
	if err != nil {
		return false, fmt.Errorf("postgres: process exists: %w", err)
	}

	return row.Column, nil
}

// ProcessPidTaken answers whether the machine runs a process with the pid.
func (r MachinesRepo) ProcessPidTaken(ctx context.Context, machineID uuid.UUID, pid string) (bool, error) {
	row, err := r.q.ProcessPidTaken(ctx, db.ProcessPidTakenParams{MachineID: machineID, Pid: pid})
	if err != nil {
		return false, fmt.Errorf("postgres: pid taken: %w", err)
	}

	return row.Column, nil
}

// DeadMachines is a batch of dead machines for the cull.
func (r MachinesRepo) DeadMachines(ctx context.Context, now time.Time, limit int) ([]machines.Ref, error) {
	rows, err := r.q.DeadMachines(ctx, db.DeadMachinesParams{Now: now, Batch: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("postgres: dead machines: %w", err)
	}

	out := make([]machines.Ref, 0, len(rows))
	for _, row := range rows {
		out = append(out, machines.Ref{ID: row.ID, AccountID: row.AccountID})
	}

	return out, nil
}

// DeadProcesses is a batch of dead processes for the cull.
func (r MachinesRepo) DeadProcesses(ctx context.Context, now time.Time, limit int) ([]machines.Ref, error) {
	rows, err := r.q.DeadProcesses(ctx, db.DeadProcessesParams{Now: now, Batch: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("postgres: dead processes: %w", err)
	}

	out := make([]machines.Ref, 0, len(rows))
	for _, row := range rows {
		out = append(out, machines.Ref{ID: row.ID, AccountID: row.AccountID})
	}

	return out, nil
}

// ProcessCount counts the processes the limits compare against.
func (r MachinesRepo) ProcessCount(
	ctx context.Context, kind string, scopeID uuid.UUID, owner machines.Owner,
) (int64, error) {
	row, err := r.q.ProcessCount(ctx, db.ProcessCountParams{Kind: kind, ScopeID: scopeID, OwnerID: nullUUIDPtr(owner)})
	if err != nil {
		return 0, fmt.Errorf("postgres: process count: %w", err)
	}

	return row.Column, nil
}
