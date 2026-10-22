// Package groups is the group scenario: CRUD, the member relationship
// indexes and the owners join resource.
package groups

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound

// Store is the scenario's consumer-side storage port.
//
//nolint:interfacebloat // one port per scenario is the architecture's shape
type Store interface {
	// Do runs fn in one transaction; the writes and the webhook
	// events of one action commit together.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
	// GroupByID loads a group; ErrNotFound.
	GroupByID(ctx context.Context, accountID, id uuid.UUID) (domain.Group, error)
	// ListGroups answers the principal-scoped page.
	ListGroups(ctx context.Context, accountID uuid.UUID, memberUser uuid.NullUUID, ids []uuid.UUID, p paging.Params) ([]domain.Group, error) //nolint:lll // the scope's arguments
	// CountGroups answers the scope's total.
	CountGroups(ctx context.Context, accountID uuid.UUID, memberUser uuid.NullUUID, ids []uuid.UUID) (int, error)
	// InsertGroup writes a new group row.
	InsertGroup(ctx context.Context, group domain.Group) error
	// UpdateGroup writes every mutable column.
	UpdateGroup(ctx context.Context, group domain.Group) error
	// DeleteGroup removes the group (members' group_id nulled).
	DeleteGroup(ctx context.Context, accountID, id uuid.UUID) error
	// ListGroupUsers answers the group's member users.
	ListGroupUsers(ctx context.Context, accountID, groupID uuid.UUID, limit int) ([]domain.User, error)
	// ListGroupLicenseIDs answers the group's license ids.
	ListGroupLicenseIDs(ctx context.Context, accountID, groupID uuid.UUID, limit int) ([]uuid.UUID, error)
	// ListGroupMachineIDs answers the group's machine ids.
	ListGroupMachineIDs(ctx context.Context, accountID, groupID uuid.UUID, limit int) ([]uuid.UUID, error)
	// InsertGroupOwner attaches a user as owner.
	InsertGroupOwner(ctx context.Context, owner domain.GroupOwner) error
	// GetGroupOwner loads a group-owner row.
	GetGroupOwner(ctx context.Context, accountID, groupID, ownerID uuid.UUID) (domain.GroupOwner, error)
	// GetGroupOwnerByUser loads the group-owner row of a user.
	GetGroupOwnerByUser(ctx context.Context, accountID, groupID, userID uuid.UUID) (domain.GroupOwner, error)
	// ListGroupOwners answers the group's owners.
	ListGroupOwners(ctx context.Context, accountID, groupID uuid.UUID, limit int) ([]domain.GroupOwner, error)
	// DeleteGroupOwnersByUsers detaches the given users; it answers the
	// number of rows removed.
	DeleteGroupOwnersByUsers(ctx context.Context, accountID, groupID uuid.UUID, userIDs []uuid.UUID) (int64, error)
	// UserByID loads a user (owner attach validates them).
	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	// GroupIDsOfUser answers the group ids the user belongs to or owns.
	GroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// OwnedGroupIDsOfUser excludes membership-only groups.
	OwnedGroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// UserLicenseIDs answers the user's license ids.
	UserLicenseIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// UserProductIDs answers the product ids behind the user's licenses.
	UserProductIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
}

// maxNameLen is the group name's length limit.
const maxNameLen = 255

// docsOrigin is the reference's hardcoded docs origin of validation
// error links.
const docsOrigin = "https://keygen.sh/docs/api"

// groupOwnerUnprocessable builds a group-owner 422 with its docs link.
func groupOwnerUnprocessable(code, detail string) *apperr.T {
	t := apperr.Unprocessable(code, detail, "/data/relationships/user")
	t.About = docsOrigin + "/group-owners/#group-owners-object-relationships-user"

	return t
}

// Scenario runs the group scenarios.
type Scenario struct {
	store  Store
	events events.Publisher
	clock  func() time.Time
}

// New builds the scenario.
func New(store Store, publisher events.Publisher, clock func() time.Time) Scenario {
	return Scenario{store: store, events: publisher, clock: clock}
}

// Page is one result page with its total.
type Page struct {
	Rows  []domain.Group
	Total int
}

