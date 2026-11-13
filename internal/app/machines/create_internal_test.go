package machines

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestCreateSucceeds(t *testing.T) {
	t.Parallel()

	w := newWorld()
	group := uuid.New()
	w.groups[group] = domain.Group{ID: group}
	w.license.GroupID = uuid.NullUUID{UUID: group, Valid: true}
	w.license.Policy.HeartbeatBasis = "FROM_CREATION"
	w.license.Policy.HeartbeatDuration = ptr(int32(60))

	machine, err := w.scenario().Create(admin(), w.account, Input{
		Fingerprint: "fp-new", LicenseID: w.license.ID, Name: Field[string]{Set: true, Value: ptr("laptop")},
		Cores: Field[int64]{Set: true, Value: ptr(int64(8))},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(w.inserted) != 1 || w.inserted[0].ID != machine.ID {
		t.Fatalf("inserted = %+v", w.inserted)
	}

	if machine.PolicyID != w.license.PolicyID || machine.ProductID != w.license.ProductID {
		t.Errorf("policy/product not copied from the license: %+v", machine)
	}

	if machine.GroupID != w.license.GroupID {
		t.Errorf("group = %v, want the license's", machine.GroupID)
	}

	if machine.Cores == nil || *machine.Cores != 8 || machine.Name == nil || *machine.Name != "laptop" {
		t.Errorf("attributes lost: %+v", machine)
	}

	if machine.LastHeartbeatAt == nil || !machine.LastHeartbeatAt.Equal(testNow) {
		t.Errorf("FROM_CREATION heartbeat not started: %v", machine.LastHeartbeatAt)
	}

	if machine.HeartbeatStatus != domain.MachineAlive {
		t.Errorf("status = %s", machine.HeartbeatStatus)
	}

	if w.locked != 1 || w.refreshed != 1 || len(w.events) != 1 || w.events[0] != "machine.created" {
		t.Errorf("locked=%d refreshed=%d events=%v", w.locked, w.refreshed, w.events)
	}
}

func TestCreateClientChosenID(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id := uuid.New()

	machine, err := w.scenario().Create(admin(), w.account, Input{
		ID: id.String(), Fingerprint: "fp-new", LicenseID: w.license.ID,
	})
	if err != nil || machine.ID != id {
		t.Fatalf("id = %v, err = %v", machine.ID, err)
	}
}

func TestCreateOwnerInheritsGroup(t *testing.T) {
	t.Parallel()

	w := newWorld()
	group := uuid.New()
	w.groups[group] = domain.Group{ID: group}
	owner := w.seedUser(uuid.NullUUID{UUID: group, Valid: true}, true)

	machine, err := w.scenario().Create(admin(), w.account, Input{
		Fingerprint: "fp-new", LicenseID: w.license.ID, Owner: uuid.NullUUID{UUID: owner.ID, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	if machine.GroupID.UUID != group {
		t.Fatalf("group = %v, want the owner's %v", machine.GroupID, group)
	}
}

func TestCreateValidation(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 256)
	known := uuid.New()
	groupID := uuid.New()

	for _, test := range []struct {
		name  string
		setup func(w *world, in *Input)
		want  []string
	}{
		{
			"license missing", func(w *world, _ *Input) { w.licenseMissing = true },
			[]string{"LICENSE_NOT_FOUND", "POLICY_NOT_FOUND"},
		},
		{
			"license id unknown", func(_ *world, in *Input) { in.LicenseID = uuid.New() },
			[]string{"LICENSE_NOT_FOUND", "POLICY_NOT_FOUND"},
		},
		{"blank fingerprint", func(_ *world, in *Input) { in.Fingerprint = "" }, []string{"FINGERPRINT_MISSING"}},
		{"reserved action", func(_ *world, in *Input) { in.Fingerprint = "action" }, []string{"FINGERPRINT_NOT_ALLOWED"}},
		{"reserved actions", func(_ *world, in *Input) { in.Fingerprint = "actions" }, []string{"FINGERPRINT_NOT_ALLOWED"}},
		{
			"too long", func(_ *world, in *Input) { in.Fingerprint = strings.Repeat("a", 4097) },
			[]string{"FINGERPRINT_TOO_LONG"},
		},
		{"exactly the maximum", func(_ *world, in *Input) { in.Fingerprint = strings.Repeat("a", 4096) }, nil},
		{
			"taken in license is reported once", func(w *world, _ *Input) { w.taken["license:fp-new"] = true },
			[]string{"FINGERPRINT_TAKEN"},
		},
		{"taken elsewhere only", func(w *world, _ *Input) { w.taken["account:fp-new"] = true }, nil},
		{"fingerprint is another machine's uuid", func(w *world, in *Input) {
			w.existing[known] = true
			in.Fingerprint = known.String()
		}, []string{"FINGERPRINT_CONFLICT"}},
		{"fingerprint uuid unused", func(_ *world, in *Input) { in.Fingerprint = known.String() }, nil},
		{"invalid id", func(_ *world, in *Input) { in.ID = "nope" }, []string{"ID_INVALID"}},
		{"conflicting id", func(w *world, in *Input) {
			w.existing[known] = true
			in.ID = known.String()
		}, []string{"ID_CONFLICT"}},
		{
			"owner missing", func(_ *world, in *Input) { in.Owner = uuid.NullUUID{UUID: uuid.New(), Valid: true} },
			[]string{"OWNER_NOT_FOUND"},
		},
		{"owner not a license user", func(w *world, in *Input) {
			in.Owner = uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{}, false).ID, Valid: true}
		}, []string{"OWNER_INVALID"}},
		{"owner is a license user", func(w *world, in *Input) {
			in.Owner = uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{}, true).ID, Valid: true}
		}, nil},
		{
			"group missing", func(_ *world, in *Input) { in.Group = uuid.NullUUID{UUID: uuid.New(), Valid: true} },
			[]string{"GROUP_NOT_FOUND"},
		},
		{"group full", func(w *world, in *Input) {
			w.groups[groupID] = domain.Group{ID: groupID, MaxMachines: ptr(2)}
			w.groupCount = 2
			in.Group = uuid.NullUUID{UUID: groupID, Valid: true}
		}, []string{"GROUP_MACHINE_LIMIT_EXCEEDED"}},
		{"group has room", func(w *world, in *Input) {
			w.groups[groupID] = domain.Group{ID: groupID, MaxMachines: ptr(2)}
			w.groupCount = 1
			in.Group = uuid.NullUUID{UUID: groupID, Valid: true}
		}, nil},
		{"group without limit", func(w *world, in *Input) {
			w.groups[groupID] = domain.Group{ID: groupID}
			w.groupCount = 100
			in.Group = uuid.NullUUID{UUID: groupID, Valid: true}
		}, nil},
		{
			"zero cores", func(_ *world, in *Input) { in.Cores = Field[int64]{Set: true, Value: ptr(int64(0))} },
			[]string{"CORES_INVALID"},
		},
		{"cores past int32", func(_ *world, in *Input) {
			in.Cores = Field[int64]{Set: true, Value: ptr(int64(math.MaxInt32) + 1)}
		}, []string{"CORES_INVALID"}},
		{"null cores", func(_ *world, in *Input) { in.Cores = Field[int64]{Set: true} }, nil},
		{
			"zero memory", func(_ *world, in *Input) { in.Memory = Field[int64]{Set: true, Value: ptr(int64(0))} },
			[]string{"MEMORY_INVALID"},
		},
		{
			"zero disk", func(_ *world, in *Input) { in.Disk = Field[int64]{Set: true, Value: ptr(int64(0))} },
			[]string{"DISK_INVALID"},
		},
		{"texts too long", func(_ *world, in *Input) {
			in.Name = Field[string]{Set: true, Value: &long}
			in.Platform = Field[string]{Set: true, Value: &long}
			in.Hostname = Field[string]{Set: true, Value: &long}
			in.IP = Field[string]{Set: true, Value: &long}
		}, []string{"IP_TOO_LONG", "HOSTNAME_TOO_LONG", "PLATFORM_TOO_LONG", "NAME_TOO_LONG"}},
		{"metadata too large", func(_ *world, in *Input) {
			big := map[string]any{"k": strings.Repeat("x", 17000)}
			in.Metadata = Field[map[string]any]{Set: true, Value: &big}
		}, []string{"METADATA_TOO_LONG"}},
		{"order: relationships, then fingerprint", func(w *world, in *Input) {
			w.licenseMissing = true
			in.Fingerprint = ""
		}, []string{"LICENSE_NOT_FOUND", "POLICY_NOT_FOUND", "FINGERPRINT_MISSING"}},
		{"order: owner before attributes", func(_ *world, in *Input) {
			in.Owner = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			in.Disk = Field[int64]{Set: true, Value: ptr(int64(0))}
		}, []string{"OWNER_NOT_FOUND", "DISK_INVALID"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			in := Input{Fingerprint: "fp-new", LicenseID: w.license.ID}
			test.setup(w, &in)

			_, err := w.scenario().Create(admin(), w.account, in)
			wantCodes(t, err, test.want...)

			if test.want != nil && len(w.inserted) != 0 {
				t.Errorf("a refused activation inserted %d machines", len(w.inserted))
			}
		})
	}
}

