package machines

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ProcessList is one processes index page.
type ProcessList struct {
	Items []domain.MachineProcess
	Count int
	Next  uuid.NullUUID
}

// ProcessInput is the decoded spawn body.
type ProcessInput struct {
	// ID is the client-chosen id.
	ID       string
	PID      string
	Metadata map[string]any
}

// IndexProcesses answers the filtered page within the principal's scope.
func (s Scenario) IndexProcesses(
	ctx context.Context, account domain.Account, f SubFilter, params paging.Params,
) (ProcessList, error) {
	if err := access.Require(ctx, "process.read"); err != nil {
		return ProcessList{}, err
	}

	return s.processPage(ctx, account, f, params)
}

// MachineProcesses answers a machine's processes (its relationship
// index).
func (s Scenario) MachineProcesses(
	ctx context.Context, account domain.Account, idOrFingerprint string, f SubFilter, params paging.Params,
) (ProcessList, error) {
	machine, err := s.related(ctx, account, idOrFingerprint, "process.read")
	if err != nil {
		return ProcessList{}, err
	}

	f.MachineID = uuid.NullUUID{UUID: machine.ID, Valid: true}

	return s.processPage(ctx, account, f, params)
}

// MachineProcess answers one process of a machine.
func (s Scenario) MachineProcess(
	ctx context.Context, account domain.Account, idOrFingerprint, id string,
) (domain.MachineProcess, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.MachineProcess{}, err
	}

	process, err := s.findProcess(ctx, account, Scope{}, id)
	if err == nil && process.MachineID != machine.ID {
		err = ErrNotFound
	}

	if err != nil {
		return domain.MachineProcess{}, notFoundOr(err, "machine process", id)
	}

	if err := s.authorize(ctx, "process.read", machine, access.MachineShow); err != nil {
		return domain.MachineProcess{}, err
	}

	return process, nil
}

func (s Scenario) processPage(
	ctx context.Context, account domain.Account, f SubFilter, params paging.Params,
) (ProcessList, error) {
	window := params.Window()

	items, total, err := s.store.ListProcesses(ctx, account.ID, f, ScopeOf(access.PrincipalOf(ctx)), window, s.now())
	if err != nil {
		return ProcessList{}, fmt.Errorf("machines: processes: %w", err)
	}

	out := ProcessList{Items: items, Count: total}
	if params.Mode == paging.ModeCursor && len(items) > int(window.Limit) {
		out.Items = items[:window.Limit]
		out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
	}

	return out, nil
}

// ShowProcess answers one process.
func (s Scenario) ShowProcess(ctx context.Context, account domain.Account, id string) (domain.MachineProcess, error) {
	process, _, err := s.processAction(ctx, account, id, "process.read", access.MachineShow)

	return process, err
}

// SpawnProcess starts a process on a machine.
func (s Scenario) SpawnProcess(
	ctx context.Context, account domain.Account, machineID uuid.UUID, in ProcessInput,
) (domain.MachineProcess, error) {
	machine, found, err := s.machineOf(ctx, account, machineID)
	if err != nil {
		return domain.MachineProcess{}, err
	}

	if err := access.Require(ctx, "process.create"); err != nil {
		return domain.MachineProcess{}, err
	}

	if err := access.SubresourceCreate(access.PrincipalOf(ctx), machine); err != nil {
		return domain.MachineProcess{}, err
	}

	now := s.now()
	process := domain.MachineProcess{
		ID: uuid.New(), AccountID: account.ID, MachineID: machineID, PID: in.PID, Metadata: in.Metadata,
		LastHeartbeatAt: now, CreatedAt: now, UpdatedAt: now, Heartbeat: machine.Heartbeat,
		ProductID: machine.ProductID, LicenseID: machine.LicenseID,
	}

	if uuidRe.MatchString(in.ID) {
		process.ID = uuid.MustParse(in.ID)
	}

	if process.Metadata == nil {
		process.Metadata = map[string]any{}
	}

	err = s.write(ctx, func(ctx context.Context) error {
		if found {
			if err := s.licenses.LockLicense(ctx, account.ID, machine.LicenseID); err != nil {
				return fmt.Errorf("machines: lock license: %w", err)
			}
		}

		if err := s.validateProcess(ctx, account, machine, found, process, in.ID); err != nil {
			return err
		}

		if err := s.store.InsertProcess(ctx, process); err != nil {
			return fmt.Errorf("machines: spawn process: %w", err)
		}

		return s.publish(ctx, "process.created", process)
	})
	if err != nil {
		return domain.MachineProcess{}, err
	}

	process.HeartbeatStatus = process.Status(now)

	return process, nil
}

