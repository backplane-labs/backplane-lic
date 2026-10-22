package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// The Store delegates the app scenarios' storage ports to its
// repositories, so the tree's single dependency satisfies them all.

// --- users scenario ---

// UpdateUser writes every mutable column of the row.
func (s *Store) UpdateUser(ctx context.Context, user domain.User, attrs users.Attrs) error {
	return s.Users.UpdateUser(ctx, user, attrs)
}

// DeleteUser removes the row.
func (s *Store) DeleteUser(ctx context.Context, accountID, id uuid.UUID) error {
	return s.Users.DeleteUser(ctx, accountID, id)
}

// ListUsers answers the filtered page with computed statuses.
func (s *Store) ListUsers(
	ctx context.Context, accountID uuid.UUID, f users.Filter, scopeKind string,
	scopeID uuid.UUID, p paging.Params,
) ([]users.Listed, error) {
	return s.Users.ListUsers(ctx, accountID, f, scopeKind, scopeID, p)
}

// CountUsers answers the filter's total.
func (s *Store) CountUsers(
	ctx context.Context, accountID uuid.UUID, f users.Filter, scopeKind string, scopeID uuid.UUID,
) (int, error) {
	return s.Users.CountUsers(ctx, accountID, f, scopeKind, scopeID)
}

// UserIsTeammate answers whether other shares a license with user.
func (s *Store) UserIsTeammate(ctx context.Context, accountID, userID, otherID uuid.UUID) (bool, error) {
	return s.Users.UserIsTeammate(ctx, accountID, userID, otherID)
}

// UserOfLicense answers whether the user owns or is attached to the license.
func (s *Store) UserOfLicense(ctx context.Context, accountID, licenseID, userID uuid.UUID) (bool, error) {
	return s.Users.UserOfLicense(ctx, accountID, licenseID, userID)
}

// CountAdmins answers the account's admin count.
func (s *Store) CountAdmins(ctx context.Context, accountID uuid.UUID) (int, error) {
	return s.Users.CountAdmins(ctx, accountID)
}

// UpdateUserPassword writes a new password digest and clears the reset fields.
func (s *Store) UpdateUserPassword(ctx context.Context, accountID, id uuid.UUID, digest string) error {
	return s.Users.UpdateUserPassword(ctx, accountID, id, digest)
}

// SetUserPasswordReset stores the reset token digest and timestamp.
func (s *Store) SetUserPasswordReset(
	ctx context.Context, accountID, id uuid.UUID, token string, sentAt time.Time,
) error {
	return s.Users.SetUserPasswordReset(ctx, accountID, id, token, sentAt)
}

// SetUserBannedAt writes (or clears) the ban timestamp.
func (s *Store) SetUserBannedAt(ctx context.Context, accountID, id uuid.UUID, bannedAt *time.Time) error {
	return s.Users.SetUserBannedAt(ctx, accountID, id, bannedAt)
}

// DeleteUserTokens revokes every token of the user.
func (s *Store) DeleteUserTokens(ctx context.Context, accountID, userID uuid.UUID) error {
	return s.Users.DeleteUserTokens(ctx, accountID, userID)
}

// DeleteUserTokensExcept revokes every token of the user but the acting one.
func (s *Store) DeleteUserTokensExcept(ctx context.Context, accountID, userID, exceptID uuid.UUID) error {
	return s.Users.DeleteUserTokensExcept(ctx, accountID, userID, exceptID)
}

// CountGroupUsers answers the group's member count.
func (s *Store) CountGroupUsers(ctx context.Context, accountID, groupID uuid.UUID) (int, error) {
	return s.Users.CountGroupUsers(ctx, accountID, groupID)
}

// GroupByID loads a group; ErrNotFound.
func (s *Store) GroupByID(ctx context.Context, accountID, id uuid.UUID) (domain.Group, error) {
	return s.Groups.GroupByID(ctx, accountID, id)
}

// UserLicenseIDs answers the user's license ids.
func (s *Store) UserLicenseIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.Users.UserLicenseIDs(ctx, accountID, userID)
}