func TestCreateUniquenessStrategies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		strategy  string
		takenIn   string
		wantTaken bool
		scope     string
	}{
		{"", "license", true, "license"},
		{"", "account", false, "license"},
		{"UNIQUE_PER_LICENSE", "license", true, "license"},
		{"UNIQUE_PER_LICENSE", "policy", false, "license"},
		{"UNIQUE_PER_POLICY", "policy", true, "policy"},
		{"UNIQUE_PER_POLICY", "product", false, "policy"},
		{"UNIQUE_PER_PRODUCT", "product", true, "product"},
		{"UNIQUE_PER_PRODUCT", "account", false, "product"},
		{"UNIQUE_PER_ACCOUNT", "account", true, "account"},
		// The license-level check always applies, whatever the strategy.
		{"UNIQUE_PER_ACCOUNT", "license", true, "account"},
		{"UNIQUE_PER_ACCOUNT", "policy", false, "account"},
	} {
		t.Run(test.strategy+"/"+test.takenIn, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.license.Policy.MachineUniquenessStrategy = test.strategy
			w.taken[test.takenIn+":fp-new"] = true

			_, err := w.scenario().Create(admin(), w.account, Input{Fingerprint: "fp-new", LicenseID: w.license.ID})

			if test.wantTaken {
				wantCodes(t, err, "FINGERPRINT_TAKEN")
			} else {
				wantCodes(t, err)
			}

			last := w.takenScope[len(w.takenScope)-1]
			if last.Kind != test.scope {
				t.Errorf("uniqueness checked in %s, want %s", last.Kind, test.scope)
			}
		})
	}
}

