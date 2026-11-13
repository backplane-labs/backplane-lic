package machines

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Product answers the machine's product.
func (s Scenario) Product(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.Product, error) {
	machine, err := s.related(ctx, account, idOrFingerprint, "product.read")
	if err != nil {
		return domain.Product{}, err
	}

	product, err := s.directory.ProductByID(ctx, account.ID, machine.ProductID)
	if err != nil {
		return domain.Product{}, fmt.Errorf("machines: product: %w", err)
	}

	return product, nil
}

// License answers the machine's license.
func (s Scenario) License(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.License, error) {
	machine, err := s.related(ctx, account, idOrFingerprint, "license.read")
	if err != nil {
		return domain.License{}, err
	}

	license, err := s.licenses.LicenseByID(ctx, account.ID, machine.LicenseID)
	if err != nil {
		return domain.License{}, fmt.Errorf("machines: license: %w", err)
	}

	return license, nil
}

// Owner answers the machine's owner and its status; present is false
// for an unowned machine.
func (s Scenario) Owner(
	ctx context.Context, account domain.Account, idOrFingerprint string,
) (domain.User, string, bool, error) {
	machine, err := s.related(ctx, account, idOrFingerprint, "user.read")
	if err != nil {
		return domain.User{}, "", false, err
	}

	return s.userOf(ctx, account, machine.OwnerID)
}

// LicenseOwner answers the v1.5 machine "user": its license's owner.
func (s Scenario) LicenseOwner(
	ctx context.Context, account domain.Account, idOrFingerprint string,
) (domain.User, string, bool, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.User{}, "", false, err
	}

	if err := s.authorize(ctx, "user.read", machine, access.MachineLicenseOwnerShow); err != nil {
		return domain.User{}, "", false, err
	}

	return s.userOf(ctx, account, machine.LicenseOwnerID)
}

func (s Scenario) userOf(
	ctx context.Context, account domain.Account, id uuid.NullUUID,
) (domain.User, string, bool, error) {
	if !id.Valid {
		return domain.User{}, "", false, nil
	}

	owner, err := s.directory.UserByID(ctx, account.ID, id.UUID)
	if err != nil {
		return domain.User{}, "", false, fmt.Errorf("machines: owner: %w", err)
	}

	active, err := s.directory.UserHasActiveLicense(ctx, account.ID, owner.ID)
	if err != nil {
		return domain.User{}, "", false, fmt.Errorf("machines: owner status: %w", err)
	}

	return owner, owner.Status(active, s.now()), true, nil
}

// Group answers the machine's group; an ungrouped machine is the group
// 404.
func (s Scenario) Group(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.Group, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Group{}, err
	}

	if !machine.GroupID.Valid {
		return domain.Group{}, apperr.NotFoundModel("group")
	}

	group, err := s.directory.GroupByID(ctx, account.ID, machine.GroupID.UUID)
	if errors.Is(err, ErrNotFound) {
		return domain.Group{}, apperr.NotFoundModel("group")
	}

	if err != nil {
		return domain.Group{}, fmt.Errorf("machines: group: %w", err)
	}

	if err := s.authorize(ctx, "group.read", machine, access.MachineShow); err != nil {
		return domain.Group{}, err
	}

	return group, nil
}

// SetOwner moves the machine to another owner (or none); decode reads
// the linkage after the authorization.
func (s Scenario) SetOwner(
	ctx context.Context, account domain.Account, idOrFingerprint string, decode func() (uuid.NullUUID, error),
) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, "machine.owner.update", machine, access.MachineRelationshipUpdate); err != nil {
		return domain.Machine{}, err
	}

	owner, err := decode()
	if err != nil {
		return domain.Machine{}, err
	}

	before := machine
	machine.OwnerID, machine.UpdatedAt = owner, s.now()

	// A new owner hands its group down to an ungrouped machine.
	if owner.Valid && owner != before.OwnerID && !machine.GroupID.Valid {
		user, err := s.directory.UserByID(ctx, account.ID, owner.UUID)

		switch {
		case err == nil:
			machine.GroupID = user.GroupID
		case !errors.Is(err, ErrNotFound):
			return domain.Machine{}, fmt.Errorf("machines: owner: %w", err)
		}
	}

	return s.save(ctx, account, before, machine, "machine.owner.updated")
}

// SetGroup moves the machine to another group (or none); decode reads
// the linkage before the authorization (the reference looks the group up
// first).
func (s Scenario) SetGroup(
	ctx context.Context, account domain.Account, idOrFingerprint string, decode func() (uuid.NullUUID, error),
) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	group, err := decode()
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, "machine.group.update", machine, access.MachineRelationshipUpdate); err != nil {
		return domain.Machine{}, err
	}

	before := machine
	machine.GroupID, machine.UpdatedAt = group, s.now()

	return s.save(ctx, account, before, machine, "machine.group.updated")
}

// related loads the machine and authorizes reading one of its
// relationships.
func (s Scenario) related(
	ctx context.Context, account domain.Account, idOrFingerprint, permission string,
) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, permission, machine, access.MachineShow); err != nil {
		return domain.Machine{}, err
	}

	return machine, nil
}
