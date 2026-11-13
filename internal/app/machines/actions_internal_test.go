package machines

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// heartbeating is a machine that pinged so that its next ping was due
// "overdue" ago (negative: due in the future).
func heartbeating(policy domain.HeartbeatPolicy, overdue time.Duration) domain.Machine {
	last := testNow.Add(-policy.DurationOrDefault() - overdue)
	m := domain.Machine{Heartbeat: policy, LastHeartbeatAt: &last, CreatedAt: last}
	m.HeartbeatStatus = m.Status(testNow)

	return m
}

func TestPing(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		machine domain.Machine
		event   string
		status  string
		code    string
	}{
		{"alive", heartbeating(domain.HeartbeatPolicy{}, -time.Minute), "machine.heartbeat.ping", domain.MachineAlive, ""},
		{"first ping", domain.Machine{HeartbeatStatus: domain.MachineNotStarted}, "machine.heartbeat.ping", domain.MachineAlive, ""},
		{"dead, never revives", heartbeating(domain.HeartbeatPolicy{}, time.Minute), "", "", "MACHINE_HEARTBEAT_DEAD"},
		{"dead, NO_REVIVE", heartbeating(domain.HeartbeatPolicy{Resurrect: "NO_REVIVE"}, time.Second), "", "", "MACHINE_HEARTBEAT_DEAD"},
		{
			"dead inside the window", heartbeating(domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, 3*time.Minute),
			"machine.heartbeat.resurrected", domain.HeartbeatResurrected, "",
		},
		{
			"dead past the window", heartbeating(domain.HeartbeatPolicy{Resurrect: "5_MINUTE_REVIVE"}, 6*time.Minute),
			"", "", "MACHINE_HEARTBEAT_DEAD",
		},
		{
			"dead, always revives", heartbeating(domain.HeartbeatPolicy{Resurrect: "ALWAYS_REVIVE"}, 240*time.Hour),
			"machine.heartbeat.resurrected", domain.HeartbeatResurrected, "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			dead := testNow.Add(-time.Hour)
			test.machine.LastDeathEventSentAt = &dead
			machine := w.seedMachine(test.machine)

			got, err := w.scenario().Ping(admin(), w.account, machine.Fingerprint)

			if test.code != "" {
				wantCodes(t, err, test.code)
				wantStatus(t, err, 422)

				if len(w.updated) != 0 || len(w.events) != 0 {
					t.Errorf("a refused ping wrote: updated=%d events=%v", len(w.updated), w.events)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.HeartbeatStatus != test.status || !got.LastHeartbeatAt.Equal(testNow) {
				t.Errorf("status = %s, last = %v", got.HeartbeatStatus, got.LastHeartbeatAt)
			}

			if len(w.events) != 1 || w.events[0] != test.event {
				t.Errorf("events = %v, want %s", w.events, test.event)
			}

			if resurrected := test.event == "machine.heartbeat.resurrected"; resurrected != (got.LastDeathEventSentAt == nil) {
				t.Errorf("death marker = %v after %s", got.LastDeathEventSentAt, test.event)
			}
		})
	}
}

func TestPingAuthorization(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		role      domain.Role
		bearer    func(w *world, m domain.Machine) uuid.UUID
		protected bool
		ok        bool
	}{
		{"admin", domain.RoleAdmin, anyone, false, true},
		{"developer", domain.RoleDeveloper, anyone, false, true},
		{"sales agent", domain.RoleSalesAgent, anyone, false, false},
		{"support agent", domain.RoleSupportRole, anyone, false, false},
		{"read only", domain.RoleReadOnly, anyone, false, false},
		{"own product", domain.RoleProduct, func(w *world, _ domain.Machine) uuid.UUID { return w.license.ProductID }, false, true},
		{"foreign product", domain.RoleProduct, anyone, false, false},
		{"own license", domain.RoleLicense, func(w *world, _ domain.Machine) uuid.UUID { return w.license.ID }, false, true},
		{"machine owner", domain.RoleUser, func(_ *world, m domain.Machine) uuid.UUID { return m.OwnerID.UUID }, false, true},
		{"machine owner, protected", domain.RoleUser, func(_ *world, m domain.Machine) uuid.UUID { return m.OwnerID.UUID }, true, false},
		{"stranger", domain.RoleUser, anyone, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{
				OwnerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, LicenseProtected: test.protected,
			})

			_, err := w.scenario().Ping(as(test.role, test.bearer(w, machine)), w.account, machine.ID.String())
			if (err == nil) != test.ok {
				t.Fatalf("err = %v, want ok = %v", err, test.ok)
			}
		})
	}
}

func TestPingMissingMachine(t *testing.T) {
	t.Parallel()

	_, err := newWorld().scenario().Ping(admin(), newWorld().account, "nope")
	wantStatus(t, err, 404)
}

