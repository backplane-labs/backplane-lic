package machines

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// SubFilter is the filter set of the component and process indexes.
type SubFilter struct {
	MachineID uuid.NullUUID
	ProductID uuid.NullUUID
	LicenseID uuid.NullUUID
	OwnerID   uuid.NullUUID
	// User is an id or an email of a license user.
	User string
	// Status (processes) is ALIVE or DEAD; any other value matches none.
	Status string
}

// ComponentList is one components index page.
type ComponentList struct {
	Items []domain.MachineComponent
	Count int
	Next  uuid.NullUUID
}

// ComponentUpdate is the decoded update body.
type ComponentUpdate struct {
	Name     Field[string]
	Metadata Field[map[string]any]
}

// IndexComponents answers the filtered page within the principal's scope.
func (s Scenario) IndexComponents(
	ctx context.Context, account domain.Account, f SubFilter, params paging.Params,
) (ComponentList, error) {
	if err := access.Require(ctx, "component.read"); err != nil {
		return ComponentList{}, err
	}

	return s.componentPage(ctx, account, f, params)
}

// MachineComponents answers a machine's components (its relationship
// index).
func (s Scenario) MachineComponents(
	ctx context.Context, account domain.Account, idOrFingerprint string, params paging.Params,
) (ComponentList, error) {
	machine, err := s.related(ctx, account, idOrFingerprint, "component.read")
	if err != nil {
		return ComponentList{}, err
	}

	return s.componentPage(ctx, account, SubFilter{MachineID: uuid.NullUUID{UUID: machine.ID, Valid: true}}, params)
}

// MachineComponent answers one component of a machine.
func (s Scenario) MachineComponent(
	ctx context.Context, account domain.Account, idOrFingerprint, id string,
) (domain.MachineComponent, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.MachineComponent{}, err
	}

	component, err := s.findComponent(ctx, account, Scope{}, id)
	if err == nil && component.MachineID != machine.ID {
		err = ErrNotFound
	}

	if err != nil {
		return domain.MachineComponent{}, notFoundOr(err, "machine component", id)
	}

	if err := s.authorize(ctx, "component.read", machine, access.MachineShow); err != nil {
		return domain.MachineComponent{}, err
	}

	return component, nil
}

func (s Scenario) componentPage(
	ctx context.Context, account domain.Account, f SubFilter, params paging.Params,
) (ComponentList, error) {
	window := params.Window()

	items, total, err := s.store.ListComponents(ctx, account.ID, f, ScopeOf(access.PrincipalOf(ctx)), window)
	if err != nil {
		return ComponentList{}, fmt.Errorf("machines: components: %w", err)
	}

	out := ComponentList{Items: items, Count: total}
	if params.Mode == paging.ModeCursor && len(items) > int(window.Limit) {
		out.Items = items[:window.Limit]
		out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
	}

	return out, nil
}

// ShowComponent answers one component.
func (s Scenario) ShowComponent(
	ctx context.Context, account domain.Account, id string,
) (domain.MachineComponent, error) {
	component, _, err := s.componentAction(ctx, account, id, "component.read", access.MachineShow)

	return component, err
}

// CreateComponent adds a component to a machine.
func (s Scenario) CreateComponent(
	ctx context.Context, account domain.Account, machineID uuid.UUID, in ComponentInput,
) (domain.MachineComponent, error) {
	machine, found, err := s.machineOf(ctx, account, machineID)
	if err != nil {
		return domain.MachineComponent{}, err
	}

	if err := access.Require(ctx, "component.create"); err != nil {
		return domain.MachineComponent{}, err
	}

	if err := access.SubresourceCreate(access.PrincipalOf(ctx), machine); err != nil {
		return domain.MachineComponent{}, err
	}

	now := s.now()
	component := domain.MachineComponent{
		ID: uuid.New(), AccountID: account.ID, MachineID: machineID, Name: in.Name, Fingerprint: in.Fingerprint,
		Metadata: in.Metadata, CreatedAt: now, UpdatedAt: now, ProductID: machine.ProductID, LicenseID: machine.LicenseID,
	}

	if component.Metadata == nil {
		component.Metadata = map[string]any{}
	}

	err = s.write(ctx, func(ctx context.Context) error {
		v := violations{topic: topicComponents}

		if !found {
			v.add("MACHINE_NOT_FOUND", "must exist", "/data/relationships/machine")
		}

		if err := s.validateComponent(ctx, account, machine, found, component, "/data/attributes/", &v); err != nil {
			return err
		}

		if err := v.err(); err != nil {
			return err
		}

		if err := s.store.InsertComponent(ctx, component); err != nil {
			return fmt.Errorf("machines: create component: %w", err)
		}

		return s.publish(ctx, "component.created", component)
	})
	if err != nil {
		return domain.MachineComponent{}, err
	}

	return component, nil
}