func TestCreateNestedComponents(t *testing.T) {
	t.Parallel()

	known := uuid.New()

	for _, test := range []struct {
		name        string
		strategy    string
		taken       string
		fingerprint string
		components  []ComponentInput
		want        []string
		inserted    int
	}{
		{"distinct", "", "", "fp-new", []ComponentInput{{Name: "cpu", Fingerprint: "c1"}, {Name: "gpu", Fingerprint: "c2"}}, nil, 2},
		{
			"duplicated pair", "", "", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "dup"}, {Name: "b", Fingerprint: "dup"}},
			[]string{"COMPONENTS_FINGERPRINT_CONFLICT", "COMPONENTS_FINGERPRINT_CONFLICT"},
			0,
		},
		{"duplicate among three", "", "", "fp-new", []ComponentInput{
			{Name: "a", Fingerprint: "dup"}, {Name: "b", Fingerprint: "ok"}, {Name: "c", Fingerprint: "dup"},
		}, []string{"COMPONENTS_FINGERPRINT_CONFLICT", "COMPONENTS_FINGERPRINT_CONFLICT"}, 0},
		{"blank name", "", "", "fp-new", []ComponentInput{{Fingerprint: "c1"}}, []string{"COMPONENTS_NAME_MISSING"}, 0},
		{"blank fingerprint", "", "", "fp-new", []ComponentInput{{Name: "a"}}, []string{"COMPONENTS_FINGERPRINT_MISSING"}, 0},
		{
			"reserved fingerprint", "", "", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "action"}},
			[]string{"COMPONENTS_FINGERPRINT_NOT_ALLOWED"},
			0,
		},
		{"nested errors precede the machine's", "", "", "", []ComponentInput{
			{Name: "a", Fingerprint: "dup"}, {Name: "b", Fingerprint: "dup"},
		}, []string{"COMPONENTS_FINGERPRINT_CONFLICT", "COMPONENTS_FINGERPRINT_CONFLICT", "FINGERPRINT_MISSING"}, 0},
		{
			"taken per machine by default", "", "machine", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "c1"}},
			[]string{"COMPONENTS_FINGERPRINT_TAKEN"},
			0,
		},
		{
			"taken per license", "UNIQUE_PER_LICENSE", "license", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "c1"}},
			[]string{"COMPONENTS_FINGERPRINT_TAKEN"},
			0,
		},
		{
			"taken per policy", "UNIQUE_PER_POLICY", "policy", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "c1"}},
			[]string{"COMPONENTS_FINGERPRINT_TAKEN"},
			0,
		},
		{
			"taken per product", "UNIQUE_PER_PRODUCT", "product", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "c1"}},
			[]string{"COMPONENTS_FINGERPRINT_TAKEN"},
			0,
		},
		{
			"taken per account", "UNIQUE_PER_ACCOUNT", "account", "fp-new",
			[]ComponentInput{{Name: "a", Fingerprint: "c1"}},
			[]string{"COMPONENTS_FINGERPRINT_TAKEN"},
			0,
		},
		{"taken in another scope", "UNIQUE_PER_LICENSE", "account", "fp-new", []ComponentInput{{Name: "a", Fingerprint: "c1"}}, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.license.Policy.ComponentUniquenessStrategy = test.strategy

			if test.taken != "" {
				w.compTaken[test.taken+":c1"] = true
			}

			machine, err := w.scenario().Create(admin(), w.account, Input{
				ID: known.String(), Fingerprint: test.fingerprint, LicenseID: w.license.ID, Components: test.components,
			})
			wantCodes(t, err, test.want...)

			if len(w.insertedComp) != test.inserted {
				t.Fatalf("inserted %d components, want %d", len(w.insertedComp), test.inserted)
			}

			for i, c := range w.insertedComp {
				if c.MachineID != machine.ID {
					t.Errorf("component %d on machine %v", i, c.MachineID)
				}

				if i > 0 && !c.CreatedAt.After(w.insertedComp[i-1].CreatedAt) {
					t.Errorf("component %d not saved after the previous one", i)
				}
			}
		})
	}
}