// OptionalInt is a tri-state integer attribute: absent, JSON null
// (clears the limit) or a value.
type OptionalInt struct {
	Set   bool
	Null  bool
	Value int
}

// value answers the attribute as a column value (nil without a limit).
func (o OptionalInt) value() *int {
	if !o.Set || o.Null {
		return nil
	}

	return &o.Value
}

// Attrs is the decoded write payload of POST/PATCH /groups.
type Attrs struct {
	Name        *string
	MaxUsers    OptionalInt
	MaxLicenses OptionalInt
	MaxMachines OptionalInt
	Metadata    map[string]any
}

// Show loads one group under the GroupPolicy.
func (s Scenario) Show(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.Group, error) {
	if err := access.Require(ctx, "group.read"); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the port's own error
	}

	group, err := s.load(ctx, account, principal, id)
	if err != nil {
		return domain.Group{}, err
	}

	if err := access.GroupShow(principal, group, s.memberOrOwner(ctx, account.ID, principal)); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the authorization's own error
	}

	return group, nil
}

// List answers the principal-scoped page.
func (s Scenario) List(
	ctx context.Context, account domain.Account, principal access.Principal, p paging.Params,
) (Page, error) {
	return s.list(ctx, account, principal, nil, p)
}

// Search pages the account's groups among a search's matches.
func (s Scenario) Search(
	ctx context.Context, account domain.Account, principal access.Principal, matches []uuid.UUID, p paging.Params,
) (Page, error) {
	return s.list(ctx, account, principal, matches, p)
}

// list pages the principal's scope; matches (nil: all) narrows it.
func (s Scenario) list(
	ctx context.Context, account domain.Account, principal access.Principal, matches []uuid.UUID, p paging.Params,
) (Page, error) {
	if err := access.Require(ctx, "group.read"); err != nil {
		return Page{}, err //nolint:wrapcheck // the port's own error
	}

	member := uuid.NullUUID{}
	if principal.IsUser() && principal.Role == domain.RoleUser {
		// The authorized scope of a plain user: her groups.
		member = uuid.NullUUID{UUID: principal.BearerID, Valid: true}
	}

	rows, err := s.store.ListGroups(ctx, account.ID, member, matches, p)
	if err != nil {
		return Page{}, fmt.Errorf("groups: list: %w", err)
	}

	total := len(rows)
	if p.Mode == paging.ModeOffset {
		total, err = s.store.CountGroups(ctx, account.ID, member, matches)
		if err != nil {
			return Page{}, fmt.Errorf("groups: count: %w", err)
		}
	}

	return Page{Rows: rows, Total: total}, nil
}

// Create validates and writes a new group.
func (s Scenario) Create(
	ctx context.Context, account domain.Account, principal access.Principal, attrs Attrs,
) (domain.Group, error) {
	if err := access.Require(ctx, "group.create"); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the port's own error
	}

	if err := access.GroupWrite(principal); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the authorization's own error
	}

	if attrs.Name == nil {
		return domain.Group{}, apperr.BadRequestSource("is missing", "/data/attributes/name")
	}

	if *attrs.Name == "" {
		return domain.Group{}, apperr.BadRequestSource("cannot be blank", "/data/attributes/name")
	}

	if len(*attrs.Name) > maxNameLen {
		return domain.Group{}, apperr.Unprocessable(
			"NAME_TOO_LONG", "is too long (maximum is 255 characters)", "/data/attributes/name",
		)
	}

	now := s.clock()

	group := domain.Group{
		ID:          uuid.New(),
		AccountID:   account.ID,
		Name:        *attrs.Name,
		MaxUsers:    attrs.MaxUsers.value(),
		MaxLicenses: attrs.MaxLicenses.value(),
		MaxMachines: attrs.MaxMachines.value(),
		Metadata:    attrs.Metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := metadataValidation(group.Metadata); err != nil {
		return domain.Group{}, err
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.InsertGroup(ctx, group); err != nil {
			return fmt.Errorf("groups: create: %w", err)
		}

		return s.events.Publish(ctx, "group.created", group)
	}); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the transaction's own error
	}

	return group, nil
}

