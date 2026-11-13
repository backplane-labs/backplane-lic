package machines

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestSpawnProcessLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		policy    func(p *domain.Policy)
		machine   func(m *domain.Machine)
		count     int64
		want      []string
		kind      string
		scopeLic  bool
		wantOwner bool
		counted   bool
	}{
		{
			name:    "no limit, no count",
			machine: func(*domain.Machine) {},
		},
		{
			name:    "per machine with room",
			machine: func(m *domain.Machine) { m.MaxProcessesOverride = ptr(int32(2)) },
			count:   1, kind: "machine", counted: true,
		},
		{
			name:    "per machine at the limit",
			machine: func(m *domain.Machine) { m.MaxProcessesOverride = ptr(int32(2)) },
			count:   2, kind: "machine", counted: true, want: []string{"MACHINE_PROCESS_LIMIT_EXCEEDED"},
		},
		{
			name:    "per machine takes the license limit when it has no override",
			machine: func(m *domain.Machine) { m.LicenseMaxProcesses = ptr(int32(1)) },
			count:   1, kind: "machine", counted: true, want: []string{"MACHINE_PROCESS_LIMIT_EXCEEDED"},
		},
		{
			name:    "zero override reads the license limit",
			machine: func(m *domain.Machine) { m.MaxProcessesOverride, m.LicenseMaxProcesses = ptr(int32(0)), ptr(int32(3)) },
			count:   2, kind: "machine", counted: true,
		},
		{
			name:    "per license counts the license",
			policy:  func(p *domain.Policy) { p.ProcessLeasingStrategy, p.MaxProcesses = "PER_LICENSE", ptr(int32(2)) },
			machine: func(m *domain.Machine) { m.LicenseMaxProcesses = ptr(int32(2)) },
			count:   2, kind: "license", scopeLic: true, counted: true, want: []string{"MACHINE_PROCESS_LIMIT_EXCEEDED"},
		},
		{
			name:    "per license ignores the machine override",
			policy:  func(p *domain.Policy) { p.ProcessLeasingStrategy, p.MaxProcesses = "PER_LICENSE", ptr(int32(5)) },
			machine: func(m *domain.Machine) { m.MaxProcessesOverride, m.LicenseMaxProcesses = ptr(int32(1)), ptr(int32(5)) },
			count:   2, kind: "license", scopeLic: true, counted: true,
		},
		{
			name:   "per user counts the owner's processes",
			policy: func(p *domain.Policy) { p.ProcessLeasingStrategy, p.MaxProcesses = "PER_USER", ptr(int32(1)) },
			machine: func(m *domain.Machine) {
				m.LicenseMaxProcesses, m.OwnerID = ptr(int32(1)), uuid.NullUUID{UUID: uuid.New(), Valid: true}
			},
			count: 1, kind: "user", scopeLic: true, wantOwner: true, counted: true, want: []string{"MACHINE_PROCESS_LIMIT_EXCEEDED"},
		},
		{
			name:    "per user of an unowned machine counts the unowned",
			policy:  func(p *domain.Policy) { p.ProcessLeasingStrategy, p.MaxProcesses = "PER_USER", ptr(int32(1)) },
			machine: func(m *domain.Machine) { m.LicenseMaxProcesses = ptr(int32(1)) },
			count:   0, kind: "user", scopeLic: true, counted: true,
		},
		{
			name:    "1.5x overage allows one more",
			policy:  func(p *domain.Policy) { p.OverageStrategy = "ALLOW_1_5X_OVERAGE" },
			machine: func(m *domain.Machine) { m.MaxProcessesOverride = ptr(int32(2)) },
			count:   2, kind: "machine", counted: true,
		},
		{
			name:    "1.5x overage refuses the next",
			policy:  func(p *domain.Policy) { p.OverageStrategy = "ALLOW_1_5X_OVERAGE" },
			machine: func(m *domain.Machine) { m.MaxProcessesOverride = ptr(int32(2)) },
			count:   3, kind: "machine", counted: true, want: []string{"MACHINE_PROCESS_LIMIT_EXCEEDED"},
		},
		{
			name:    "always allow overage skips the count",
			policy:  func(p *domain.Policy) { p.OverageStrategy = "ALWAYS_ALLOW_OVERAGE" },
			machine: func(m *domain.Machine) { m.MaxProcessesOverride = ptr(int32(1)) },
			count:   50,
		},
		{
			name:    "a license limit the policy lacks leaves nothing to exceed",
			policy:  func(p *domain.Policy) { p.ProcessLeasingStrategy = "PER_LICENSE" },
			machine: func(m *domain.Machine) { m.MaxProcessesOverride = ptr(int32(1)) },
			count:   50,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.procCount = test.count

			if test.policy != nil {
				test.policy(&w.license.Policy)
			}

			var base domain.Machine
			test.machine(&base)
			machine := w.seedMachine(base)

			_, err := w.scenario().SpawnProcess(admin(), w.account, machine.ID, ProcessInput{PID: "1234"})
			wantCodes(t, err, test.want...)

			call := w.procCountCall
			if test.counted {
				scopeID := machine.ID
				if test.scopeLic {
					scopeID = w.license.ID
				}

				if call.kind != test.kind || call.id != scopeID || call.owner != machine.OwnerID {
					t.Errorf("count call = %+v, want %s %v owner %v", call, test.kind, scopeID, machine.OwnerID)
				}
			} else if call.kind != "" {
				t.Errorf("unexpected count call %+v", call)
			}

			if (err == nil) != (len(w.insertedProcesses) == 1) {
				t.Errorf("err = %v, inserted %d", err, len(w.insertedProcesses))
			}
		})
	}
}