func TestCreateNestedComponentPointer(t *testing.T) {
	t.Parallel()

	w := newWorld()

	_, err := w.scenario().Create(admin(), w.account, Input{
		Fingerprint: "fp-new", LicenseID: w.license.ID,
		Components: []ComponentInput{{Name: "ok", Fingerprint: "c0"}, {Name: "", Fingerprint: "c1"}},
	})

	wantCodes(t, err, "COMPONENTS_NAME_MISSING")

	var doc *apperr.T
	if !errors.As(err, &doc) || doc.Source.Pointer != "/data/relationships/components/data/1/attributes/name" {
		t.Fatalf("pointer = %+v", doc)
	}
}

func TestCreateAuthorization(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		role domain.Role
		// bearer picks the bearer id given the world.
		bearer    func(w *world) uuid.UUID
		protected bool
		ok        bool
	}{
		{"admin", domain.RoleAdmin, func(*world) uuid.UUID { return uuid.New() }, false, true},
		{"developer", domain.RoleDeveloper, func(*world) uuid.UUID { return uuid.New() }, false, true},
		{"sales agent", domain.RoleSalesAgent, func(*world) uuid.UUID { return uuid.New() }, false, true},
		{"support agent", domain.RoleSupportRole, func(*world) uuid.UUID { return uuid.New() }, false, false},
		{"read only", domain.RoleReadOnly, func(*world) uuid.UUID { return uuid.New() }, false, false},
		{"own product", domain.RoleProduct, func(w *world) uuid.UUID { return w.license.ProductID }, false, true},
		{"foreign product", domain.RoleProduct, func(*world) uuid.UUID { return uuid.New() }, false, false},
		{"own license", domain.RoleLicense, func(w *world) uuid.UUID { return w.license.ID }, false, true},
		{"foreign license", domain.RoleLicense, func(*world) uuid.UUID { return uuid.New() }, false, false},
		{"license owner", domain.RoleUser, func(w *world) uuid.UUID { return w.license.OwnerID.UUID }, false, true},
		{"license owner, protected", domain.RoleUser, func(w *world) uuid.UUID { return w.license.OwnerID.UUID }, true, false},
		{"other user", domain.RoleUser, func(*world) uuid.UUID { return uuid.New() }, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.license.OwnerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			w.license.Protected = &test.protected

			_, err := w.scenario().Create(as(test.role, test.bearer(w)), w.account,
				Input{Fingerprint: "fp-new", LicenseID: w.license.ID})

			if (err == nil) != test.ok {
				t.Fatalf("err = %v, want ok = %v", err, test.ok)
			}

			if !test.ok {
				wantStatus(t, err, 403)

				if len(w.inserted) != 0 || w.locked != 0 {
					t.Errorf("refused create touched the store: inserted=%d locked=%d", len(w.inserted), w.locked)
				}
			}
		})
	}
}