// UpdateProcess merges the metadata; decode reads the body once the
// process is found and authorized.
func (s Scenario) UpdateProcess(
	ctx context.Context, account domain.Account, id string, decode func() (Field[map[string]any], error),
) (domain.MachineProcess, error) {
	process, _, err := s.processAction(ctx, account, id, "process.update", access.SubresourceUpdate)
	if err != nil {
		return domain.MachineProcess{}, err
	}

	metadata, err := decode()
	if err != nil {
		return domain.MachineProcess{}, err
	}

	if metadata.Set {
		process.Metadata = map[string]any{}
		if metadata.Value != nil {
			process.Metadata = *metadata.Value
		}
	}

	v := violations{topic: "processes"}
	if detail := domain.MetadataViolation(process.Metadata); detail != "" {
		v.add("METADATA_TOO_LONG", detail, "/data/attributes/metadata")
	}

	if err := v.err(); err != nil {
		return domain.MachineProcess{}, err
	}

	process.UpdatedAt = s.now()

	return process, s.saveProcess(ctx, process, "process.updated")
}

// PingProcess records a process heartbeat; a dead process is resurrected
// when its policy allows it, else the ping is refused.
func (s Scenario) PingProcess(ctx context.Context, account domain.Account, id string) (domain.MachineProcess, error) {
	process, _, err := s.processAction(ctx, account, id, "process.heartbeat.ping", access.MachinePing)
	if err != nil {
		return domain.MachineProcess{}, err
	}

	now := s.now()
	event := "process.heartbeat.ping"

	if process.HeartbeatStatus == domain.MachineDead {
		if !process.Heartbeat.ResurrectDead() || process.ResurrectionPeriodPassed(now) {
			return domain.MachineProcess{}, apperr.Unprocessable("PROCESS_HEARTBEAT_DEAD", "is dead", "")
		}

		event = "process.heartbeat.resurrected"
		process.LastDeathEventSentAt = nil
	}

	process.LastHeartbeatAt, process.UpdatedAt = now, now

	if err := s.saveProcess(ctx, process, event); err != nil {
		return domain.MachineProcess{}, err
	}

	process.HeartbeatStatus = process.Status(now)
	if event == "process.heartbeat.resurrected" {
		process.HeartbeatStatus = domain.HeartbeatResurrected
	}

	return process, nil
}

// KillProcess removes the process.
func (s Scenario) KillProcess(ctx context.Context, account domain.Account, id string) error {
	process, _, err := s.processAction(ctx, account, id, "process.delete", access.SubresourceDestroy)
	if err != nil {
		return err
	}

	return s.write(ctx, func(ctx context.Context) error {
		if err := s.publish(ctx, "process.deleted", process); err != nil {
			return err
		}

		if err := s.store.DeleteProcess(ctx, account.ID, process.ID); err != nil {
			return fmt.Errorf("machines: kill process: %w", err)
		}

		return nil
	})
}

// ProcessMachine answers the process's machine; its product and license
// relationships read through it.
func (s Scenario) ProcessMachine(
	ctx context.Context, account domain.Account, id, permission string,
) (domain.Machine, error) {
	_, machine, err := s.processAction(ctx, account, id, permission, access.MachineShow)

	return machine, err
}

func (s Scenario) saveProcess(ctx context.Context, process domain.MachineProcess, event string) error {
	return s.write(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateProcess(ctx, process); err != nil {
			return fmt.Errorf("machines: update process: %w", err)
		}

		return s.publish(ctx, event, process)
	})
}

// processAction loads the process within the scope and authorizes the
// action against its machine.
func (s Scenario) processAction(
	ctx context.Context, account domain.Account, id, permission string,
	rule func(access.Principal, domain.Machine) error,
) (domain.MachineProcess, domain.Machine, error) {
	process, err := s.findProcess(ctx, account, ScopeOf(access.PrincipalOf(ctx)), id)
	if err != nil {
		return domain.MachineProcess{}, domain.Machine{}, notFoundOr(err, "machine process", id)
	}

	machine, _, err := s.machineOf(ctx, account, process.MachineID)
	if err != nil {
		return domain.MachineProcess{}, domain.Machine{}, err
	}

	if err := s.authorize(ctx, permission, machine, rule); err != nil {
		return domain.MachineProcess{}, domain.Machine{}, err
	}

	return process, machine, nil
}