// UserMachineIDs answers the user's machine ids.
func (s *Store) UserMachineIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.Users.UserMachineIDs(ctx, accountID, userID)
}

// UserProductIDs answers the product ids behind the user's licenses.
func (s *Store) UserProductIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.Users.UserProductIDs(ctx, accountID, userID)
}

// GroupIDsOfUser answers the group ids the user belongs to or owns.
func (s *Store) GroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.Users.GroupIDsOfUser(ctx, accountID, userID)
}

// --- groups scenario ---

// ListGroups answers the member-scoped page.
func (s *Store) ListGroups(
	ctx context.Context, accountID uuid.UUID, memberUser uuid.NullUUID, ids []uuid.UUID, p paging.Params,
) ([]domain.Group, error) {
	return s.Groups.ListGroups(ctx, accountID, memberUser, ids, p)
}

// CountGroups answers the member-scoped total.
func (s *Store) CountGroups(ctx context.Context, accountID uuid.UUID, memberUser uuid.NullUUID, ids []uuid.UUID) (int, error) { //nolint:lll // the scope's arguments
	return s.Groups.CountGroups(ctx, accountID, memberUser, ids)
}

// InsertGroup writes a new group row.
func (s *Store) InsertGroup(ctx context.Context, group domain.Group) error {
	return s.Groups.InsertGroup(ctx, group)
}

// UpdateGroup writes every mutable column.
func (s *Store) UpdateGroup(ctx context.Context, group domain.Group) error {
	return s.Groups.UpdateGroup(ctx, group)
}

// DeleteGroup removes the group.
func (s *Store) DeleteGroup(ctx context.Context, accountID, id uuid.UUID) error {
	return s.Groups.DeleteGroup(ctx, accountID, id)
}

// ListGroupUsers answers the group's member users.
func (s *Store) ListGroupUsers(
	ctx context.Context, accountID, groupID uuid.UUID, limit int,
) ([]domain.User, error) {
	return s.Groups.ListGroupUsers(ctx, accountID, groupID, limit)
}

// ListGroupLicenseIDs answers the group's license ids.
func (s *Store) ListGroupLicenseIDs(ctx context.Context, accountID, groupID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return s.Groups.ListGroupLicenseIDs(ctx, accountID, groupID, limit)
}

// ListGroupMachineIDs answers the group's machine ids.
func (s *Store) ListGroupMachineIDs(ctx context.Context, accountID, groupID uuid.UUID, limit int) ([]uuid.UUID, error) {
	return s.Groups.ListGroupMachineIDs(ctx, accountID, groupID, limit)
}

// InsertGroupOwner attaches a user as owner.
func (s *Store) InsertGroupOwner(ctx context.Context, owner domain.GroupOwner) error {
	return s.Groups.InsertGroupOwner(ctx, owner)
}

// GetGroupOwner loads a group-owner row.
func (s *Store) GetGroupOwner(ctx context.Context, accountID, groupID, ownerID uuid.UUID) (domain.GroupOwner, error) {
	return s.Groups.GetGroupOwner(ctx, accountID, groupID, ownerID)
}

// GetGroupOwnerByUser loads the group-owner row of a user.
func (s *Store) GetGroupOwnerByUser(
	ctx context.Context, accountID, groupID, userID uuid.UUID,
) (domain.GroupOwner, error) {
	return s.Groups.GetGroupOwnerByUser(ctx, accountID, groupID, userID)
}

// ListGroupOwners answers the group's owners.
func (s *Store) ListGroupOwners(
	ctx context.Context, accountID, groupID uuid.UUID, limit int,
) ([]domain.GroupOwner, error) {
	return s.Groups.ListGroupOwners(ctx, accountID, groupID, limit)
}

// DeleteGroupOwnersByUsers detaches the given users.
func (s *Store) DeleteGroupOwnersByUsers(
	ctx context.Context, accountID, groupID uuid.UUID, userIDs []uuid.UUID,
) (int64, error) {
	return s.Groups.DeleteGroupOwnersByUsers(ctx, accountID, groupID, userIDs)
}

