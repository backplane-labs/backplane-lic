package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// GroupsRepo adapts the generated group queries to the groups scenario's
// storage port.
type GroupsRepo struct {
	q *db.Queries
}

// OwnedGroupIDsOfUser answers ownership without membership-only groups.
func (r GroupsRepo) OwnedGroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.OwnedGroupIdsForUser(ctx, db.OwnedGroupIdsForUserParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: owned groups: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.GroupID)
	}

	return ids, nil
}

// GroupByID loads a group.
func (r GroupsRepo) GroupByID(ctx context.Context, accountID, id uuid.UUID) (domain.Group, error) {
	row, err := r.q.GetGroupByID(ctx, db.GetGroupByIDParams{AccountID: accountID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Group{}, fmt.Errorf("postgres: group %s: %w", id, groups.ErrNotFound)
	}

	if err != nil {
		return domain.Group{}, fmt.Errorf("postgres: group %s: %w", id, err)
	}

	return groupDomain(row.ID, row.AccountID, row.Name, row.MaxUsers, row.MaxLicenses,
		row.MaxMachines, row.Metadata, row.CreatedAt, row.UpdatedAt), nil
}

// ListGroups answers the member-scoped page.
func (r GroupsRepo) ListGroups(
	ctx context.Context, accountID uuid.UUID, memberUser uuid.NullUUID, p paging.Params,
) ([]domain.Group, error) {
	rows, err := r.q.ListGroups(ctx, db.ListGroupsParams{
		AccountID:     accountID,
		MemberUser:    nullUUIDPtr(memberUser),
		MemberLicense: nil,
		Cursor:        cursorPtr(p.Cursor),
		Asc:           p.Order == paging.OrderAsc,
		Limit:         int64(p.SizeOrDefault()),
		Offset:        int64(offsetOf(p)),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list groups: %w", err)
	}

	out := make([]domain.Group, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, groupDomain(row.ID, row.AccountID, row.Name, row.MaxUsers, row.MaxLicenses,
			row.MaxMachines, row.Metadata, row.CreatedAt, row.UpdatedAt))
	}

	return out, nil
}

// CountGroups answers the member-scoped total.
func (r GroupsRepo) CountGroups(ctx context.Context, accountID uuid.UUID, memberUser uuid.NullUUID) (int, error) {
	row, err := r.q.CountGroups(ctx, db.CountGroupsParams{
		AccountID:  accountID,
		MemberUser: nullUUIDPtr(memberUser),
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count groups: %w", err)
	}

	return int(row.Total), nil
}

// InsertGroup writes a new group row.
func (r GroupsRepo) InsertGroup(ctx context.Context, group domain.Group) error {
	err := r.q.InsertGroup(ctx, db.InsertGroupParams{
		ID:          group.ID,
		AccountID:   group.AccountID,
		Name:        ref(group.Name),
		MaxUsers:    int32Ptr(group.MaxUsers),
		MaxLicenses: int32Ptr(group.MaxLicenses),
		MaxMachines: int32Ptr(group.MaxMachines),
		Metadata:    jsonMap(group.Metadata),
	})
	if err != nil {
		return mapGroupWrite(group.ID, err)
	}

	return nil
}

// UpdateGroup writes every mutable column.
func (r GroupsRepo) UpdateGroup(ctx context.Context, group domain.Group) error {
	err := r.q.UpdateGroupColumns(ctx, db.UpdateGroupColumnsParams{
		ID:          group.ID,
		AccountID:   group.AccountID,
		Name:        ref(group.Name),
		MaxUsers:    int32Ptr(group.MaxUsers),
		MaxLicenses: int32Ptr(group.MaxLicenses),
		MaxMachines: int32Ptr(group.MaxMachines),
		Metadata:    jsonMap(group.Metadata),
	})
	if err != nil {
		return mapGroupWrite(group.ID, err)
	}

	return nil
}

// DeleteGroup removes the group.
func (r GroupsRepo) DeleteGroup(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteGroup(ctx, db.DeleteGroupParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: delete group %s: %w", id, err)
	}

	return nil
}

// ListGroupUsers answers the group's member users.
func (r GroupsRepo) ListGroupUsers(
	ctx context.Context, accountID, groupID uuid.UUID, limit int,
) ([]domain.User, error) {
	rows, err := r.q.ListGroupUsers(ctx, db.ListGroupUsersParams{
		AccountID: accountID, GroupID: &groupID, Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: group users: %w", err)
	}

	out := make([]domain.User, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, userDomain(row.ID, row.AccountID, row.FirstName, row.LastName, row.Email,
			row.PasswordDigest, ref(row.Role), row.Permissions, row.GroupID, row.Metadata,
			nil, nil, row.BannedAt, row.CreatedAt, row.UpdatedAt))
	}

	return out, nil
}

// ListGroupLicenseIDs answers the group's license ids.
func (r GroupsRepo) ListGroupLicenseIDs(
	ctx context.Context, accountID, groupID uuid.UUID, limit int,
) ([]uuid.UUID, error) {
	rows, err := r.q.ListGroupLicenses(ctx, db.ListGroupLicensesParams{
		AccountID: accountID, GroupID: &groupID, Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: group licenses: %w", err)
	}

	out := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, row.ID)
	}

	return out, nil
}

// ListGroupMachineIDs answers the group's machine ids.
func (r GroupsRepo) ListGroupMachineIDs(
	ctx context.Context, accountID, groupID uuid.UUID, limit int,
) ([]uuid.UUID, error) {
	rows, err := r.q.ListGroupMachines(ctx, db.ListGroupMachinesParams{
		AccountID: accountID, GroupID: &groupID, Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: group machines: %w", err)
	}

	out := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, row.ID)
	}

	return out, nil
}