func (s Scenario) findProcess(
	ctx context.Context, account domain.Account, scope Scope, id string,
) (domain.MachineProcess, error) {
	if !uuidRe.MatchString(id) {
		return domain.MachineProcess{}, ErrNotFound
	}

	process, err := s.store.FindProcess(ctx, account.ID, scope, uuid.MustParse(id), s.now())
	if err != nil {
		return domain.MachineProcess{}, fmt.Errorf("machines: process: %w", err)
	}

	return process, nil
}

// validateProcess runs the process model's create validations.
func (s Scenario) validateProcess(
	ctx context.Context, account domain.Account, machine domain.Machine, found bool, process domain.MachineProcess,
	id string,
) error {
	v := violations{topic: "processes"}

	if !found {
		v.add("MACHINE_NOT_FOUND", "must exist", "/data/relationships/machine")
	}

	if found {
		taken, err := s.store.ProcessPidTaken(ctx, machine.ID, process.PID)
		if err != nil {
			return fmt.Errorf("machines: pid taken: %w", err)
		}

		if taken {
			v.add("PID_TAKEN", "has already been taken", "/data/attributes/pid")
		}
	}

	if isReservedWord(process.PID) {
		v.add("PID_NOT_ALLOWED", "is reserved", "/data/attributes/pid")
	}

	if tooLong(&process.PID, maxFingerprintLength) {
		v.add("PID_TOO_LONG", "is too long (maximum is 4096 characters)", "/data/attributes/pid")
	}

	if process.PID == "" {
		v.add("PID_MISSING", "can't be blank", "/data/attributes/pid")
	}

	if detail := domain.MetadataViolation(process.Metadata); detail != "" {
		v.add("METADATA_TOO_LONG", detail, "/data/attributes/metadata")
	}

	if id != "" {
		if err := s.validateProcessID(ctx, id, &v); err != nil {
			return err
		}
	}

	if found {
		if err := s.validateProcessLimit(ctx, account, machine, &v); err != nil {
			return err
		}
	}

	return v.err()
}

func (s Scenario) validateProcessID(ctx context.Context, id string, v *violations) error {
	if !uuidRe.MatchString(id) {
		v.add("ID_INVALID", "must be a valid UUID", "/data/id")

		return nil
	}

	exists, err := s.store.ProcessExists(ctx, uuid.MustParse(id))
	if err != nil {
		return fmt.Errorf("machines: process id: %w", err)
	}

	if exists {
		v.add("ID_CONFLICT", "must not conflict with another process", "/data/id")
	}

	return nil
}

// validateProcessLimit checks the process count against the limit of
// the policy's leasing strategy, within the overage allowance.
func (s Scenario) validateProcessLimit(
	ctx context.Context, account domain.Account, machine domain.Machine, v *violations,
) error {
	if machine.MaxProcesses() == nil {
		return nil
	}

	license, err := s.licenses.LicenseByID(ctx, account.ID, machine.LicenseID)
	if err != nil {
		return fmt.Errorf("machines: process license: %w", err)
	}

	if license.Policy.OverageStrategy == "ALWAYS_ALLOW_OVERAGE" {
		return nil
	}

	kind, scopeID, limit := kindMachine, machine.ID, machine.MaxProcesses()
	holder := kind

	switch license.Policy.ProcessLeasing() {
	case "PER_LICENSE":
		kind, scopeID, limit = kindLicense, license.ID, license.MaxProcesses()
		holder = kind
	case "PER_USER":
		kind, scopeID, limit = kindUser, license.ID, license.MaxProcesses()
		holder = kind
	}

	// The reference reads a license limit the machine may not have; with
	// none there is nothing to exceed.
	if limit == nil {
		return nil
	}

	count, err := s.store.ProcessCount(ctx, kind, scopeID, machine.OwnerID)
	if err != nil {
		return fmt.Errorf("machines: process count: %w", err)
	}

	if exceeds(license.Policy, count+1, int64(*limit)) {
		v.add("MACHINE_PROCESS_LIMIT_EXCEEDED",
			fmt.Sprintf("process count has exceeded maximum allowed for %s (%d)", holder, *limit), pointerBase)
	}

	return nil
}
