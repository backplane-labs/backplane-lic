package machines

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestCreateComponent(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		strategy string
		taken    string
		in       ComponentInput
		missing  bool
		want     []string
	}{
		{"valid", "", "", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, nil},
		{"blank name", "", "", ComponentInput{Fingerprint: "c1"}, false, []string{"NAME_MISSING"}},
		{"long name", "", "", ComponentInput{Name: strings.Repeat("n", 256), Fingerprint: "c1"}, false, []string{"NAME_TOO_LONG"}},
		{"blank fingerprint", "", "", ComponentInput{Name: "cpu"}, false, []string{"FINGERPRINT_MISSING"}},
		{"reserved fingerprint", "", "", ComponentInput{Name: "cpu", Fingerprint: "actions"}, false, []string{"FINGERPRINT_NOT_ALLOWED"}},
		{
			"metadata too large", "", "",
			ComponentInput{Name: "cpu", Fingerprint: "c1", Metadata: map[string]any{"k": strings.Repeat("x", 17000)}},
			false,
			[]string{"METADATA_TOO_LONG"},
		},
		{"taken in machine is reported once", "", "machine", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, []string{"FINGERPRINT_TAKEN"}},
		{"taken per license", "UNIQUE_PER_LICENSE", "license", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, []string{"FINGERPRINT_TAKEN"}},
		{"taken per policy", "UNIQUE_PER_POLICY", "policy", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, []string{"FINGERPRINT_TAKEN"}},
		{"taken per product", "UNIQUE_PER_PRODUCT", "product", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, []string{"FINGERPRINT_TAKEN"}},
		{"taken per account", "UNIQUE_PER_ACCOUNT", "account", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, []string{"FINGERPRINT_TAKEN"}},
		{"taken in another scope", "UNIQUE_PER_LICENSE", "account", ComponentInput{Name: "cpu", Fingerprint: "c1"}, false, nil},
		{"unknown machine", "", "", ComponentInput{Name: "cpu", Fingerprint: "c1"}, true, []string{"MACHINE_NOT_FOUND"}},
		{"unknown machine and blank name", "", "", ComponentInput{Fingerprint: "c1"}, true, []string{"MACHINE_NOT_FOUND", "NAME_MISSING"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.license.Policy.ComponentUniquenessStrategy = test.strategy
			machine := w.seedMachine(domain.Machine{})

			if test.taken != "" {
				w.compTaken[test.taken+":c1"] = true
			}

			id := machine.ID
			if test.missing {
				id = uuid.New()
			}

			got, err := w.scenario().CreateComponent(admin(), w.account, id, test.in)
			wantCodes(t, err, test.want...)

			if err != nil {
				if len(w.insertedComp) != 0 {
					t.Error("a refused component was stored")
				}

				return
			}

			if got.MachineID != machine.ID || got.ProductID != machine.ProductID || got.LicenseID != machine.LicenseID ||
				got.Metadata == nil || len(w.events) != 1 || w.events[0] != "component.created" {
				t.Errorf("component = %+v events = %v", got, w.events)
			}
		})
	}
}

func TestCreateComponentAuthorization(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		role   domain.Role
		bearer func(w *world) uuid.UUID
		ok     bool
	}{
		{domain.RoleAdmin, func(*world) uuid.UUID { return uuid.New() }, true},
		{domain.RoleSalesAgent, func(*world) uuid.UUID { return uuid.New() }, true},
		{domain.RoleSupportRole, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleReadOnly, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleProduct, func(w *world) uuid.UUID { return w.license.ProductID }, true},
		{domain.RoleProduct, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleLicense, func(w *world) uuid.UUID { return w.license.ID }, true},
	} {
		t.Run(string(test.role), func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})

			_, err := w.scenario().CreateComponent(as(test.role, test.bearer(w)), w.account, machine.ID,
				ComponentInput{Name: "cpu", Fingerprint: "c1"})
			if (err == nil) != test.ok {
				t.Fatalf("err = %v, want ok = %v", err, test.ok)
			}
		})
	}
}