func TestCreateComponentPermissionRequired(t *testing.T) {
	t.Parallel()

	w := newWorld()
	p := access.Principal{
		BearerType: access.BearerUser, BearerID: uuid.New(), Role: domain.RoleAdmin,
		Permissions: map[string]struct{}{"machine.create": {}},
	}

	_, err := w.scenario().Create(access.With(admin(), p), w.account, Input{
		Fingerprint: "fp-new", LicenseID: w.license.ID, Components: []ComponentInput{{Name: "a", Fingerprint: "c"}},
	})
	wantStatus(t, err, 403)
}

func TestCreateActivationTokenCounting(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		token   domain.Token
		locked  bool
		plain   bool
		want    []string
		nowait  bool
		counted bool
		set     [2]int
	}{
		{"unlimited token locks skip-locked", domain.Token{Activations: 3}, true, false, nil, false, true, [2]int{4, 0}},
		{"limited token with room locks nowait", domain.Token{MaxActivations: ptr(2), Activations: 1}, true, false, nil, true, true, [2]int{2, 0}},
		{
			"limit exceeded",
			domain.Token{MaxActivations: ptr(1), Activations: 1},
			true, false,
			[]string{"ACTIVATIONS_LIMIT_EXCEEDED"},
			true, false,
			[2]int{},
		},
		{"lock not taken skips the count", domain.Token{MaxActivations: ptr(1), Activations: 9}, false, false, nil, true, false, [2]int{}},
		{"deactivation limit is not an activation limit", domain.Token{MaxDeactivations: ptr(0)}, true, false, nil, false, true, [2]int{1, 0}},
		{"license key authentication is not counted", domain.Token{MaxActivations: ptr(0)}, true, true, nil, false, false, [2]int{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.token, w.tokenLocked = test.token, test.locked

			p := access.Principal{
				BearerType: access.BearerLicense, BearerID: w.license.ID, Role: domain.RoleLicense,
				Permissions: access.RolePermissions(domain.RoleLicense), TokenID: uuid.New(),
			}
			if test.plain {
				p.TokenID = uuid.Nil
			}

			_, err := w.scenario().Create(access.With(admin(), p), w.account,
				Input{Fingerprint: "fp-new", LicenseID: w.license.ID})
			wantCodes(t, err, test.want...)

			if !test.plain && w.lockNowait != test.nowait {
				t.Errorf("nowait = %v, want %v", w.lockNowait, test.nowait)
			}

			if w.tokenSetOK != test.counted || w.tokenSetOK && w.tokenSet != test.set {
				t.Errorf("counters set = %v %v, want %v %v", w.tokenSetOK, w.tokenSet, test.counted, test.set)
			}

			if (err == nil) != (len(w.inserted) == 1) {
				t.Errorf("err = %v with %d inserted", err, len(w.inserted))
			}
		})
	}
}

