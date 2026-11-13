package machines

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestCreateLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		policy func(p *domain.Policy)
		lic    func(l *domain.License)
		totals Totals
		owner  bool
		in     func(in *Input)
		want   []string
	}{
		{
			name:   "first activation passes when the count is 0",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(1)) },
		},
		{
			name:   "first activation passes even under a zero limit",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(0)) },
		},
		{
			name:   "limit reached, no overage",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(1)) },
			lic:    func(l *domain.License) { l.MachinesCount = 1 },
			want:   []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "room left",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(2)) },
			lic:    func(l *domain.License) { l.MachinesCount = 1 },
		},
		{
			name:   "1.25x allows 5 of 4",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(4)), "ALLOW_1_25X_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 4 },
		},
		{
			name:   "1.25x refuses 6 of 4",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(4)), "ALLOW_1_25X_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 5 },
			want:   []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "1.5x allows 6 of 4",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(4)), "ALLOW_1_5X_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 5 },
		},
		{
			name:   "1.5x refuses 7 of 4",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(4)), "ALLOW_1_5X_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 6 },
			want:   []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "2x allows 8 of 4",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(4)), "ALLOW_2X_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 7 },
		},
		{
			name:   "2x refuses 9 of 4",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(4)), "ALLOW_2X_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 8 },
			want:   []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "always allow overage",
			policy: func(p *domain.Policy) { p.MaxMachines, p.OverageStrategy = ptr(int32(1)), "ALWAYS_ALLOW_OVERAGE" },
			lic:    func(l *domain.License) { l.MachinesCount = 100 },
		},
		{
			name:   "override beats the policy",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(1)) },
			lic:    func(l *domain.License) { l.MaxMachinesOverride, l.MachinesCount = ptr(int32(2)), 1 },
		},
		{
			name:   "zero override reads the policy",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(1)) },
			lic:    func(l *domain.License) { l.MaxMachinesOverride, l.MachinesCount = ptr(int32(0)), 1 },
			want:   []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "per user counts the owner's machines, not the license counter",
			policy: func(p *domain.Policy) { p.MaxMachines, p.MachineLeasingStrategy = ptr(int32(1)), "PER_USER" },
			lic:    func(l *domain.License) { l.MachinesCount = 0 },
			totals: Totals{Count: 1}, owner: true,
			want: []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "per user ignores other users' machines",
			policy: func(p *domain.Policy) { p.MaxMachines, p.MachineLeasingStrategy = ptr(int32(1)), "PER_USER" },
			lic:    func(l *domain.License) { l.MachinesCount = 5 },
			totals: Totals{Count: 0}, owner: true,
		},
		{
			name:   "per user without an owner counts the unowned",
			policy: func(p *domain.Policy) { p.MaxMachines, p.MachineLeasingStrategy = ptr(int32(1)), "PER_USER" },
			totals: Totals{Count: 1},
			want:   []string{"MACHINE_LIMIT_EXCEEDED"},
		},
		{
			name:   "cores within the limit",
			policy: func(p *domain.Policy) { p.MaxCores = ptr(int32(4)) },
			totals: Totals{Cores: 2},
			in:     func(in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(2))} },
		},
		{
			name:   "cores past the limit",
			policy: func(p *domain.Policy) { p.MaxCores = ptr(int32(4)) },
			totals: Totals{Cores: 2},
			in:     func(in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(3))} },
			want:   []string{"MACHINE_CORE_LIMIT_EXCEEDED"},
		},
		{
			name:   "cores under 1.5x overage",
			policy: func(p *domain.Policy) { p.MaxCores, p.OverageStrategy = ptr(int32(4)), "ALLOW_1_5X_OVERAGE" },
			totals: Totals{Cores: 2},
			in:     func(in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(4))} },
		},
		{
			name:   "cores past 1.5x overage",
			policy: func(p *domain.Policy) { p.MaxCores, p.OverageStrategy = ptr(int32(4)), "ALLOW_1_5X_OVERAGE" },
			totals: Totals{Cores: 2},
			in:     func(in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(5))} },
			want:   []string{"MACHINE_CORE_LIMIT_EXCEEDED"},
		},
		{
			name:   "memory at the limit",
			policy: func(p *domain.Policy) { p.MaxMemory = ptr(int64(100)) },
			totals: Totals{Memory: 90},
			in:     func(in *Input) { in.Memory = Field[int64]{Set: true, Value: ptr(int64(10))} },
		},
		{
			name:   "memory past the limit",
			policy: func(p *domain.Policy) { p.MaxMemory = ptr(int64(100)) },
			totals: Totals{Memory: 90},
			in:     func(in *Input) { in.Memory = Field[int64]{Set: true, Value: ptr(int64(11))} },
			want:   []string{"MACHINE_MEMORY_LIMIT_EXCEEDED"},
		},
		{
			name:   "disk past the limit",
			policy: func(p *domain.Policy) { p.MaxDisk = ptr(int64(100)) },
			totals: Totals{Disk: 100},
			in:     func(in *Input) { in.Disk = Field[int64]{Set: true, Value: ptr(int64(1))} },
			want:   []string{"MACHINE_DISK_LIMIT_EXCEEDED"},
		},
		{
			name:   "resources are checked on the first activation too",
			policy: func(p *domain.Policy) { p.MaxCores = ptr(int32(1)) },
			in:     func(in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(2))} },
			want:   []string{"MACHINE_CORE_LIMIT_EXCEEDED"},
		},
		{
			name: "every limit reported in order",
			policy: func(p *domain.Policy) {
				p.MaxMachines, p.MaxCores, p.MaxMemory, p.MaxDisk = ptr(int32(1)), ptr(int32(1)), ptr(int64(1)), ptr(int64(1))
			},
			lic: func(l *domain.License) { l.MachinesCount = 1 },
			in: func(in *Input) {
				in.Cores = Field[int64]{Set: true, Value: ptr(int64(2))}
				in.Memory = Field[int64]{Set: true, Value: ptr(int64(2))}
				in.Disk = Field[int64]{Set: true, Value: ptr(int64(2))}
			},
			want: []string{
				"MACHINE_LIMIT_EXCEEDED", "MACHINE_CORE_LIMIT_EXCEEDED",
				"MACHINE_MEMORY_LIMIT_EXCEEDED", "MACHINE_DISK_LIMIT_EXCEEDED",
			},
		},
		{
			name: "always allow skips the totals entirely",
			policy: func(p *domain.Policy) {
				p.MaxMachines, p.MaxCores, p.OverageStrategy = ptr(int32(1)), ptr(int32(1)), "ALWAYS_ALLOW_OVERAGE"
			},
			totals: Totals{Cores: 50},
			in:     func(in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(50))} },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.totals = test.totals

			if test.policy != nil {
				test.policy(&w.license.Policy)
			}

			if test.lic != nil {
				test.lic(&w.license)
			}

			in := Input{Fingerprint: "fp-new", LicenseID: w.license.ID}

			if test.owner {
				in.Owner = uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{}, true).ID, Valid: true}
			}

			if test.in != nil {
				test.in(&in)
			}

			_, err := w.scenario().Create(admin(), w.account, in)
			wantCodes(t, err, test.want...)

			if w.license.Policy.OverageStrategy == "ALWAYS_ALLOW_OVERAGE" && len(w.totalsCalls) != 0 {
				t.Errorf("totals were read under ALWAYS_ALLOW_OVERAGE")
			}

			if w.license.Policy.MachineLeasePerUser() && len(w.totalsCalls) == 1 {
				call := w.totalsCalls[0]
				if !call.perUser || call.owner.Valid != test.owner {
					t.Errorf("totals call = %+v, want per user with owner %v", call, test.owner)
				}
			}
		})
	}
}