func TestReset(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		require bool
		status  string
	}{
		{"required, long created", true, domain.MachineDead},
		{"not required", false, domain.MachineNotStarted},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := heartbeating(domain.HeartbeatPolicy{Require: test.require}, -time.Minute)
			machine.CreatedAt = testNow.Add(-24 * time.Hour)
			machine = w.seedMachine(machine)

			got, err := w.scenario().Reset(admin(), w.account, machine.ID.String())
			if err != nil {
				t.Fatal(err)
			}

			if got.LastHeartbeatAt != nil || got.HeartbeatStatus != test.status {
				t.Errorf("last = %v, status = %s", got.LastHeartbeatAt, got.HeartbeatStatus)
			}

			if len(w.events) != 1 || w.events[0] != "machine.heartbeat.reset" || len(w.updated) != 1 {
				t.Errorf("events = %v updated = %d", w.events, len(w.updated))
			}
		})
	}
}

func TestResetAuthorization(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	for role, ok := range map[domain.Role]bool{
		domain.RoleAdmin: true, domain.RoleDeveloper: true, domain.RoleSalesAgent: true,
		domain.RoleSupportRole: false, domain.RoleReadOnly: false, domain.RoleUser: false, domain.RoleLicense: false,
	} {
		_, err := w.scenario().Reset(as(role, w.license.ID), w.account, machine.ID.String())
		if (err == nil) != ok {
			t.Errorf("%s: err = %v, want ok = %v", role, err, ok)
		}
	}

	if _, err := w.scenario().Reset(as(domain.RoleProduct, w.license.ProductID), w.account, machine.ID.String()); err != nil {
		t.Errorf("own product: %v", err)
	}
}

func ttlOf(v int32) *int32 { return &v }

func TestCheckoutParamErrors(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		params CheckoutParams
		code   string
		param  string
	}{
		{"unknown include", CheckoutParams{Include: []string{"bogus"}}, "CHECKOUT_INCLUDE_INVALID", "include"},
		{"one bad include among good", CheckoutParams{Include: []string{"components", "license.nope"}}, "CHECKOUT_INCLUDE_INVALID", "include"},
		{"ttl below an hour", CheckoutParams{TTL: ttlOf(3599), TTLSet: true}, "CHECKOUT_TTL_INVALID", "ttl"},
		{"ttl zero", CheckoutParams{TTL: ttlOf(0), TTLSet: true}, "CHECKOUT_TTL_INVALID", "ttl"},
		{"blank algorithm", CheckoutParams{Algorithm: "  "}, "CHECKOUT_ALGORITHM_INVALID", "algorithm"},
		{"algorithm without signature part", CheckoutParams{Algorithm: "base64"}, "CHECKOUT_ALGORITHM_INVALID", "algorithm"},
		{"unknown encoding", CheckoutParams{Algorithm: "rot13+ed25519"}, "CHECKOUT_ALGORITHM_INVALID", "algorithm"},
		{"unknown signature", CheckoutParams{Algorithm: "base64+nope"}, "CHECKOUT_ALGORITHM_INVALID", "algorithm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})

			_, err := w.scenario().Checkout(admin(), w.account, machine.ID.String(), func() (CheckoutParams, error) {
				return test.params, nil
			})
			wantCodes(t, err, test.code)
			wantStatus(t, err, 400)

			if len(w.updated) != 0 || len(w.events) != 0 {
				t.Errorf("a refused check-out stamped the machine: %d %v", len(w.updated), w.events)
			}
		})
	}
}

func TestCheckoutOf(t *testing.T) {
	t.Parallel()

	license := domain.License{}
	month := crypto.DefaultTTL()

	for _, test := range []struct {
		name      string
		params    CheckoutParams
		algorithm string
		encrypted bool
		ttl       *int32
		includes  []string
	}{
		{"defaults", CheckoutParams{}, "base64+ed25519", false, &month, []string{}},
		{"encrypted", CheckoutParams{Encrypt: ptr(true)}, "aes-256-gcm+ed25519", true, &month, []string{}},
		{"explicit algorithm", CheckoutParams{Algorithm: "base64+ecdsa-p256"}, "base64+ecdsa-p256", false, &month, []string{}},
		{"explicit null ttl is perpetual", CheckoutParams{TTLSet: true}, "base64+ed25519", false, nil, []string{}},
		{"custom ttl", CheckoutParams{TTL: ttlOf(3600), TTLSet: true}, "base64+ed25519", false, ttlOf(3600), []string{}},
		{
			"license.user is the owner include",
			CheckoutParams{Include: []string{"license.user", "components"}},
			"base64+ed25519", false, &month,
			[]string{"license.owner", "components"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			out, err := checkoutOf(domain.Machine{}, license, test.params, testNow)
			if err != nil {
				t.Fatal(err)
			}

			if out.Encrypted != test.encrypted || strings.HasPrefix(out.Algorithm, crypto.EncodingAES256GCM) != test.encrypted {
				t.Errorf("encrypted = %v algorithm = %s", out.Encrypted, out.Algorithm)
			}

			if out.Algorithm != test.algorithm {
				t.Errorf("algorithm = %s, want %s", out.Algorithm, test.algorithm)
			}

			switch {
			case test.ttl == nil && (out.TTL != nil || out.Expiry != nil):
				t.Errorf("perpetual file has ttl %v expiry %v", out.TTL, out.Expiry)
			case test.ttl != nil && (out.TTL == nil || *out.TTL != *test.ttl ||
				out.Expiry == nil || !out.Expiry.Equal(testNow.Add(time.Duration(*test.ttl)*time.Second))):
				t.Errorf("ttl = %v expiry = %v", out.TTL, out.Expiry)
			}

			if strings.Join(out.Includes, ",") != strings.Join(test.includes, ",") {
				t.Errorf("includes = %v, want %v", out.Includes, test.includes)
			}
		})
	}
}