func TestSpawnProcessValidation(t *testing.T) {
	t.Parallel()

	known := uuid.New()

	for _, test := range []struct {
		name  string
		setup func(w *world, in *ProcessInput, machine *uuid.UUID)
		want  []string
	}{
		{"valid", func(*world, *ProcessInput, *uuid.UUID) {}, nil},
		{"blank pid", func(_ *world, in *ProcessInput, _ *uuid.UUID) { in.PID = "" }, []string{"PID_MISSING"}},
		{"reserved pid", func(_ *world, in *ProcessInput, _ *uuid.UUID) { in.PID = "action" }, []string{"PID_NOT_ALLOWED"}},
		{"too long pid", func(_ *world, in *ProcessInput, _ *uuid.UUID) { in.PID = strings.Repeat("9", 4097) }, []string{"PID_TOO_LONG"}},
		{"taken pid", func(w *world, _ *ProcessInput, _ *uuid.UUID) { w.pidTaken = true }, []string{"PID_TAKEN"}},
		{"metadata too large", func(_ *world, in *ProcessInput, _ *uuid.UUID) {
			in.Metadata = map[string]any{"k": strings.Repeat("x", 17000)}
		}, []string{"METADATA_TOO_LONG"}},
		{"invalid id", func(_ *world, in *ProcessInput, _ *uuid.UUID) { in.ID = "x" }, []string{"ID_INVALID"}},
		{"conflicting id", func(w *world, in *ProcessInput, _ *uuid.UUID) {
			w.processExists = true
			in.ID = known.String()
		}, []string{"ID_CONFLICT"}},
		{
			"unknown machine", func(_ *world, _ *ProcessInput, machine *uuid.UUID) { *machine = uuid.New() },
			[]string{"MACHINE_NOT_FOUND"},
		},
		{"unknown machine and blank pid", func(_ *world, in *ProcessInput, machine *uuid.UUID) {
			*machine = uuid.New()
			in.PID = ""
		}, []string{"MACHINE_NOT_FOUND", "PID_MISSING"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})
			in, id := ProcessInput{PID: "42"}, machine.ID
			test.setup(w, &in, &id)

			process, err := w.scenario().SpawnProcess(admin(), w.account, id, in)
			wantCodes(t, err, test.want...)

			if err != nil {
				return
			}

			if process.HeartbeatStatus != domain.MachineAlive || !process.LastHeartbeatAt.Equal(testNow) ||
				process.Metadata == nil || process.ProductID != machine.ProductID {
				t.Errorf("process = %+v", process)
			}

			if len(w.events) != 1 || w.events[0] != "process.created" {
				t.Errorf("events = %v", w.events)
			}
		})
	}
}

func TestSpawnProcessClientChosenID(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})
	id := uuid.New()

	process, err := w.scenario().SpawnProcess(admin(), w.account, machine.ID, ProcessInput{ID: id.String(), PID: "7"})
	if err != nil || process.ID != id {
		t.Fatalf("id = %v err = %v", process.ID, err)
	}
}

func TestSpawnProcessAuthorization(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		role   domain.Role
		bearer func(w *world) uuid.UUID
		ok     bool
	}{
		{domain.RoleAdmin, func(*world) uuid.UUID { return uuid.New() }, true},
		{domain.RoleSupportRole, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleReadOnly, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleLicense, func(w *world) uuid.UUID { return w.license.ID }, true},
		{domain.RoleLicense, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleUser, func(*world) uuid.UUID { return uuid.New() }, false},
	} {
		t.Run(string(test.role), func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})

			_, err := w.scenario().SpawnProcess(as(test.role, test.bearer(w)), w.account, machine.ID, ProcessInput{PID: "1"})
			if (err == nil) != test.ok {
				t.Fatalf("err = %v, want ok = %v", err, test.ok)
			}
		})
	}
}

func seedProcess(w *world, machine domain.Machine, policy domain.HeartbeatPolicy, overdue time.Duration) domain.MachineProcess {
	p := domain.MachineProcess{
		ID: uuid.New(), AccountID: w.account.ID, MachineID: machine.ID, PID: "1", Metadata: map[string]any{},
		Heartbeat: policy, LastHeartbeatAt: testNow.Add(-policy.DurationOrDefault() - overdue),
		ProductID: machine.ProductID, LicenseID: machine.LicenseID,
	}
	p.HeartbeatStatus = p.Status(testNow)
	w.processes = append(w.processes, p)

	return p
}