func TestUpdateComponent(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})
	component := domain.MachineComponent{
		ID: uuid.New(), MachineID: machine.ID, Name: "cpu", Fingerprint: "c1", Metadata: map[string]any{"a": 1.0},
	}
	w.components = []domain.MachineComponent{component}
	s := w.scenario()

	meta := map[string]any{"b": 2.0}
	got, err := s.UpdateComponent(admin(), w.account, component.ID.String(), func() (ComponentUpdate, error) {
		return ComponentUpdate{
			Name: Field[string]{Set: true, Value: ptr("gpu")}, Metadata: Field[map[string]any]{Set: true, Value: &meta},
		}, nil
	})
	if err != nil || got.Name != "gpu" || got.Fingerprint != "c1" || got.Metadata["b"] != 2.0 || got.Metadata["a"] != nil {
		t.Fatalf("update = %+v %v", got, err)
	}

	if len(w.events) != 1 || w.events[0] != "component.updated" {
		t.Errorf("events = %v", w.events)
	}

	// A null name is ignored; a null metadata empties the map.
	got, err = s.UpdateComponent(admin(), w.account, component.ID.String(), func() (ComponentUpdate, error) {
		return ComponentUpdate{Name: Field[string]{Set: true}, Metadata: Field[map[string]any]{Set: true}}, nil
	})
	if err != nil || got.Name != "gpu" || got.Metadata == nil || len(got.Metadata) != 0 {
		t.Fatalf("null update = %+v %v", got, err)
	}

	big := map[string]any{"k": strings.Repeat("x", 17000)}
	_, err = s.UpdateComponent(admin(), w.account, component.ID.String(), func() (ComponentUpdate, error) {
		return ComponentUpdate{Metadata: Field[map[string]any]{Set: true, Value: &big}}, nil
	})
	wantCodes(t, err, "METADATA_TOO_LONG")

	empty := ""
	_, err = s.UpdateComponent(admin(), w.account, component.ID.String(), func() (ComponentUpdate, error) {
		return ComponentUpdate{Name: Field[string]{Set: true, Value: &empty}}, nil
	})
	wantCodes(t, err, "NAME_MISSING")

	// 404 for an unknown or malformed id; 403 before decoding.
	_, err = s.UpdateComponent(admin(), w.account, "nope", func() (ComponentUpdate, error) { return ComponentUpdate{}, nil })
	wantStatus(t, err, 404)

	decoded := false
	_, err = s.UpdateComponent(as(domain.RoleReadOnly, uuid.New()), w.account, component.ID.String(),
		func() (ComponentUpdate, error) {
			decoded = true

			return ComponentUpdate{}, nil
		})
	wantStatus(t, err, 403)

	if decoded {
		t.Error("the body was decoded for a forbidden update")
	}
}

func TestComponentLookupAndDestroy(t *testing.T) {
	t.Parallel()

	w := newWorld()
	machine := w.seedMachine(domain.Machine{})
	other := w.seedMachine(domain.Machine{})
	component := domain.MachineComponent{ID: uuid.New(), MachineID: machine.ID, Name: "cpu", Fingerprint: "c1"}
	w.components = []domain.MachineComponent{component}
	s := w.scenario()

	if got, err := s.ShowComponent(admin(), w.account, component.ID.String()); err != nil || got.ID != component.ID {
		t.Fatalf("show = %v %v", got.ID, err)
	}

	if _, err := s.MachineComponent(admin(), w.account, machine.ID.String(), component.ID.String()); err != nil {
		t.Fatal(err)
	}

	_, err := s.MachineComponent(admin(), w.account, other.ID.String(), component.ID.String())
	wantStatus(t, err, 404)

	list, err := s.MachineComponents(admin(), w.account, machine.ID.String(), pagingDefault())
	if err != nil || list.Count != 1 {
		t.Fatalf("list = %+v %v", list, err)
	}

	list, err = s.MachineComponents(admin(), w.account, other.ID.String(), pagingDefault())
	if err != nil || list.Count != 0 {
		t.Fatalf("other list = %+v %v", list, err)
	}

	if err := s.DestroyComponent(as(domain.RoleSupportRole, uuid.New()), w.account, component.ID.String()); err == nil {
		t.Error("support agent destroyed a component")
	}

	if err := s.DestroyComponent(admin(), w.account, component.ID.String()); err != nil {
		t.Fatal(err)
	}

	if len(w.deletedComponents) != 1 || w.events[len(w.events)-1] != "component.deleted" {
		t.Errorf("deleted = %v events = %v", w.deletedComponents, w.events)
	}
}