// Update applies the write payload.
func (s Scenario) Update(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, attrs Attrs,
) (domain.Group, error) {
	if err := access.Require(ctx, "group.update"); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the port's own error
	}

	group, err := s.load(ctx, account, principal, id)
	if err != nil {
		return domain.Group{}, err
	}

	if err := access.GroupWrite(principal); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the authorization's own error
	}

	if attrs.Name != nil {
		if *attrs.Name == "" {
			return domain.Group{}, apperr.BadRequestSource("cannot be blank", "/data/attributes/name")
		}

		if len(*attrs.Name) > maxNameLen {
			return domain.Group{}, apperr.Unprocessable(
				"NAME_TOO_LONG", "is too long (maximum is 255 characters)", "/data/attributes/name",
			)
		}

		group.Name = *attrs.Name
	}

	if attrs.MaxUsers.Set {
		group.MaxUsers = attrs.MaxUsers.value()
	}

	if attrs.MaxLicenses.Set {
		group.MaxLicenses = attrs.MaxLicenses.value()
	}

	if attrs.MaxMachines.Set {
		group.MaxMachines = attrs.MaxMachines.value()
	}

	if attrs.Metadata != nil {
		group.Metadata = attrs.Metadata

		if err := metadataValidation(group.Metadata); err != nil {
			return domain.Group{}, err
		}
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateGroup(ctx, group); err != nil {
			return fmt.Errorf("groups: update: %w", err)
		}

		return s.events.Publish(ctx, "group.updated", group)
	}); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the transaction's own error
	}

	return group, nil
}

// Delete removes the group.
func (s Scenario) Delete(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) error {
	if err := access.Require(ctx, "group.delete"); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	group, err := s.load(ctx, account, principal, id)
	if err != nil {
		return err
	}

	if err := access.GroupWrite(principal); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.DeleteGroup(ctx, account.ID, group.ID); err != nil {
			return fmt.Errorf("groups: delete: %w", err)
		}

		return s.events.Publish(ctx, "group.deleted", group)
	}); err != nil {
		return err //nolint:wrapcheck // the transaction's own error
	}

	return nil
}

// Users answers the group's member users (limit mode size).
func (s Scenario) Users(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, limit int,
) ([]domain.User, error) {
	visible, err := s.readRelationship(ctx, account, principal, id, "group.users.read")
	if err != nil {
		return nil, err
	}

	if !visible {
		return []domain.User{}, nil
	}

	return s.store.ListGroupUsers(ctx, account.ID, id, limit) //nolint:wrapcheck // the port's own error
}

// LicenseIDs answers the group's license ids.
func (s Scenario) LicenseIDs(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, limit int,
) ([]uuid.UUID, error) {
	visible, err := s.readRelationship(ctx, account, principal, id, "group.licenses.read")
	if err != nil {
		return nil, err
	}

	if !visible {
		return []uuid.UUID{}, nil
	}

	return s.store.ListGroupLicenseIDs(ctx, account.ID, id, limit) //nolint:wrapcheck // the port's own error
}

// MachineIDs answers the group's machine ids.
func (s Scenario) MachineIDs(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, limit int,
) ([]uuid.UUID, error) {
	visible, err := s.readRelationship(ctx, account, principal, id, "group.machines.read")
	if err != nil {
		return nil, err
	}

	if !visible {
		return []uuid.UUID{}, nil
	}

	return s.store.ListGroupMachineIDs(ctx, account.ID, id, limit) //nolint:wrapcheck // the port's own error
}

// Owners answers the group's owners (the join resource).
func (s Scenario) Owners(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, limit int,
) ([]domain.GroupOwner, error) {
	group, err := s.readRelationshipGroup(ctx, account, principal, id, "group.owners.read")
	if err != nil {
		return nil, err
	}

	if err := access.GroupOwnersRead(principal, group, s.memberOrOwner(ctx, account.ID, principal)); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	return s.store.ListGroupOwners(ctx, account.ID, id, limit) //nolint:wrapcheck // the port's own error
}