func TestCheckoutStampsAndIncludes(t *testing.T) {
	t.Parallel()

	w := newWorld()
	group := uuid.New()
	w.groups[group] = domain.Group{ID: group, Name: "g"}
	owner := w.seedUser(uuid.NullUUID{}, true)
	machine := w.seedMachine(domain.Machine{
		OwnerID: uuid.NullUUID{UUID: owner.ID, Valid: true}, GroupID: uuid.NullUUID{UUID: group, Valid: true},
	})
	w.components = []domain.MachineComponent{{ID: uuid.New(), MachineID: machine.ID}, {ID: uuid.New(), MachineID: uuid.New()}}

	out, err := w.scenario().Checkout(admin(), w.account, machine.ID.String(), func() (CheckoutParams, error) {
		return CheckoutParams{Include: []string{"license.owner", "license.product", "components", "group", "owner"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if out.Machine.LastCheckOutAt == nil || !out.Machine.LastCheckOutAt.Equal(testNow) || !out.IssuedAt.Equal(testNow) {
		t.Errorf("check-out not stamped: %v", out.Machine.LastCheckOutAt)
	}

	if len(w.events) != 1 || w.events[0] != "machine.checked-out" || len(w.updated) != 1 {
		t.Errorf("events = %v updated = %d", w.events, len(w.updated))
	}

	if len(w.includeRequests) != 1 || strings.Join(w.includeRequests[0], ",") != "owner,product" {
		t.Errorf("license includes = %v", w.includeRequests)
	}

	if out.Owner == nil || out.Owner.ID != owner.ID || out.OwnerStatus != "ACTIVE" || out.Group == nil || out.Group.ID != group {
		t.Errorf("owner/group not included: %+v %+v", out.Owner, out.Group)
	}

	if len(out.Components) != 1 || out.Components[0].MachineID != machine.ID {
		t.Errorf("components = %+v", out.Components)
	}
}

func TestCheckoutNoIncludesNoLookups(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	out, err := w.scenario().Checkout(admin(), w.account, machine.ID.String(), func() (CheckoutParams, error) {
		return CheckoutParams{Include: []string{"owner", "group"}}, nil
	})
	if err != nil || out.Owner != nil || out.Group != nil || len(w.includeRequests) != 0 {
		t.Fatalf("out = %+v err = %v includes = %v", out, err, w.includeRequests)
	}
}

func TestCheckoutOrderAndAuthorization(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	// The parameters are decoded before the authorization.
	_, err := w.scenario().Checkout(as(domain.RoleReadOnly, uuid.New()), w.account, machine.ID.String(),
		func() (CheckoutParams, error) { return CheckoutParams{}, errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want the decode error", err)
	}

	for _, test := range []struct {
		name      string
		role      domain.Role
		own       bool
		protected bool
		ok        bool
	}{
		{"read only", domain.RoleReadOnly, false, false, false},
		{"support agent", domain.RoleSupportRole, false, false, true},
		{"own license", domain.RoleLicense, true, false, true},
		{"foreign license", domain.RoleLicense, false, false, false},
		{"user", domain.RoleUser, false, false, true},
		{"user, protected", domain.RoleUser, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fresh := newWorld()
			m := fresh.seedMachine(domain.Machine{LicenseProtected: test.protected})
			bearer := uuid.New()

			if test.own {
				bearer = fresh.license.ID
			}

			_, err := fresh.scenario().Checkout(as(test.role, bearer), fresh.account, m.ID.String(),
				func() (CheckoutParams, error) { return CheckoutParams{}, nil })
			if (err == nil) != test.ok {
				t.Errorf("err = %v, want ok = %v", err, test.ok)
			}
		})
	}
}

func BenchmarkCheckoutOf(b *testing.B) {
	params := CheckoutParams{Include: []string{"license.owner", "components"}}

	for b.Loop() {
		if _, err := checkoutOf(domain.Machine{}, domain.License{}, params, testNow); err != nil {
			b.Fatal(err)
		}
	}
}