func TestPingProcess(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		policy  domain.HeartbeatPolicy
		overdue time.Duration
		event   string
		status  string
		code    string
	}{
		{"alive", domain.HeartbeatPolicy{}, -time.Minute, "process.heartbeat.ping", domain.MachineAlive, ""},
		{"dead, never revives", domain.HeartbeatPolicy{}, time.Minute, "", "", "PROCESS_HEARTBEAT_DEAD"},
		{
			"dead inside the window",
			domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"},
			time.Minute,
			"process.heartbeat.resurrected", domain.HeartbeatResurrected, "",
		},
		{"dead past the window", domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, 6 * time.Minute, "", "", "PROCESS_HEARTBEAT_DEAD"},
		{
			"dead, always revives",
			domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"},
			100 * time.Hour,
			"process.heartbeat.resurrected", domain.HeartbeatResurrected, "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})
			process := seedProcess(w, machine, test.policy, test.overdue)

			got, err := w.scenario().PingProcess(admin(), w.account, process.ID.String())

			if test.code != "" {
				wantCodes(t, err, test.code)
				wantStatus(t, err, 422)

				if len(w.updatedProcesses) != 0 {
					t.Error("a refused ping wrote")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.HeartbeatStatus != test.status || !got.LastHeartbeatAt.Equal(testNow) ||
				len(w.events) != 1 || w.events[0] != test.event {
				t.Errorf("status = %s events = %v", got.HeartbeatStatus, w.events)
			}
		})
	}
}

func TestProcessLookupAndLifecycle(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})
	process := seedProcess(w, machine, domain.HeartbeatPolicy{}, -time.Minute)
	s := w.scenario()

	wantStatus(t, func() error { _, err := s.ShowProcess(admin(), w.account, "not-a-uuid"); return err }(), 404)
	wantStatus(t, func() error { _, err := s.ShowProcess(admin(), w.account, uuid.NewString()); return err }(), 404)

	if got, err := s.ShowProcess(admin(), w.account, process.ID.String()); err != nil || got.ID != process.ID {
		t.Fatalf("show = %v %v", got.ID, err)
	}

	// A process of another machine is not found under this machine.
	if _, err := s.MachineProcess(admin(), w.account, machine.ID.String(), process.ID.String()); err != nil {
		t.Fatalf("own process: %v", err)
	}

	other := w.seedMachine(domain.Machine{})
	_, err := s.MachineProcess(admin(), w.account, other.ID.String(), process.ID.String())
	wantStatus(t, err, 404)

	list, err := s.MachineProcesses(admin(), w.account, machine.ID.String(), SubFilter{}, pagingDefault())
	if err != nil || list.Count != 1 {
		t.Fatalf("list = %+v %v", list, err)
	}

	// Metadata update.
	meta := map[string]any{"k": "v"}
	updated, err := s.UpdateProcess(admin(), w.account, process.ID.String(), func() (Field[map[string]any], error) {
		return Field[map[string]any]{Set: true, Value: &meta}, nil
	})
	if err != nil || updated.Metadata["k"] != "v" {
		t.Fatalf("update = %+v %v", updated, err)
	}

	big := map[string]any{"k": strings.Repeat("x", 17000)}
	_, err = s.UpdateProcess(admin(), w.account, process.ID.String(), func() (Field[map[string]any], error) {
		return Field[map[string]any]{Set: true, Value: &big}, nil
	})
	wantCodes(t, err, "METADATA_TOO_LONG")

	// Authorization precedes decoding.
	decoded := false
	_, err = s.UpdateProcess(as(domain.RoleReadOnly, uuid.New()), w.account, process.ID.String(),
		func() (Field[map[string]any], error) {
			decoded = true

			return Field[map[string]any]{}, nil
		})
	wantStatus(t, err, 403)

	if decoded {
		t.Error("the body was decoded for a forbidden update")
	}

	// Kill.
	if err := s.KillProcess(admin(), w.account, process.ID.String()); err != nil {
		t.Fatal(err)
	}

	if len(w.deletedProcesses) != 1 || w.events[len(w.events)-1] != "process.deleted" {
		t.Errorf("deleted = %v events = %v", w.deletedProcesses, w.events)
	}
}

func BenchmarkSpawnProcess(b *testing.B) {
	w := newWorld()
	machine := w.seedMachine(domain.Machine{MaxProcessesOverride: ptr(int32(1 << 30))})
	s := w.scenario()
	ctx := admin()

	for b.Loop() {
		w.processes, w.insertedProcesses, w.events = nil, nil, nil

		if _, err := s.SpawnProcess(ctx, w.account, machine.ID, ProcessInput{PID: "1"}); err != nil {
			b.Fatal(err)
		}
	}
}
