package machines

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestUpdateMergesAttributes(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{
		Name: ptr("old"), Hostname: ptr("host"), Metadata: map[string]any{"a": 1.0}, Cores: ptr(int32(2)),
	})

	meta := map[string]any{"b": 2.0}
	got, err := w.scenario().Update(admin(), w.account, machine.Fingerprint, func() (Input, error) {
		return Input{
			Name:     Field[string]{Set: true, Value: ptr("new")},
			Hostname: Field[string]{Set: true},
			Metadata: Field[map[string]any]{Set: true, Value: &meta},
			Cores:    Field[int64]{Set: true, Value: ptr(int64(4))},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if *got.Name != "new" || got.Hostname != nil || *got.Cores != 4 || got.Metadata["b"] != 2.0 || got.Metadata["a"] != nil {
		t.Errorf("merge result = %+v", got)
	}

	if !got.UpdatedAt.Equal(testNow) || len(w.updated) != 1 || len(w.events) != 1 || w.events[0] != "machine.updated" {
		t.Errorf("updated=%d events=%v", len(w.updated), w.events)
	}

	if w.locked != 1 || w.refreshed != 1 {
		t.Errorf("locked=%d refreshed=%d", w.locked, w.refreshed)
	}

	// An absent field is left alone, a null metadata empties the map.
	got, err = w.scenario().Update(admin(), w.account, machine.ID.String(), func() (Input, error) {
		return Input{Metadata: Field[map[string]any]{Set: true}}, nil
	})
	if err != nil || *got.Name != "new" || len(got.Metadata) != 0 || got.Metadata == nil {
		t.Errorf("second update = %+v, %v", got, err)
	}
}

func TestUpdateOrderAndAuthorization(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		role     domain.Role
		bearer   func(w *world, m domain.Machine) uuid.UUID
		missing  bool
		wantCode int
		decoded  bool
	}{
		{"unknown machine is 404 before decoding", domain.RoleAdmin, anyone, true, 404, false},
		{"read only is refused before decoding", domain.RoleReadOnly, anyone, false, 403, false},
		{"support agent may update", domain.RoleSupportRole, anyone, false, 0, true},
		{"own product", domain.RoleProduct, func(w *world, _ domain.Machine) uuid.UUID { return w.license.ProductID }, false, 0, true},
		{"foreign product", domain.RoleProduct, anyone, false, 403, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})
			id := machine.ID.String()

			if test.missing {
				id = uuid.NewString()
			}

			decoded := false
			_, err := w.scenario().Update(as(test.role, test.bearer(w, machine)), w.account, id, func() (Input, error) {
				decoded = true

				return Input{}, nil
			})

			if decoded != test.decoded {
				t.Errorf("decoded = %v, want %v", decoded, test.decoded)
			}

			if test.wantCode == 0 {
				if err != nil {
					t.Fatal(err)
				}

				return
			}

			wantStatus(t, err, test.wantCode)
		})
	}
}

func anyone(*world, domain.Machine) uuid.UUID { return uuid.New() }

func TestUpdateDecodeErrorStopsTheUpdate(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	_, err := w.scenario().Update(admin(), w.account, machine.ID.String(), func() (Input, error) {
		return Input{}, errBoom
	})
	if !errors.Is(err, errBoom) || len(w.updated) != 0 {
		t.Errorf("err = %v, updated = %d", err, len(w.updated))
	}
}

func TestUpdateLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		policy func(p *domain.Policy)
		lic    func(l *domain.License)
		totals Totals
		cores  int64
		want   []string
	}{
		{
			name:   "cores past the limit",
			policy: func(p *domain.Policy) { p.MaxCores = ptr(int32(4)) },
			totals: Totals{Cores: 2}, cores: 3,
			want: []string{"MACHINE_CORE_LIMIT_EXCEEDED"},
		},
		{
			name:   "cores within the limit",
			policy: func(p *domain.Policy) { p.MaxCores = ptr(int32(4)) },
			totals: Totals{Cores: 2}, cores: 2,
		},
		{
			name:   "the machine count is not rechecked on update",
			policy: func(p *domain.Policy) { p.MaxMachines = ptr(int32(1)) },
			lic:    func(l *domain.License) { l.MachinesCount = 5 },
			totals: Totals{Count: 5}, cores: 2,
		},
		{
			name:   "overage allows",
			policy: func(p *domain.Policy) { p.MaxCores, p.OverageStrategy = ptr(int32(4)), "ALLOW_2X_OVERAGE" },
			totals: Totals{Cores: 4}, cores: 4,
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

			machine := w.seedMachine(domain.Machine{})

			_, err := w.scenario().Update(admin(), w.account, machine.ID.String(), func() (Input, error) {
				return Input{Cores: Field[int64]{Set: true, Value: ptr(test.cores)}}, nil
			})
			wantCodes(t, err, test.want...)

			if len(w.totalsCalls) == 0 || w.totalsCalls[0].except != machine.ID {
				t.Errorf("totals must exclude the machine itself: %+v", w.totalsCalls)
			}
		})
	}
}

