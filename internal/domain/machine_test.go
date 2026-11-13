package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func ptr[T any](v T) *T { return &v }

func TestHeartbeatPolicy(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		policy      domain.HeartbeatPolicy
		duration    time.Duration
		fromCreate  bool
		resurrect   bool
		always      bool
		lazarus     time.Duration
		deactivates bool
	}{
		{"zero value", domain.HeartbeatPolicy{}, 600 * time.Second, false, false, false, 0, true},
		{
			"custom duration from creation",
			domain.HeartbeatPolicy{Duration: ptr(int32(90)), Basis: "FROM_CREATION"},
			90 * time.Second, true, false, false, 0, true,
		},
		{"no revive", domain.HeartbeatPolicy{Resurrect: "NO_REVIVE", CullOnDeath: "KEEP_DEAD"}, 600 * time.Second, false, false, false, 0, false},
		{"always revive", domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"}, 600 * time.Second, false, true, true, 0, true},
		{"15 minutes", domain.HeartbeatPolicy{Resurrect: "15_MINUTE_REVIVE"}, 600 * time.Second, false, true, false, 15 * time.Minute, true},
		{"10 minutes", domain.HeartbeatPolicy{Resurrect: "10_MINUTE_REVIVE"}, 600 * time.Second, false, true, false, 10 * time.Minute, true},
		{"5 minutes", domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, 600 * time.Second, false, true, false, 5 * time.Minute, true},
		{"2 minutes", domain.HeartbeatPolicy{Resurrect: "2_MINUTE_REVIVE"}, 600 * time.Second, false, true, false, 2 * time.Minute, true},
		{"1 minute", domain.HeartbeatPolicy{Resurrect: "1_MINUTE_REVIVE"}, 600 * time.Second, false, true, false, time.Minute, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			p := test.policy
			if got := p.DurationOrDefault(); got != test.duration {
				t.Errorf("DurationOrDefault = %v, want %v", got, test.duration)
			}

			if got := p.FromCreation(); got != test.fromCreate {
				t.Errorf("FromCreation = %v", got)
			}

			if got := p.ResurrectDead(); got != test.resurrect {
				t.Errorf("ResurrectDead = %v", got)
			}

			if got := p.AlwaysResurrectDead(); got != test.always {
				t.Errorf("AlwaysResurrectDead = %v", got)
			}

			if got := p.LazarusTTL(); got != test.lazarus {
				t.Errorf("LazarusTTL = %v, want %v", got, test.lazarus)
			}

			if got := p.DeactivateDead(); got != test.deactivates {
				t.Errorf("DeactivateDead = %v", got)
			}
		})
	}
}

func TestPolicyHeartbeatOf(t *testing.T) {
	t.Parallel()

	policy := domain.Policy{
		RequireHeartbeat: true, HeartbeatDuration: ptr(int32(60)), HeartbeatBasis: "FROM_CREATION",
		HeartbeatResurrectionStrategy: "5_MINUTE_REVIVE", HeartbeatCullStrategy: "KEEP_DEAD",
	}

	got := policy.HeartbeatOf()
	want := domain.HeartbeatPolicy{
		Require: true, Duration: policy.HeartbeatDuration, Basis: "FROM_CREATION",
		Resurrect: "5_MINUTE_REVIVE", CullOnDeath: "KEEP_DEAD",
	}

	if got != want {
		t.Fatalf("HeartbeatOf = %+v, want %+v", got, want)
	}
}

func TestMachineStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	window := 10 * time.Minute
	pinged := func(ago time.Duration) *time.Time { return ptr(now.Add(-ago)) }

	for _, test := range []struct {
		name     string
		machine  domain.Machine
		status   string
		requires bool
		next     *time.Time
	}{
		{
			"never pinged, not required",
			domain.Machine{CreatedAt: now.Add(-time.Hour)},
			domain.MachineNotStarted, false, nil,
		},
		{
			"required, created within window",
			domain.Machine{
				CreatedAt: now.Add(-window / 2), Heartbeat: domain.HeartbeatPolicy{Require: true},
			},
			domain.MachineNotStarted, true, nil,
		},
		{
			"required, created exactly one window ago",
			domain.Machine{
				CreatedAt: now.Add(-window), Heartbeat: domain.HeartbeatPolicy{Require: true},
			},
			domain.MachineNotStarted, true, nil,
		},
		{
			"required, created past the window",
			domain.Machine{
				CreatedAt: now.Add(-window - time.Second), Heartbeat: domain.HeartbeatPolicy{Require: true},
			},
			domain.MachineDead, true, nil,
		},
		{
			"pinged recently",
			domain.Machine{LastHeartbeatAt: pinged(time.Minute)},
			domain.MachineAlive, true, ptr(now.Add(-time.Minute + window)),
		},
		{
			"next ping due exactly now",
			domain.Machine{LastHeartbeatAt: pinged(window)},
			domain.MachineAlive, true, ptr(now),
		},
		{
			"next ping missed",
			domain.Machine{LastHeartbeatAt: pinged(window + time.Second)},
			domain.MachineDead, true, ptr(now.Add(-time.Second)),
		},
		{
			"custom duration",
			domain.Machine{
				LastHeartbeatAt: pinged(2 * time.Minute),
				Heartbeat:       domain.HeartbeatPolicy{Duration: ptr(int32(60))},
			},
			domain.MachineDead, true, ptr(now.Add(-time.Minute)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.machine.Status(now); got != test.status {
				t.Errorf("Status = %s, want %s", got, test.status)
			}

			if got := test.machine.RequiresHeartbeat(); got != test.requires {
				t.Errorf("RequiresHeartbeat = %v, want %v", got, test.requires)
			}

			next := test.machine.NextHeartbeatAt()

			switch {
			case test.next == nil && next != nil:
				t.Errorf("NextHeartbeatAt = %v, want nil", next)
			case test.next != nil && (next == nil || !next.Equal(*test.next)):
				t.Errorf("NextHeartbeatAt = %v, want %v", next, test.next)
			}
		})
	}
}

func TestMachineHeartbeatFlags(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		status     string
		alive      bool
		notStarted bool
	}{
		{domain.MachineAlive, true, false},
		{domain.HeartbeatResurrected, true, false},
		{domain.MachineDead, false, false},
		{domain.MachineNotStarted, false, true},
		{"", false, false},
	} {
		t.Run(test.status, func(t *testing.T) {
			t.Parallel()

			m := domain.Machine{HeartbeatStatus: test.status}
			if m.Alive() != test.alive || m.NotStarted() != test.notStarted {
				t.Errorf("Alive=%v NotStarted=%v", m.Alive(), m.NotStarted())
			}
		})
	}
}