func TestDestroyDeactivationTokenCounting(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		token  domain.Token
		locked bool
		want   []string
		set    [2]int
		count  bool
	}{
		{"unlimited", domain.Token{Activations: 2, Deactivations: 1}, true, nil, [2]int{2, 2}, true},
		{"limited with room", domain.Token{MaxDeactivations: ptr(2), Deactivations: 1}, true, nil, [2]int{0, 2}, true},
		{
			"limit exceeded",
			domain.Token{MaxDeactivations: ptr(1), Deactivations: 1},
			true,
			[]string{"DEACTIVATIONS_LIMIT_EXCEEDED"},
			[2]int{},
			false,
		},
		{"lock not taken", domain.Token{MaxDeactivations: ptr(1), Deactivations: 1}, false, nil, [2]int{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.token, w.tokenLocked = test.token, test.locked
			machine := w.seedMachine(domain.Machine{})

			p := access.Principal{
				BearerType: access.BearerLicense, BearerID: w.license.ID, Role: domain.RoleLicense,
				Permissions: access.RolePermissions(domain.RoleLicense), TokenID: uuid.New(),
			}

			err := w.scenario().Destroy(access.With(admin(), p), w.account, machine.ID.String())
			wantCodes(t, err, test.want...)

			if w.tokenSetOK != test.count || w.tokenSetOK && w.tokenSet != test.set {
				t.Errorf("counters set = %v %v", w.tokenSetOK, w.tokenSet)
			}

			if deleted := len(w.deletedMachines) == 1; deleted != (err == nil) {
				t.Errorf("deleted = %v with err %v", deleted, err)
			}
		})
	}
}

func TestDestroy(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	if err := w.scenario().Destroy(admin(), w.account, machine.Fingerprint); err != nil {
		t.Fatal(err)
	}

	if len(w.deletedMachines) != 1 || w.deletedMachines[0] != machine.ID || w.refreshed != 1 ||
		len(w.events) != 1 || w.events[0] != "machine.deleted" {
		t.Errorf("deleted=%v refreshed=%d events=%v", w.deletedMachines, w.refreshed, w.events)
	}

	wantStatus(t, w.scenario().Destroy(admin(), w.account, "missing"), 404)
	wantStatus(t, w.scenario().Destroy(as(domain.RoleReadOnly, uuid.New()), w.account, machine.ID.String()), 403)
}

func BenchmarkCreate(b *testing.B) {
	w := newWorld()
	s := w.scenario()
	ctx := admin()

	for b.Loop() {
		w.machines, w.inserted, w.events = nil, nil, nil

		if _, err := s.Create(ctx, w.account, Input{Fingerprint: "fp-new", LicenseID: w.license.ID}); err != nil {
			b.Fatal(err)
		}
	}
}