func TestUpdateRejectsOutOfRangeCores(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	_, err := w.scenario().Update(admin(), w.account, machine.ID.String(), func() (Input, error) {
		return Input{Cores: Field[int64]{Set: true, Value: ptr(int64(1) << 40)}}, nil
	})
	wantCodes(t, err, "CORES_INVALID")
}

func TestSetOwner(t *testing.T) {
	t.Parallel()

	group := uuid.New()
	other := uuid.New()

	for _, test := range []struct {
		name  string
		setup func(w *world, m *domain.Machine) uuid.NullUUID
		want  []string
		check func(t *testing.T, w *world, got domain.Machine)
	}{
		{"owner must be a license user", func(w *world, _ *domain.Machine) uuid.NullUUID {
			return uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{}, false).ID, Valid: true}
		}, []string{"OWNER_INVALID"}, nil},
		{"license user becomes owner and hands its group down", func(w *world, _ *domain.Machine) uuid.NullUUID {
			w.groups[group] = domain.Group{ID: group}

			return uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{UUID: group, Valid: true}, true).ID, Valid: true}
		}, nil, func(t *testing.T, _ *world, got domain.Machine) {
			t.Helper()

			if got.GroupID.UUID != group {
				t.Errorf("group = %v", got.GroupID)
			}
		}},
		{"a grouped machine keeps its group", func(w *world, m *domain.Machine) uuid.NullUUID {
			w.groups[other] = domain.Group{ID: other}
			m.GroupID = uuid.NullUUID{UUID: other, Valid: true}
			w.groups[group] = domain.Group{ID: group}

			return uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{UUID: group, Valid: true}, true).ID, Valid: true}
		}, nil, func(t *testing.T, _ *world, got domain.Machine) {
			t.Helper()

			if got.GroupID.UUID != other {
				t.Errorf("group = %v, want unchanged", got.GroupID)
			}
		}},
		{"unknown user", func(*world, *domain.Machine) uuid.NullUUID {
			return uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}, []string{"OWNER_NOT_FOUND"}, nil},
		{"the same owner is not revalidated", func(w *world, m *domain.Machine) uuid.NullUUID {
			u := w.seedUser(uuid.NullUUID{}, false)
			m.OwnerID = uuid.NullUUID{UUID: u.ID, Valid: true}

			return m.OwnerID
		}, nil, nil},
		{"clearing the owner", func(w *world, m *domain.Machine) uuid.NullUUID {
			m.OwnerID = uuid.NullUUID{UUID: w.seedUser(uuid.NullUUID{}, true).ID, Valid: true}

			return uuid.NullUUID{}
		}, nil, func(t *testing.T, _ *world, got domain.Machine) {
			t.Helper()

			if got.OwnerID.Valid {
				t.Errorf("owner = %v", got.OwnerID)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			base := domain.Machine{}
			owner := test.setup(w, &base)
			machine := w.seedMachine(base)

			got, err := w.scenario().SetOwner(admin(), w.account, machine.ID.String(), func() (uuid.NullUUID, error) {
				return owner, nil
			})
			wantCodes(t, err, test.want...)

			if err == nil {
				if got.OwnerID != owner || w.events[len(w.events)-1] != "machine.owner.updated" {
					t.Errorf("owner = %v, events %v", got.OwnerID, w.events)
				}

				if test.check != nil {
					test.check(t, w, got)
				}
			}
		})
	}
}