func TestLimitDetailNamesTheHolder(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		leasing string
		holder  string
	}{
		{"", "for license (1)"},
		{"PER_LICENSE", "for license (1)"},
		{"PER_USER", "for user (1)"},
	} {
		t.Run(test.leasing, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.license.Policy.MaxMachines, w.license.Policy.MachineLeasingStrategy = ptr(int32(1)), test.leasing
			w.license.MachinesCount = 1
			w.totals = Totals{Count: 1}

			_, err := w.scenario().Create(admin(), w.account, Input{Fingerprint: "fp-new", LicenseID: w.license.ID})
			wantCodes(t, err, "MACHINE_LIMIT_EXCEEDED")

			if !strings.Contains(err.Error(), test.holder) {
				t.Errorf("detail %q lacks %q", err.Error(), test.holder)
			}
		})
	}
}

func TestExceeds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		strategy string
		next     int64
		limit    int64
		want     bool
	}{
		{"", 1, 1, false},
		{"", 2, 1, true},
		{"ALLOW_1_25X_OVERAGE", 5, 4, false},
		{"ALLOW_1_25X_OVERAGE", 6, 4, true},
		{"ALWAYS_ALLOW_OVERAGE", 1 << 40, 1, false},
	} {
		t.Run(test.strategy, func(t *testing.T) {
			t.Parallel()

			if got := exceeds(domain.Policy{OverageStrategy: test.strategy}, test.next, test.limit); got != test.want {
				t.Errorf("exceeds(%d, %d) = %v, want %v", test.next, test.limit, got, test.want)
			}
		})
	}
}

func BenchmarkViolations(b *testing.B) {
	for b.Loop() {
		var v violations

		for _, code := range []string{"A", "B", "C", "D", "A", "B"} {
			v.add(code, "detail", "/data/attributes/"+code)
		}

		if err := v.err(); err == nil {
			b.Fatal("expected violations")
		}
	}
}

func TestViolationsGroupingAndDedup(t *testing.T) {
	t.Parallel()

	var v violations

	v.add("A", "x", "/data/attributes/a")
	v.add("B", "x", "/data/attributes/b")
	v.add("A2", "x", "/data/attributes/a")
	v.add("A", "dup", "/data/attributes/a")
	v.add("B", "dup", "/data/attributes/b")

	wantCodes(t, v.err(), "A", "A2", "B")

	nested := violations{codePrefix: "COMPONENTS_"}
	nested.add("NAME_MISSING", "x", "/p")
	wantCodes(t, nested.err(), "COMPONENTS_NAME_MISSING")

	if (&violations{}).err() != nil {
		t.Error("no violations must be no error")
	}
}