// InsertGroupOwner attaches a user as owner.
func (r GroupsRepo) InsertGroupOwner(ctx context.Context, owner domain.GroupOwner) error {
	err := r.q.InsertGroupOwner(ctx, db.InsertGroupOwnerParams{
		ID:        owner.ID,
		AccountID: owner.AccountID,
		GroupID:   owner.GroupID,
		UserID:    owner.UserID,
	})
	if err != nil {
		return mapGroupWrite(owner.ID, err)
	}

	return nil
}

// GetGroupOwner loads a group-owner row.
func (r GroupsRepo) GetGroupOwner(
	ctx context.Context, accountID, groupID, ownerID uuid.UUID,
) (domain.GroupOwner, error) {
	row, err := r.q.GetGroupOwner(ctx, db.GetGroupOwnerParams{
		AccountID: accountID, GroupID: groupID, ID: ownerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GroupOwner{}, fmt.Errorf("postgres: owner %s: %w", ownerID, groups.ErrNotFound)
	}

	if err != nil {
		return domain.GroupOwner{}, fmt.Errorf("postgres: owner %s: %w", ownerID, err)
	}

	return ownerDomain(row.ID, row.AccountID, row.GroupID, row.UserID, row.CreatedAt, row.UpdatedAt), nil
}

// GetGroupOwnerByUser loads the group-owner row of a user.
func (r GroupsRepo) GetGroupOwnerByUser(
	ctx context.Context, accountID, groupID, userID uuid.UUID,
) (domain.GroupOwner, error) {
	row, err := r.q.GetGroupOwnerByUser(ctx, db.GetGroupOwnerByUserParams{
		AccountID: accountID, GroupID: groupID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GroupOwner{}, fmt.Errorf("postgres: owner of %s: %w", userID, groups.ErrNotFound)
	}

	if err != nil {
		return domain.GroupOwner{}, fmt.Errorf("postgres: owner of %s: %w", userID, err)
	}

	return ownerDomain(row.ID, row.AccountID, row.GroupID, row.UserID, row.CreatedAt, row.UpdatedAt), nil
}

// ListGroupOwners answers the group's owners.
func (r GroupsRepo) ListGroupOwners(
	ctx context.Context, accountID, groupID uuid.UUID, limit int,
) ([]domain.GroupOwner, error) {
	rows, err := r.q.ListGroupOwners(ctx, db.ListGroupOwnersParams{
		AccountID: accountID, GroupID: groupID, Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: group owners: %w", err)
	}

	out := make([]domain.GroupOwner, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, ownerDomain(row.ID, row.AccountID, row.GroupID, row.UserID, row.CreatedAt, row.UpdatedAt))
	}

	return out, nil
}

// DeleteGroupOwnersByUsers detaches the given users.
func (r GroupsRepo) DeleteGroupOwnersByUsers(
	ctx context.Context, accountID, groupID uuid.UUID, userIDs []uuid.UUID,
) (int64, error) {
	n, err := r.q.DeleteGroupOwnersByUsers(ctx, db.DeleteGroupOwnersByUsersParams{
		AccountID: accountID, GroupID: groupID, UserIds: userIDs,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: detach owners: %w", err)
	}

	return n, nil
}

func groupDomain(
	id, accountID uuid.UUID,
	name *string,
	maxUsers, maxLicenses, maxMachines *int32,
	metadata []byte,
	createdAt, updatedAt time.Time,
) domain.Group {
	meta := map[string]any{}
	_ = json.Unmarshal(metadata, &meta)

	return domain.Group{
		ID:          id,
		AccountID:   accountID,
		Name:        deref(name),
		MaxUsers:    intPtr(maxUsers),
		MaxLicenses: intPtr(maxLicenses),
		MaxMachines: intPtr(maxMachines),
		Metadata:    meta,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func ownerDomain(
	id, accountID, groupID, userID uuid.UUID,
	createdAt, updatedAt time.Time,
) domain.GroupOwner {
	return domain.GroupOwner{
		ID:        id,
		AccountID: accountID,
		GroupID:   groupID,
		UserID:    userID,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}

	n := int(*v)

	return &n
}

func int32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}

	// The wire's own limit bounds keep the conversion value-preserving.
	if *v > math.MaxInt32 || *v < math.MinInt32 {
		return nil
	}

	n := int32(*v)

	return &n
}

// offsetOf answers the offset-mode window offset.
func offsetOf(p paging.Params) int {
	if p.Mode != paging.ModeOffset {
		return 0
	}

	return (p.Number - 1) * p.SizeOrDefault()
}

func mapGroupWrite(id uuid.UUID, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: write group %s: unique violation: %w", id, err)
	}

	return fmt.Errorf("postgres: write group %s: %w", id, err)
}