func TestSetOwnerAuthorization(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})
	decode := func() (uuid.NullUUID, error) { return uuid.NullUUID{}, nil }

	for _, role := range []domain.Role{domain.RoleSupportRole, domain.RoleReadOnly, domain.RoleUser, domain.RoleLicense} {
		_, err := w.scenario().SetOwner(as(role, w.license.ID), w.account, machine.ID.String(), decode)
		wantStatus(t, err, 403)
	}

	if _, err := w.scenario().SetOwner(as(domain.RoleProduct, w.license.ProductID), w.account,
		machine.ID.String(), decode); err != nil {
		t.Fatal(err)
	}
}

func TestSetGroupLimitOnlyWhenTheGroupChanges(t *testing.T) {
	t.Parallel()

	current, target := uuid.New(), uuid.New()

	for _, test := range []struct {
		name   string
		to     uuid.NullUUID
		toFull bool
		want   []string
	}{
		{"same full group", uuid.NullUUID{UUID: current, Valid: true}, false, nil},
		{"another full group", uuid.NullUUID{UUID: target, Valid: true}, true, []string{"GROUP_MACHINE_LIMIT_EXCEEDED"}},
		{"another group with room", uuid.NullUUID{UUID: target, Valid: true}, false, nil},
		{"no group", uuid.NullUUID{}, false, nil},
		{"unknown group", uuid.NullUUID{UUID: uuid.New(), Valid: true}, false, []string{"GROUP_NOT_FOUND"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.groups[current] = domain.Group{ID: current, MaxMachines: ptr(1)}
			w.groups[target] = domain.Group{ID: target, MaxMachines: ptr(1)}
			w.groupCount = 1

			if !test.toFull && test.to.UUID == target {
				w.groups[target] = domain.Group{ID: target}
			}

			machine := w.seedMachine(domain.Machine{GroupID: uuid.NullUUID{UUID: current, Valid: true}})

			got, err := w.scenario().SetGroup(admin(), w.account, machine.ID.String(), func() (uuid.NullUUID, error) {
				return test.to, nil
			})
			wantCodes(t, err, test.want...)

			if err == nil && got.GroupID != test.to {
				t.Errorf("group = %v, want %v", got.GroupID, test.to)
			}
		})
	}
}

func TestSetGroupDecodesBeforeAuthorizing(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	_, err := w.scenario().SetGroup(as(domain.RoleReadOnly, uuid.New()), w.account, machine.ID.String(),
		func() (uuid.NullUUID, error) { return uuid.NullUUID{}, errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want the decode error", err)
	}

	_, err = w.scenario().SetGroup(as(domain.RoleReadOnly, uuid.New()), w.account, machine.ID.String(),
		func() (uuid.NullUUID, error) { return uuid.NullUUID{}, nil })
	wantStatus(t, err, 403)
}

func TestShowAndIndex(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})

	got, err := w.scenario().Show(admin(), w.account, machine.Fingerprint)
	if err != nil || got.ID != machine.ID {
		t.Fatalf("show = %v, %v", got.ID, err)
	}

	_, err = w.scenario().Show(admin(), w.account, "unknown")
	wantStatus(t, err, 404)

	_, err = w.scenario().Show(as(domain.RoleProduct, uuid.New()), w.account, machine.ID.String())
	wantStatus(t, err, 403)

	list, err := w.scenario().Index(as(domain.RoleProduct, w.license.ProductID), w.account, Filter{}, pagingDefault())
	if err != nil || list.Count != 1 {
		t.Fatalf("index = %+v, %v", list, err)
	}

	if scope := w.listedScopes[len(w.listedScopes)-1]; scope.Kind != "product" || scope.ID != w.license.ProductID {
		t.Errorf("scope = %+v", scope)
	}
}

func TestScopeOf(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	for _, test := range []struct {
		role domain.Role
		want Scope
	}{
		{domain.RoleAdmin, Scope{}},
		{domain.RoleReadOnly, Scope{}},
		{domain.RoleProduct, Scope{Kind: "product", ID: id}},
		{domain.RoleUser, Scope{Kind: "user", ID: id}},
		{domain.RoleLicense, Scope{Kind: "license", ID: id}},
	} {
		t.Run(string(test.role), func(t *testing.T) {
			t.Parallel()

			p := accessPrincipal(test.role, id)
			if got := ScopeOf(p); got != test.want {
				t.Errorf("scope = %+v, want %+v", got, test.want)
			}
		})
	}
}
