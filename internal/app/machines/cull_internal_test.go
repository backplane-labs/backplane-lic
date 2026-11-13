package machines

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestCulled(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		policy domain.HeartbeatPolicy
		passed bool
		want   bool
	}{
		{"unset policy culls", domain.HeartbeatPolicy{}, false, true},
		{"unset policy culls after the period too", domain.HeartbeatPolicy{}, true, true},
		{"deactivate dead", domain.HeartbeatPolicy{CullOnDeath: "DEACTIVATE_DEAD", Resurrect: "NO_REVIVE"}, false, true},
		{"keep dead", domain.HeartbeatPolicy{CullOnDeath: "KEEP_DEAD"}, true, false},
		{"always revive is never culled", domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"}, true, false},
		{
			"always revive and deactivate",
			domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE", CullOnDeath: "DEACTIVATE_DEAD"},
			true, false,
		},
		{"window open", domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, false, false},
		{"window passed", domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, true, true},
		{"window passed but kept", domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE", CullOnDeath: "KEEP_DEAD"}, true, false},
		{"window open and kept", domain.HeartbeatPolicy{Resurrect: "1_MINUTE_REVIVE", CullOnDeath: "KEEP_DEAD"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := culled(test.policy, test.passed); got != test.want {
				t.Errorf("culled = %v, want %v", got, test.want)
			}
		})
	}
}

func refOf(w *world, id uuid.UUID) Ref { return Ref{ID: id, AccountID: w.account.ID} }

func TestCullMachines(t *testing.T) {
	t.Parallel()

	sent := testNow.Add(-time.Hour)

	for _, test := range []struct {
		name       string
		machine    domain.Machine
		wantEvents []string
		wantUpdate int
		wantDelete bool
	}{
		{
			"first death is announced and culled", heartbeating(domain.HeartbeatPolicy{}, time.Minute),
			[]string{"machine.heartbeat.dead"},
			1, true,
		},
		{
			"death already announced, culled again without a second event",
			withDeathSent(heartbeating(domain.HeartbeatPolicy{}, time.Minute), sent), nil, 0, true,
		},
		{
			"keep dead: announced, kept", heartbeating(domain.HeartbeatPolicy{CullOnDeath: "KEEP_DEAD"}, time.Minute),
			[]string{"machine.heartbeat.dead"},
			1, false,
		},
		{
			"keep dead and already announced: nothing", withDeathSent(
				heartbeating(domain.HeartbeatPolicy{CullOnDeath: "KEEP_DEAD"}, time.Minute), sent), nil, 0, false,
		},
		{
			"resurrection window open: announced, kept",
			heartbeating(domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, 2*time.Minute),
			[]string{"machine.heartbeat.dead"},
			1, false,
		},
		{
			"resurrection window passed: announced and culled",
			heartbeating(domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, 6*time.Minute),
			[]string{"machine.heartbeat.dead"},
			1, true,
		},
		{
			"always revive: announced, never culled",
			heartbeating(domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"}, 100*time.Hour),
			[]string{"machine.heartbeat.dead"},
			1, false,
		},
		{"alive: untouched", heartbeating(domain.HeartbeatPolicy{}, -time.Minute), nil, 0, false},
		{"never pinged and not required: untouched", domain.Machine{HeartbeatStatus: domain.MachineDead}, nil, 0, false},
		{
			"status not dead at load: untouched",
			func() domain.Machine {
				m := heartbeating(domain.HeartbeatPolicy{}, time.Minute)
				m.HeartbeatStatus = domain.MachineAlive

				return m
			}(), nil, 0, false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(test.machine)
			w.deadMachines = []Ref{refOf(w, machine.ID)}

			if err := w.scenario().Cull(t.Context(), testNow); err != nil {
				t.Fatal(err)
			}

			if len(w.events) != len(test.wantEvents) || len(w.events) > 0 && w.events[0] != test.wantEvents[0] {
				t.Errorf("events = %v, want %v", w.events, test.wantEvents)
			}

			if len(w.updated) != test.wantUpdate {
				t.Errorf("updates = %d, want %d", len(w.updated), test.wantUpdate)
			}

			if test.wantUpdate > 0 && (w.updated[0].LastDeathEventSentAt == nil || !w.updated[0].LastDeathEventSentAt.Equal(testNow)) {
				t.Errorf("death marker = %v", w.updated[0].LastDeathEventSentAt)
			}

			if deleted := len(w.deletedMachines) == 1; deleted != test.wantDelete {
				t.Errorf("deleted = %v, want %v", w.deletedMachines, test.wantDelete)
			}

			if test.wantDelete && w.refreshed != 1 {
				t.Errorf("license counters refreshed %d times", w.refreshed)
			}
		})
	}
}

func withDeathSent(m domain.Machine, at time.Time) domain.Machine {
	m.LastDeathEventSentAt = &at

	return m
}

func TestCullAnnouncesDeathOnce(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(heartbeating(domain.HeartbeatPolicy{CullOnDeath: "KEEP_DEAD"}, time.Minute))
	w.deadMachines = []Ref{refOf(w, machine.ID)}
	s := w.scenario()

	for range 3 {
		if err := s.Cull(t.Context(), testNow); err != nil {
			t.Fatal(err)
		}
	}

	if len(w.events) != 1 {
		t.Fatalf("death announced %d times: %v", len(w.events), w.events)
	}
}

func TestCullProcesses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		policy     domain.HeartbeatPolicy
		overdue    time.Duration
		sent       bool
		alive      bool
		wantEvents int
		wantDelete bool
	}{
		{"dead and culled", domain.HeartbeatPolicy{}, time.Minute, false, false, 1, true},
		{"announced earlier, culled silently", domain.HeartbeatPolicy{}, time.Minute, true, false, 0, true},
		{"keep dead", domain.HeartbeatPolicy{CullOnDeath: "KEEP_DEAD"}, time.Minute, false, false, 1, false},
		{"window open", domain.HeartbeatPolicy{Resurrect: "2_MINUTE_REVIVE"}, time.Minute, false, false, 1, false},
		{"window passed", domain.HeartbeatPolicy{Resurrect: "2_MINUTE_REVIVE"}, 3 * time.Minute, false, false, 1, true},
		{"always revive", domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"}, time.Hour, false, false, 1, false},
		{"alive", domain.HeartbeatPolicy{}, -time.Minute, false, true, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})
			process := domain.MachineProcess{
				ID: uuid.New(), AccountID: w.account.ID, MachineID: machine.ID, Heartbeat: test.policy,
				LastHeartbeatAt: testNow.Add(-test.policy.DurationOrDefault() - test.overdue),
			}
			process.HeartbeatStatus = process.Status(testNow)

			if test.sent {
				at := testNow.Add(-time.Hour)
				process.LastDeathEventSentAt = &at
			}

			w.processes = []domain.MachineProcess{process}
			w.deadProcesses = []Ref{refOf(w, process.ID)}

			if err := w.scenario().Cull(t.Context(), testNow); err != nil {
				t.Fatal(err)
			}

			if len(w.events) != test.wantEvents || test.wantEvents > 0 && w.events[0] != "process.heartbeat.dead" {
				t.Errorf("events = %v", w.events)
			}

			if deleted := len(w.deletedProcesses) == 1; deleted != test.wantDelete {
				t.Errorf("deleted = %v, want %v", w.deletedProcesses, test.wantDelete)
			}
		})
	}
}

func TestCullPublishFailureStopsThePass(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(heartbeating(domain.HeartbeatPolicy{}, time.Minute))
	w.deadMachines = []Ref{refOf(w, machine.ID)}
	w.failPublish = errBoom

	if err := w.scenario().Cull(t.Context(), testNow); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want the publish failure", err)
	}
}