// OwnedGroupIDsOfUser answers the user's ownership-only group scope.
func (s *Store) OwnedGroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.Groups.OwnedGroupIDsOfUser(ctx, accountID, userID)
}

// --- tokens scenario (management face) ---

// TokenByID loads a token by its id.
func (s *Store) TokenByID(ctx context.Context, accountID, id uuid.UUID) (domain.Token, error) {
	return s.Tokens.TokenByID(ctx, accountID, id)
}

// ListTokens answers the filtered, scoped page.
func (s *Store) ListTokens(
	ctx context.Context, accountID uuid.UUID, f tokens.Filter, scopeKind string, scopeID uuid.UUID, p paging.Params,
) ([]domain.Token, error) {
	return s.Tokens.ListTokens(ctx, accountID, f, scopeKind, scopeID, p)
}

// CountTokens answers the filtered, scoped total.
func (s *Store) CountTokens(
	ctx context.Context, accountID uuid.UUID, f tokens.Filter, scopeKind string, scopeID uuid.UUID,
) (int, error) {
	return s.Tokens.CountTokens(ctx, accountID, f, scopeKind, scopeID)
}

// RegenerateToken writes the new digest (and reset expiry).
func (s *Store) RegenerateToken(
	ctx context.Context, accountID, id uuid.UUID, digest string, expiry *time.Time,
) error {
	return s.Tokens.RegenerateToken(ctx, accountID, id, digest, expiry)
}

// DeleteToken removes the row.
func (s *Store) DeleteToken(ctx context.Context, accountID, id uuid.UUID) error {
	return s.Tokens.DeleteToken(ctx, accountID, id)
}

// UserLicensedToProduct answers the product-scope membership check.
func (s *Store) UserLicensedToProduct(
	ctx context.Context, accountID, userID, productID uuid.UUID,
) (bool, error) {
	return s.Tokens.UserLicensedToProduct(ctx, accountID, userID, productID)
}

// LicenseOfProduct answers whether the license belongs to the product.
func (s *Store) LicenseOfProduct(
	ctx context.Context, accountID, licenseID, productID uuid.UUID,
) (bool, error) {
	return s.Tokens.LicenseOfProduct(ctx, accountID, licenseID, productID)
}

// PolicyByID loads a policy; ErrNotFound.
func (s *Store) PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error) {
	return s.Policies.PolicyByID(ctx, accountID, id)
}

// Compile-time proofs that the Store satisfies the scenarios' ports.
var (
	_ users.Store  = (*Store)(nil)
	_ groups.Store = (*Store)(nil)
	_ tokens.Store = (*Store)(nil)
)

// --- second factors scenario ---

// SecondFactorByID loads the user's factor.
func (s *Store) SecondFactorByID(
	ctx context.Context, accountID, userID, id uuid.UUID,
) (domain.SecondFactor, error) {
	return s.SecondFactors.SecondFactorByID(ctx, accountID, userID, id)
}

// AnySecondFactor loads the user's (only) factor.
func (s *Store) AnySecondFactor(ctx context.Context, userID uuid.UUID) (domain.SecondFactor, error) {
	return s.SecondFactors.AnySecondFactor(ctx, userID)
}

// InsertSecondFactor writes the factor.
func (s *Store) InsertSecondFactor(ctx context.Context, factor domain.SecondFactor) error {
	return s.SecondFactors.InsertSecondFactor(ctx, factor)
}

// UpdateSecondFactor writes enabled and the replay guard.
func (s *Store) UpdateSecondFactor(ctx context.Context, factor domain.SecondFactor) error {
	return s.SecondFactors.UpdateSecondFactor(ctx, factor)
}

// DeleteSecondFactor removes the factor.
func (s *Store) DeleteSecondFactor(ctx context.Context, accountID, userID, id uuid.UUID) error {
	return s.SecondFactors.DeleteSecondFactor(ctx, accountID, userID, id)
}

// ListSecondFactors answers the user's factors.
func (s *Store) ListSecondFactors(
	ctx context.Context, accountID, userID uuid.UUID, limit int,
) ([]domain.SecondFactor, error) {
	return s.SecondFactors.ListSecondFactors(ctx, accountID, userID, limit)
}