// Owner loads one group-owner row.
func (s Scenario) Owner(
	ctx context.Context, account domain.Account, principal access.Principal, groupID, ownerID uuid.UUID,
) (domain.GroupOwner, error) {
	group, err := s.readRelationshipGroup(ctx, account, principal, groupID, "group.owners.read")
	if err != nil {
		return domain.GroupOwner{}, err
	}

	if err := access.GroupOwnersRead(principal, group, s.memberOrOwner(ctx, account.ID, principal)); err != nil {
		return domain.GroupOwner{}, err //nolint:wrapcheck // the authorization's own error
	}

	owner, err := s.store.GetGroupOwner(ctx, account.ID, groupID, ownerID)
	if errors.Is(err, ErrNotFound) {
		return domain.GroupOwner{}, apperr.NotFound("group owner", ownerID.String())
	}

	if err != nil {
		return domain.GroupOwner{}, fmt.Errorf("groups: owner: %w", err)
	}

	return owner, nil
}

// AttachOwners attaches users as the group's owners, answering the
// created join rows.
func (s Scenario) AttachOwners(
	ctx context.Context, account domain.Account, principal access.Principal, groupID uuid.UUID,
	userIDs []uuid.UUID,
) ([]domain.GroupOwner, error) {
	if err := access.Require(ctx, "group.owners.attach"); err != nil {
		return nil, err //nolint:wrapcheck // the port's own error
	}

	group, err := s.load(ctx, account, principal, groupID)
	if err != nil {
		return nil, err
	}

	if err := access.GroupOwnersWrite(principal); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	attached := make([]domain.GroupOwner, 0, len(userIDs))
	now := s.clock()

	attach := func(ctx context.Context) error {
		for _, userID := range userIDs {
			if _, err := s.store.UserByID(ctx, account.ID, userID); err != nil {
				if errors.Is(err, ErrNotFound) {
					return groupOwnerUnprocessable("USER_NOT_FOUND", "must exist")
				}

				return err //nolint:wrapcheck // the port's own error
			}

			if _, err := s.store.GetGroupOwnerByUser(ctx, account.ID, group.ID, userID); err == nil {
				return groupOwnerUnprocessable("USER_TAKEN", "already exists")
			} else if !errors.Is(err, ErrNotFound) {
				return err //nolint:wrapcheck // the port's own error
			}

			owner := domain.GroupOwner{
				ID:        uuid.New(),
				AccountID: account.ID,
				GroupID:   group.ID,
				UserID:    userID,
				CreatedAt: now,
				UpdatedAt: now,
			}

			if err := s.store.InsertGroupOwner(ctx, owner); err != nil {
				return fmt.Errorf("groups: attach owner: %w", err)
			}

			attached = append(attached, owner)
		}

		return s.events.Publish(ctx, "group.owners.attached", attached)
	}

	if err := s.store.Do(ctx, attach); err != nil {
		return nil, err //nolint:wrapcheck // the transaction's own error
	}

	return attached, nil
}

// DetachOwners detaches the given owner users; an unknown one is a 422
// pointing at its body slot.
func (s Scenario) DetachOwners(
	ctx context.Context, account domain.Account, principal access.Principal, groupID uuid.UUID,
	userIDs []uuid.UUID,
) error {
	if err := access.Require(ctx, "group.owners.detach"); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	group, err := s.load(ctx, account, principal, groupID)
	if err != nil {
		return err
	}

	if err := access.GroupOwnersWrite(principal); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	for index, userID := range userIDs {
		if _, err := s.store.GetGroupOwnerByUser(ctx, account.ID, group.ID, userID); errors.Is(err, ErrNotFound) {
			return &apperr.T{
				Status: http.StatusUnprocessableEntity,
				Title:  "Unprocessable entity",
				Detail: fmt.Sprintf("owner relationship for user '%s' not found", userID),
				Source: apperr.Source{Pointer: fmt.Sprintf("/data/%d", index)},
			}
		} else if err != nil {
			return err //nolint:wrapcheck // the port's own error
		}
	}

	// The join rows are collected before the delete so the event
	// carries them (the reference broadcasts the destroyed records).
	detached := make([]domain.GroupOwner, 0, len(userIDs))

	for _, userID := range userIDs {
		owner, err := s.store.GetGroupOwnerByUser(ctx, account.ID, group.ID, userID)
		if err != nil {
			return err //nolint:wrapcheck // the port's own error
		}

		detached = append(detached, owner)
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if _, err := s.store.DeleteGroupOwnersByUsers(ctx, account.ID, group.ID, userIDs); err != nil {
			return fmt.Errorf("groups: detach owners: %w", err)
		}

		return s.events.Publish(ctx, "group.owners.detached", detached)
	}); err != nil {
		return err //nolint:wrapcheck // the transaction's own error
	}

	return nil
}

