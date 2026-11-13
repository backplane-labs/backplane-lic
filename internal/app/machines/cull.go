package machines

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// cullBatch bounds the records one cull pass visits; the next pass takes
// the rest.
const cullBatch = 500

// Ref names a record across accounts.
type Ref struct {
	ID        uuid.UUID
	AccountID uuid.UUID
}

// Cull runs the heartbeat workers over the dead machines and processes
// (CullDeadMachinesWorker, CullDeadProcessesWorker): the death event once,
// then, past any resurrection window, the deactivation the policy asks for.
func (s Scenario) Cull(ctx context.Context, now time.Time) error {
	machines, err := s.store.DeadMachines(ctx, now, cullBatch)
	if err != nil {
		return fmt.Errorf("machines: dead machines: %w", err)
	}

	for _, ref := range machines {
		if err := s.cullMachine(ctx, ref, now); err != nil {
			return err
		}
	}

	processes, err := s.store.DeadProcesses(ctx, now, cullBatch)
	if err != nil {
		return fmt.Errorf("machines: dead processes: %w", err)
	}

	for _, ref := range processes {
		if err := s.cullProcess(ctx, ref, now); err != nil {
			return err
		}
	}

	return nil
}

// cullMachine is MachineHeartbeatWorker#perform for a dead machine.
func (s Scenario) cullMachine(ctx context.Context, ref Ref, now time.Time) error {
	machine, err := s.store.FindMachine(ctx, ref.AccountID, Scope{}, ref.ID.String(), now)
	if err != nil {
		return notFoundOr(err, "machine", ref.ID.String())
	}

	if !machine.RequiresHeartbeat() || machine.HeartbeatStatus != domain.MachineDead {
		return nil
	}

	return s.write(ctx, func(ctx context.Context) error {
		if machine.LastDeathEventSentAt == nil {
			machine.LastDeathEventSentAt = &now

			if err := s.store.UpdateMachine(ctx, machine); err != nil {
				return fmt.Errorf("machines: death event: %w", err)
			}

			if err := s.publish(ctx, "machine.heartbeat.dead", machine); err != nil {
				return err
			}
		}

		if !culled(machine.Heartbeat, machine.ResurrectionPeriodPassed(now)) {
			return nil
		}

		if err := s.store.DeleteMachine(ctx, ref.AccountID, machine.ID); err != nil {
			return fmt.Errorf("machines: cull: %w", err)
		}

		if err := s.store.RefreshLicenseCounters(ctx, ref.AccountID, machine.LicenseID, now); err != nil {
			return fmt.Errorf("machines: counters: %w", err)
		}

		return nil
	})
}

// cullProcess is ProcessHeartbeatWorker#perform for a dead process.
func (s Scenario) cullProcess(ctx context.Context, ref Ref, now time.Time) error {
	process, err := s.store.FindProcess(ctx, ref.AccountID, Scope{}, ref.ID, now)
	if err != nil {
		return notFoundOr(err, "process", ref.ID.String())
	}

	if process.HeartbeatStatus != domain.MachineDead {
		return nil
	}

	return s.write(ctx, func(ctx context.Context) error {
		if process.LastDeathEventSentAt == nil {
			process.LastDeathEventSentAt = &now

			if err := s.store.UpdateProcess(ctx, process); err != nil {
				return fmt.Errorf("machines: process death event: %w", err)
			}

			if err := s.publish(ctx, "process.heartbeat.dead", process); err != nil {
				return err
			}
		}

		if !culled(process.Heartbeat, process.ResurrectionPeriodPassed(now)) {
			return nil
		}

		if err := s.store.DeleteProcess(ctx, ref.AccountID, process.ID); err != nil {
			return fmt.Errorf("machines: cull process: %w", err)
		}

		return nil
	})
}

// culled answers whether a dead record is deactivated now: never while it
// may always revive or its resurrection window is open, then as the
// policy's cull strategy says.
func culled(h domain.HeartbeatPolicy, periodPassed bool) bool {
	switch {
	case h.AlwaysResurrectDead():
		return false
	case h.ResurrectDead() && !periodPassed:
		return false
	default:
		return h.DeactivateDead()
	}
}