// UpdateComponent merges the input over the component; decode reads the
// body once it is found and authorized.
func (s Scenario) UpdateComponent(
	ctx context.Context, account domain.Account, id string, decode func() (ComponentUpdate, error),
) (domain.MachineComponent, error) {
	component, _, err := s.componentAction(ctx, account, id, "component.update", access.SubresourceUpdate)
	if err != nil {
		return domain.MachineComponent{}, err
	}

	in, err := decode()
	if err != nil {
		return domain.MachineComponent{}, err
	}

	if in.Name.Set && in.Name.Value != nil {
		component.Name = *in.Name.Value
	}

	if in.Metadata.Set {
		component.Metadata = map[string]any{}
		if in.Metadata.Value != nil {
			component.Metadata = *in.Metadata.Value
		}
	}

	component.UpdatedAt = s.now()

	v := violations{topic: topicComponents}
	validateComponentAttributes(component, "/data/attributes/", &v)

	if err := v.err(); err != nil {
		return domain.MachineComponent{}, err
	}

	err = s.write(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateComponent(ctx, component); err != nil {
			return fmt.Errorf("machines: update component: %w", err)
		}

		return s.publish(ctx, "component.updated", component)
	})
	if err != nil {
		return domain.MachineComponent{}, err
	}

	return component, nil
}

// DestroyComponent removes the component.
func (s Scenario) DestroyComponent(ctx context.Context, account domain.Account, id string) error {
	component, _, err := s.componentAction(ctx, account, id, "component.delete", access.SubresourceDestroy)
	if err != nil {
		return err
	}

	return s.write(ctx, func(ctx context.Context) error {
		if err := s.publish(ctx, "component.deleted", component); err != nil {
			return err
		}

		if err := s.store.DeleteComponent(ctx, account.ID, component.ID); err != nil {
			return fmt.Errorf("machines: delete component: %w", err)
		}

		return nil
	})
}

// ComponentMachine answers the component's machine; its product and
// license relationships read through it.
func (s Scenario) ComponentMachine(
	ctx context.Context, account domain.Account, id, permission string,
) (domain.Machine, error) {
	_, machine, err := s.componentAction(ctx, account, id, permission, access.MachineShow)

	return machine, err
}

// componentAction loads the component within the scope and authorizes
// the action against its machine.
func (s Scenario) componentAction(
	ctx context.Context, account domain.Account, id, permission string,
	rule func(access.Principal, domain.Machine) error,
) (domain.MachineComponent, domain.Machine, error) {
	component, err := s.findComponent(ctx, account, ScopeOf(access.PrincipalOf(ctx)), id)
	if err != nil {
		return domain.MachineComponent{}, domain.Machine{}, notFoundOr(err, "machine component", id)
	}

	machine, _, err := s.machineOf(ctx, account, component.MachineID)
	if err != nil {
		return domain.MachineComponent{}, domain.Machine{}, err
	}

	if err := s.authorize(ctx, permission, machine, rule); err != nil {
		return domain.MachineComponent{}, domain.Machine{}, err
	}

	return component, machine, nil
}

func (s Scenario) findComponent(
	ctx context.Context, account domain.Account, scope Scope, id string,
) (domain.MachineComponent, error) {
	if !uuidRe.MatchString(id) {
		return domain.MachineComponent{}, ErrNotFound
	}

	component, err := s.store.FindComponent(ctx, account.ID, scope, uuid.MustParse(id))
	if err != nil {
		return domain.MachineComponent{}, fmt.Errorf("machines: component: %w", err)
	}

	return component, nil
}