// load mirrors GroupsController#set_group's account-wide find. Existing
// unrelated groups reach the policy and return 403, not a scoped-find 404.
func (s Scenario) load(
	ctx context.Context, account domain.Account, _ access.Principal, id uuid.UUID,
) (domain.Group, error) {
	group, err := s.store.GroupByID(ctx, account.ID, id)
	if errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return domain.Group{}, apperr.NotFound("group", id.String())
	}

	if err != nil {
		return domain.Group{}, fmt.Errorf("groups: load: %w", err)
	}

	return group, nil
}

// memberOrOwner answers the user-bearer membership-or-ownership test of
// GroupShow.
func (s Scenario) memberOrOwner(
	ctx context.Context, accountID uuid.UUID, principal access.Principal,
) func(domain.Group) bool {
	return func(group domain.Group) bool {
		ids, err := s.store.GroupIDsOfUser(ctx, accountID, principal.BearerID)
		if err != nil {
			return false
		}

		for _, id := range ids {
			if id == group.ID {
				return true
			}
		}

		return false
	}
}

// readRelationship authorizes a contents index. Plain users see only
// groups they own, not groups they merely belong to. An owner querying a
// membership-only group sees an empty scoped relation, like the reference.
func (s Scenario) readRelationship(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, permission string,
) (bool, error) {
	if _, err := s.readRelationshipGroup(ctx, account, principal, id, permission); err != nil {
		return false, err
	}

	if principal.Role != domain.RoleUser {
		return true, nil
	}

	owned, err := s.store.OwnedGroupIDsOfUser(ctx, account.ID, principal.BearerID)
	if err != nil {
		return false, fmt.Errorf("groups: owned groups: %w", err)
	}

	if len(owned) == 0 {
		return false, apperr.ForbiddenReason(access.ReasonAllResources)
	}

	return slices.Contains(owned, id), nil
}

func (s Scenario) readRelationshipGroup(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID, permission string,
) (domain.Group, error) {
	group, err := s.load(ctx, account, principal, id)
	if err != nil {
		return domain.Group{}, err
	}
	// Relationship controllers use authorized_scope for their parent group;
	// GroupsController's direct member routes intentionally do not.
	if principal.Role == domain.RoleUser {
		ids, err := s.store.GroupIDsOfUser(ctx, account.ID, principal.BearerID)
		if err != nil {
			return domain.Group{}, fmt.Errorf("groups: scope: %w", err)
		}

		if !slices.Contains(ids, id) {
			return domain.Group{}, apperr.NotFound("group", id.String())
		}
	}

	if principal.IsLicense() {
		if principal.License == nil || !principal.License.GroupID.Valid || principal.License.GroupID.UUID != id {
			return domain.Group{}, apperr.NotFound("group", id.String())
		}
	}

	if err := access.Require(ctx, permission); err != nil {
		return domain.Group{}, err //nolint:wrapcheck // the port's own error
	}

	return group, nil
}

// LicenseGroup resolves and authorizes the group of the license
// relationship routes.
func (s Scenario) LicenseGroup(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.Group, error) {
	return s.readRelationshipGroup(ctx, account, principal, id, "group.licenses.read")
}

func metadataValidation(metadata map[string]any) error {
	if detail := domain.MetadataViolation(metadata); detail != "" {
		return apperr.Validation("METADATA_TOO_LONG", detail,
			"/data/attributes/metadata", "groups/#groups-object-attrs-metadata")
	}

	return nil
}