func TestMachineResurrectionPeriodPassed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	window := 10 * time.Minute
	// last ping so that the next ping was due 'overdue' ago.
	pinged := func(overdue time.Duration) *time.Time { return ptr(now.Add(-overdue - window)) }

	for _, test := range []struct {
		name    string
		machine domain.Machine
		want    bool
	}{
		{"always revive never passes", domain.Machine{
			LastHeartbeatAt: pinged(24 * time.Hour), Heartbeat: domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"},
		}, false},
		{"no heartbeat required", domain.Machine{}, true},
		{"strategy does not revive", domain.Machine{
			LastHeartbeatAt: pinged(time.Second), Heartbeat: domain.HeartbeatPolicy{Resurrect: "NO_REVIVE"},
		}, true},
		{"required but never pinged", domain.Machine{
			Heartbeat: domain.HeartbeatPolicy{Require: true, Resurrect: "5_MINUTE_REVIVE"},
		}, true},
		{"inside lazarus window", domain.Machine{
			LastHeartbeatAt: pinged(4 * time.Minute), Heartbeat: domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"},
		}, false},
		{"exactly at lazarus boundary", domain.Machine{
			LastHeartbeatAt: pinged(5 * time.Minute), Heartbeat: domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"},
		}, false},
		{"past lazarus window", domain.Machine{
			LastHeartbeatAt: pinged(5*time.Minute + time.Second), Heartbeat: domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"},
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.machine.ResurrectionPeriodPassed(now); got != test.want {
				t.Errorf("ResurrectionPeriodPassed = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMachineMaxProcesses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		override *int32
		license  *int32
		want     *int32
	}{
		{"none", nil, nil, nil},
		{"license limit", nil, ptr(int32(3)), ptr(int32(3))},
		{"override wins", ptr(int32(7)), ptr(int32(3)), ptr(int32(7))},
		{"zero override reads the license", ptr(int32(0)), ptr(int32(3)), ptr(int32(3))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := domain.Machine{MaxProcessesOverride: test.override, LicenseMaxProcesses: test.license}.MaxProcesses()
			if (got == nil) != (test.want == nil) || got != nil && *got != *test.want {
				t.Errorf("MaxProcesses = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPolicyWithinOverage(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		strategy string
		next     float64
		limit    float64
		want     bool
	}{
		{"ALWAYS_ALLOW_OVERAGE", 1000, 1, true},
		{"ALLOW_1_25X_OVERAGE", 5, 4, true},
		{"ALLOW_1_25X_OVERAGE", 6, 4, false},
		{"ALLOW_1_5X_OVERAGE", 6, 4, true},
		{"ALLOW_1_5X_OVERAGE", 7, 4, false},
		{"ALLOW_2X_OVERAGE", 8, 4, true},
		{"ALLOW_2X_OVERAGE", 9, 4, false},
		{"NO_OVERAGE", 5, 4, false},
		{"", 5, 4, false},
		{"UNKNOWN", 5, 4, false},
	} {
		t.Run(test.strategy, func(t *testing.T) {
			t.Parallel()

			if got := (domain.Policy{OverageStrategy: test.strategy}).WithinOverage(test.next, test.limit); got != test.want {
				t.Errorf("WithinOverage(%v, %v) = %v, want %v", test.next, test.limit, got, test.want)
			}
		})
	}
}

func TestPolicyStrategies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		policy domain.Policy
		lease  bool
		unique string
		proc   string
		comp   string
	}{
		{"defaults", domain.Policy{}, false, "UNIQUE_PER_LICENSE", "PER_MACHINE", "UNIQUE_PER_MACHINE"},
		{
			"explicit",
			domain.Policy{
				MachineLeasingStrategy: "PER_USER", MachineUniquenessStrategy: "UNIQUE_PER_ACCOUNT",
				ProcessLeasingStrategy: "PER_LICENSE", ComponentUniquenessStrategy: "UNIQUE_PER_PRODUCT",
			},
			true, "UNIQUE_PER_ACCOUNT", "PER_LICENSE", "UNIQUE_PER_PRODUCT",
		},
		{"per license leasing", domain.Policy{MachineLeasingStrategy: "PER_LICENSE"}, false, "UNIQUE_PER_LICENSE", "PER_MACHINE", "UNIQUE_PER_MACHINE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			p := test.policy
			if p.MachineLeasePerUser() != test.lease || p.MachineUniqueness() != test.unique ||
				p.ProcessLeasing() != test.proc || p.ComponentUniqueness() != test.comp {
				t.Errorf("got %v %s %s %s", p.MachineLeasePerUser(), p.MachineUniqueness(), p.ProcessLeasing(), p.ComponentUniqueness())
			}
		})
	}
}

func TestMachineProcess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	window := 10 * time.Minute

	process := func(overdue time.Duration, resurrect string) domain.MachineProcess {
		return domain.MachineProcess{
			ID: uuid.New(), LastHeartbeatAt: now.Add(-overdue - window),
			Heartbeat: domain.HeartbeatPolicy{Resurrect: resurrect},
		}
	}

	for _, test := range []struct {
		name    string
		process domain.MachineProcess
		status  string
		passed  bool
	}{
		{"alive", process(-time.Minute, ""), domain.MachineAlive, true},
		{"due exactly now", process(0, ""), domain.MachineAlive, true},
		{"dead", process(time.Second, ""), domain.MachineDead, true},
		{"always revive", process(time.Hour, "ALWAYS_REVIVE"), domain.MachineDead, false},
		{"inside lazarus window", process(time.Minute, "2_MINUTE_REVIVE"), domain.MachineDead, false},
		{"at lazarus boundary", process(2*time.Minute, "2_MINUTE_REVIVE"), domain.MachineDead, false},
		{"past lazarus window", process(2*time.Minute+time.Second, "2_MINUTE_REVIVE"), domain.MachineDead, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.process.Status(now); got != test.status {
				t.Errorf("Status = %s, want %s", got, test.status)
			}

			if got := test.process.ResurrectionPeriodPassed(now); got != test.passed {
				t.Errorf("ResurrectionPeriodPassed = %v, want %v", got, test.passed)
			}

			if got, want := test.process.NextHeartbeatAt(), test.process.LastHeartbeatAt.Add(window); !got.Equal(want) {
				t.Errorf("NextHeartbeatAt = %v, want %v", got, want)
			}
		})
	}
}

func TestOverriddenAndLicenseLimits(t *testing.T) {
	t.Parallel()

	if domain.Overridden[int32](nil) || domain.Overridden(ptr(int32(0))) || domain.Overridden(ptr(int64(0))) {
		t.Error("nil and zero overrides must not count")
	}

	if !domain.Overridden(ptr(int32(1))) || !domain.Overridden(ptr(int64(-1))) {
		t.Error("non-zero override must count")
	}

	policy := domain.Policy{
		MaxMachines: ptr(int32(1)), MaxProcesses: ptr(int32(2)), MaxUsers: ptr(int32(3)), MaxCores: ptr(int32(4)),
		MaxMemory: ptr(int64(5)), MaxDisk: ptr(int64(6)), MaxUses: ptr(int32(7)),
	}

	for _, test := range []struct {
		name    string
		license domain.License
		want    [7]int64
	}{
		{"policy limits", domain.License{Policy: policy}, [7]int64{1, 2, 3, 4, 5, 6, 7}},
		{
			"overrides",
			domain.License{
				Policy: policy, MaxMachinesOverride: ptr(int32(10)), MaxProcessesOverride: ptr(int32(20)),
				MaxUsersOverride: ptr(int32(30)), MaxCoresOverride: ptr(int32(40)), MaxMemoryOverride: ptr(int64(50)),
				MaxDiskOverride: ptr(int64(60)), MaxUsesOverride: ptr(int32(70)),
			},
			[7]int64{10, 20, 30, 40, 50, 60, 70},
		},
		{
			"zero overrides read the policy",
			domain.License{
				Policy: policy, MaxMachinesOverride: ptr(int32(0)), MaxProcessesOverride: ptr(int32(0)),
				MaxUsersOverride: ptr(int32(0)), MaxCoresOverride: ptr(int32(0)), MaxMemoryOverride: ptr(int64(0)),
				MaxDiskOverride: ptr(int64(0)), MaxUsesOverride: ptr(int32(0)),
			},
			[7]int64{1, 2, 3, 4, 5, 6, 7},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			l := test.license
			got := [7]int64{
				int64(*l.MaxMachines()), int64(*l.MaxProcesses()), int64(*l.MaxUsers()), int64(*l.MaxCores()),
				*l.MaxMemory(), *l.MaxDisk(), int64(*l.MaxUses()),
			}

			if got != test.want {
				t.Errorf("limits = %v, want %v", got, test.want)
			}
		})
	}

	if (domain.License{}).MaxMachines() != nil || (domain.License{}).MaxMemory() != nil {
		t.Error("unset limits must read nil")
	}
}

func BenchmarkMachineStatus(b *testing.B) {
	now := time.Now()
	m := domain.Machine{LastHeartbeatAt: ptr(now.Add(-time.Minute))}

	for b.Loop() {
		_ = m.Status(now)
	}
}

func BenchmarkPolicyWithinOverage(b *testing.B) {
	p := domain.Policy{OverageStrategy: "ALLOW_1_5X_OVERAGE"}

	for b.Loop() {
		_ = p.WithinOverage(5, 4)
	}
}