// machineOf loads a machine by id outside the bearer's scope (its
// sub-resources' rules judge it); found is false when it does not exist.
func (s Scenario) machineOf(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Machine, bool, error) {
	machine, err := s.store.FindMachine(ctx, account.ID, Scope{}, id.String(), s.now())
	if errors.Is(err, ErrNotFound) {
		return domain.Machine{}, false, nil
	}

	if err != nil {
		return domain.Machine{}, false, fmt.Errorf("machines: machine: %w", err)
	}

	return machine, true, nil
}

// validateComponent runs the component model's validations.
func (s Scenario) validateComponent(
	ctx context.Context, account domain.Account, machine domain.Machine, found bool,
	component domain.MachineComponent, prefix string, v *violations,
) error {
	if found {
		taken, err := s.store.ComponentFingerprintTaken(ctx, account.ID,
			Uniqueness{Kind: kindMachine, ID: machine.ID}, component.Fingerprint)
		if err != nil {
			return fmt.Errorf("machines: component fingerprint: %w", err)
		}

		if taken {
			v.add("FINGERPRINT_TAKEN", "has already been taken", prefix+"fingerprint")
		}
	}

	validateComponentAttributes(component, prefix, v)

	if !found {
		return nil
	}

	license, err := s.licenses.LicenseByID(ctx, account.ID, machine.LicenseID)
	if err != nil {
		return fmt.Errorf("machines: component license: %w", err)
	}

	return s.validateComponentUniqueness(ctx, account, license, machine.ID, component.Fingerprint, prefix, v)
}

func validateComponentAttributes(component domain.MachineComponent, prefix string, v *violations) {
	if isReservedWord(component.Fingerprint) {
		v.add("FINGERPRINT_NOT_ALLOWED", "is reserved", prefix+"fingerprint")
	}

	if tooLong(&component.Fingerprint, maxFingerprintLength) {
		v.add("FINGERPRINT_TOO_LONG", "is too long (maximum is 4096 characters)", prefix+"fingerprint")
	}

	if component.Fingerprint == "" {
		v.add("FINGERPRINT_MISSING", "can't be blank", prefix+"fingerprint")
	}

	if tooLong(&component.Name, maxTextLength) {
		v.add("NAME_TOO_LONG", "is too long (maximum is 255 characters)", prefix+"name")
	}

	if component.Name == "" {
		v.add("NAME_MISSING", "can't be blank", prefix+"name")
	}

	if detail := domain.MetadataViolation(component.Metadata); detail != "" {
		v.add("METADATA_TOO_LONG", detail, prefix+"metadata")
	}
}

// validateComponentUniqueness is the fingerprint against the policy's
// component uniqueness strategy (machineID is nil while the machine is
// still unsaved: nothing is taken within it yet).
func (s Scenario) validateComponentUniqueness(
	ctx context.Context, account domain.Account, license domain.License, machineID uuid.UUID,
	fingerprint, prefix string, v *violations,
) error {
	scope, detail := Uniqueness{Kind: kindMachine, ID: machineID}, "has already been taken"

	switch license.Policy.ComponentUniqueness() {
	case "UNIQUE_PER_ACCOUNT":
		scope, detail = Uniqueness{Kind: "account", ID: account.ID}, "has already been taken for this account"
	case "UNIQUE_PER_PRODUCT":
		scope, detail = Uniqueness{Kind: kindProduct, ID: license.ProductID}, "has already been taken for this product"
	case "UNIQUE_PER_POLICY":
		scope, detail = Uniqueness{Kind: "policy", ID: license.PolicyID}, "has already been taken for this policy"
	case "UNIQUE_PER_LICENSE":
		scope, detail = Uniqueness{Kind: kindLicense, ID: license.ID}, "has already been taken for this license"
	}

	taken, err := s.store.ComponentFingerprintTaken(ctx, account.ID, scope, fingerprint)
	if err != nil {
		return fmt.Errorf("machines: component uniqueness: %w", err)
	}

	if taken {
		v.add("FINGERPRINT_TAKEN", detail, prefix+"fingerprint")
	}

	return nil
}

// notFoundOr answers the record 404 for a missing record, else the error.
func notFoundOr(err error, resource, id string) error {
	if errors.Is(err, ErrNotFound) {
		return apperr.NotFound(resource, id)
	}

	return err
}
