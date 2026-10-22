package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Group answers the user's group (nil when she has none).
func (s Scenario) Group(
	ctx context.Context, account domain.Account, principal access.Principal, alias string,
) (*domain.Group, error) {
	if err := access.Require(ctx, "group.read"); err != nil {
		return nil, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return nil, err
	}

	if err := userGroupShow(principal, user); err != nil {
		return nil, err
	}

	if !user.GroupID.Valid {
		return nil, nil //nolint:nilnil // data:null is the wire form of "no group"
	}

	group, err := s.store.GroupByID(ctx, account.ID, user.GroupID.UUID)
	if errors.Is(err, ErrNotFound) {
		return nil, apperr.NotFound("group", user.GroupID.UUID.String())
	}

	if err != nil {
		return nil, fmt.Errorf("users: group: %w", err)
	}

	return &group, nil
}

// SetGroup attaches the user to a group (zero UUID removes), answering
// the updated user.
func (s Scenario) SetGroup(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias string,
	groupID uuid.NullUUID,
) (Listed, error) {
	if err := access.Require(ctx, "user.group.update"); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return Listed{}, err
	}

	if err := userGroupUpdate(principal, user); err != nil {
		return Listed{}, err
	}

	original := user
	user.GroupID = groupID

	if groupID.Valid {
		if err := s.checkGroupJoin(ctx, account.ID, user, original); err != nil {
			return Listed{}, err
		}
	}

	if err := s.store.UpdateUser(ctx, user); err != nil {
		return Listed{}, fmt.Errorf("users: set group: %w", err)
	}

	if err := s.events.Publish(ctx, "user.group.updated", user); err != nil {
		return Listed{}, fmt.Errorf("users: set group: %w", err)
	}

	status, err := s.status(ctx, user)
	if err != nil {
		return Listed{}, err
	}

	return Listed{User: user, Status: status}, nil
}

// userGroupShow mirrors Users::GroupPolicy#show?.
func userGroupShow(p access.Principal, user domain.User) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent || p.Role == domain.RoleSupportRole ||
		p.Role == domain.RoleReadOnly:
		return allowIfUserBearer(p)
	case p.IsProduct():
		if user.Role == domain.RoleUser {
			return nil
		}
	case p.IsUser() && user.ID == p.BearerID:
		return nil
	}

	return apperr.ForbiddenReason(access.ReasonAllResources)
}

// userGroupUpdate mirrors Users::GroupPolicy#update?: the admin family
// minus read-only and support, products for plain users; a plain user
// never (not even herself).
func userGroupUpdate(p access.Principal, user domain.User) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent:
		return allowIfUserBearer(p)
	case p.IsProduct() && user.Role == domain.RoleUser:
		return nil
	}

	return apperr.ForbiddenReason(access.ReasonAllResources)
}

// allowIfUserBearer keeps role checks honest about the bearer kind (an
// activation token's bearer role is license, not user).
func allowIfUserBearer(p access.Principal) error {
	if p.IsUser() {
		return nil
	}

	return apperr.ForbiddenReason(access.ReasonAllResources)
}

// The related-record kinds of the user relationships.
const (
	kindProducts = "products"
	kindLicenses = "licenses"
	kindMachines = "machines"
)

// Related answers the ids of the user's related records of one kind,
// under that relationship's policy.
func (s Scenario) Related(
	ctx context.Context, account domain.Account, principal access.Principal, alias, kind string,
) ([]uuid.UUID, error) {
	permission := map[string]string{
		kindProducts: "product.read",
		kindLicenses: "license.read",
		kindMachines: "machine.read",
	}[kind]

	if err := access.Require(ctx, permission); err != nil {
		return nil, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return nil, err
	}

	if err := s.authorizeRelated(ctx, account, principal, user, kind, uuid.Nil); err != nil {
		return nil, err
	}

	switch kind {
	case kindProducts:
		return s.store.UserProductIDs(ctx, account.ID, user.ID) //nolint:wrapcheck // the port's own error
	case kindLicenses:
		return s.store.UserLicenseIDs(ctx, account.ID, user.ID) //nolint:wrapcheck // the port's own error
	default:
		return s.store.UserMachineIDs(ctx, account.ID, user.ID) //nolint:wrapcheck // the port's own error
	}
}

// RelatedShow answers one related record's id (the record must belong
// to the user's relation).
func (s Scenario) RelatedShow(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias, kind, recordAlias string,
) (uuid.UUID, error) {
	permission := map[string]string{
		kindProducts: "product.read",
		kindLicenses: "license.read",
		kindMachines: "machine.read",
	}[kind]

	if err := access.Require(ctx, permission); err != nil {
		return uuid.Nil, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return uuid.Nil, err
	}

	id, err := uuid.Parse(recordAlias)
	if err != nil {
		return uuid.Nil, apperr.NotFound(kindSingular(kind), recordAlias)
	}

	if err := s.authorizeRelated(ctx, account, principal, user, kind, id); err != nil {
		return uuid.Nil, err
	}

	ids, err := s.Related(ctx, account, principal, alias, kind)
	if err != nil {
		return uuid.Nil, err
	}

	for _, related := range ids {
		if related == id {
			return id, nil
		}
	}

	return uuid.Nil, apperr.NotFound(kindSingular(kind), recordAlias)
}

// authorizeRelated applies the Users::{Product,License,Machine}Policy
// index rule for the related kind (record uuid.Nil = index).
func (s Scenario) authorizeRelated(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	user domain.User,
	kind string,
	record uuid.UUID,
) error {
	adminFamily := principal.Role == domain.RoleAdmin ||
		principal.Role == domain.RoleDeveloper ||
		principal.Role == domain.RoleSalesAgent ||
		principal.Role == domain.RoleSupportRole ||
		principal.Role == domain.RoleReadOnly

	switch {
	case adminFamily && principal.IsUser():
		return nil
	case principal.IsProduct() && user.Role == domain.RoleUser:
		return s.authorizeProductScope(ctx, account, principal, user, kind, record)
	case principal.IsUser() && user.ID == principal.BearerID:
		return nil
	}

	return apperr.ForbiddenReason(access.ReasonAllResources)
}

// authorizeProductScope applies the product-bearer clause: every
// related record must belong to the product itself.
func (s Scenario) authorizeProductScope(
	ctx context.Context, account domain.Account, principal access.Principal,
	user domain.User, kind string, record uuid.UUID,
) error {
	ids, err := s.store.UserProductIDs(ctx, account.ID, user.ID)
	if err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	for _, id := range ids {
		if id != principal.BearerID {
			return apperr.ForbiddenReason(access.ReasonAllResources)
		}
	}

	if kind == kindProducts && record != uuid.Nil && record != principal.BearerID {
		return apperr.ForbiddenReason(access.ReasonAllResources)
	}

	return nil
}

func kindSingular(kind string) string {
	switch kind {
	case kindProducts:
		return "product"
	case kindMachines:
		return "machine"
	default:
		return "license"
	}
}

// RelatedUser resolves and authorizes the user of a relationship route
// (the nested license routes of the licenses group reuse it).
func (s Scenario) RelatedUser(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias, kind string,
) (domain.User, error) {
	permission := map[string]string{
		kindProducts: "product.read",
		kindLicenses: "license.read",
		kindMachines: "machine.read",
	}[kind]

	if err := access.Require(ctx, permission); err != nil {
		return domain.User{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return domain.User{}, err
	}

	if err := s.authorizeRelated(ctx, account, principal, user, kind, uuid.Nil); err != nil {
		return domain.User{}, err
	}

	return user, nil
}
