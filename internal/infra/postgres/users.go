package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// UsersRepo adapts the generated user queries to the users, authn and
// secondfactors scenarios' storage ports.
type UsersRepo struct {
	q *db.Queries
}

// UserByID loads a user by id.
func (r UsersRepo) UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error) {
	row, err := r.q.GetUserByID(ctx, db.GetUserByIDParams{AccountID: accountID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, fmt.Errorf("postgres: user %s: %w", id, users.ErrNotFound)
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("postgres: user %s: %w", id, err)
	}

	return userDomain(row.ID, row.AccountID, row.FirstName, row.LastName, row.Email,
		row.PasswordDigest, ref(row.Role), row.Permissions, row.GroupID, row.Metadata,
		row.PasswordResetToken, row.PasswordResetSentAt, row.BannedAt,
		row.CreatedAt, row.UpdatedAt), nil
}

// UserByEmail loads a user by (downcased) email.
func (r UsersRepo) UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error) {
	row, err := r.q.GetUserByEmail(ctx, db.GetUserByEmailParams{AccountID: accountID, Email: &email})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, fmt.Errorf("postgres: user %s: %w", email, users.ErrNotFound)
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("postgres: user %s: %w", email, err)
	}

	return userDomain(row.ID, row.AccountID, row.FirstName, row.LastName, row.Email,
		row.PasswordDigest, ref(row.Role), row.Permissions, row.GroupID, row.Metadata,
		row.PasswordResetToken, row.PasswordResetSentAt, row.BannedAt,
		row.CreatedAt, row.UpdatedAt), nil
}

// InsertUser writes a new user row.
func (r UsersRepo) InsertUser(ctx context.Context, user domain.User) error {
	err := r.q.InsertUser(ctx, db.InsertUserParams{
		ID:             user.ID,
		AccountID:      user.AccountID,
		FirstName:      ref(user.FirstName),
		LastName:       ref(user.LastName),
		Email:          ref(user.Email),
		PasswordDigest: ref(user.PasswordDigest),
		Role:           string(user.Role),
		Permissions:    jsonList(nil), // EE permission overrides; CE stores the wildcard default
		GroupID:        nullUUIDPtr(user.GroupID),
		Metadata:       jsonMap(user.Metadata),
	})
	if err != nil {
		return mapUserWrite(user.ID, err)
	}

	return nil
}

// UpdateUser writes only addressed fields, never stale reset or ban state.
func (r UsersRepo) UpdateUser(ctx context.Context, user domain.User, attrs users.Attrs) error {
	err := r.q.UpdateUserColumns(ctx, db.UpdateUserColumnsParams{
		ID: user.ID, AccountID: user.AccountID,
		FirstName: ref(user.FirstName), FirstNameSet: boolRef(attrs.FirstName != nil),
		LastName: ref(user.LastName), LastNameSet: boolRef(attrs.LastName != nil),
		Email: ref(user.Email), EmailSet: boolRef(attrs.Email != nil),
		PasswordDigest: ref(user.PasswordDigest), PasswordSet: boolRef(attrs.PasswordSet || attrs.Password != nil),
		Role: string(user.Role), RoleSet: boolRef(attrs.Role != ""),
		GroupID: nullUUIDPtr(user.GroupID), GroupSet: boolRef(attrs.GroupSet),
		Metadata: jsonMap(user.Metadata), MetadataSet: boolRef(attrs.Metadata != nil),
	})
	if err != nil {
		return mapUserWrite(user.ID, err)
	}

	return nil
}

// DeleteUser removes the row (licenses survive, owner nulled).
func (r UsersRepo) DeleteUser(ctx context.Context, accountID, id uuid.UUID) error {
	if err := r.q.DeleteUser(ctx, db.DeleteUserParams{AccountID: accountID, ID: id}); err != nil {
		return fmt.Errorf("postgres: delete user %s: %w", id, err)
	}

	return nil
}

// UserHasActiveLicense answers whether the user owns or is attached to
// a license with activity within the last 90 days.
func (r UsersRepo) UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	cutoff := time.Now().AddDate(0, 0, -90)

	row, err := r.q.UserHasActiveLicense(ctx, db.UserHasActiveLicenseParams{
		AccountID: accountID, ID: id, Cutoff: &cutoff,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: user %s active license: %w", id, err)
	}

	return row.Active, nil
}

// EnabledSecondFactorSecret loads the user's enabled TOTP secret.
func (r UsersRepo) EnabledSecondFactorSecret(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	row, err := r.q.GetEnabledSecondFactorSecret(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("postgres: second factor of %s: %w", userID, err)
	}

	return row.Secret, true, nil
}

// ListUsers answers the filtered, paginated page with each row's
// computed status.
func (r UsersRepo) ListUsers(
	ctx context.Context, accountID uuid.UUID, f users.Filter, scopeKind string,
	scopeID uuid.UUID, p paging.Params,
) ([]users.Listed, error) {
	rows, err := r.q.ListUsers(ctx, listUsersParams(accountID, f, scopeKind, scopeID, p))
	if err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}

	out := make([]users.Listed, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, users.Listed{
			User: userDomain(row.ID, row.AccountID, row.FirstName, row.LastName, row.Email,
				row.PasswordDigest, ref(row.Role), row.Permissions, row.GroupID, row.Metadata,
				nil, nil, row.BannedAt, row.CreatedAt, row.UpdatedAt),
			Status: row.Status,
		})
	}

	return out, nil
}

// CountUsers answers the filter's total (offset pagination meta).
func (r UsersRepo) CountUsers(
	ctx context.Context, accountID uuid.UUID, f users.Filter, scopeKind string, scopeID uuid.UUID,
) (int, error) {
	scope := (*string)(nil)
	if scopeKind != "" {
		scope = &scopeKind
	}

	params := db.CountUsersParams{
		AccountID: accountID,
		Roles:     f.Roles,
		GroupID:   nullUUIDPtr(f.GroupID),
		ProductID: nullUUIDPtr(f.ProductID),
		LicenseID: nullUUIDPtr(f.LicenseID),
		Status:    ref(f.Status),
		Assigned:  f.Assigned,
		Metadata:  jsonMap(f.Metadata),
		ScopeKind: scope,
		ScopeID:   scopeID,
	}

	row, err := r.q.CountUsers(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("postgres: count users: %w", err)
	}

	return int(row.Total), nil
}

// UserIsTeammate answers whether other shares a license with user.
func (r UsersRepo) UserIsTeammate(ctx context.Context, accountID, userID, otherID uuid.UUID) (bool, error) {
	// Implemented on top of the license membership queries: a teammate
	// is any user attached to or owning one of the user's licenses.
	row, err := r.q.UserIsTeammate(ctx, db.UserIsTeammateParams{
		AccountID: accountID, UserID: userID, OtherID: otherID,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: teammate check: %w", err)
	}

	return row.Teammate, nil
}

// UserOfLicense answers whether the user owns or is attached to the
// license.
func (r UsersRepo) UserOfLicense(ctx context.Context, accountID, licenseID, userID uuid.UUID) (bool, error) {
	row, err := r.q.UserOfLicense(ctx, db.UserOfLicenseParams{
		AccountID: accountID, LicenseID: licenseID, UserID: userID,
	})
	if err != nil {
		return false, fmt.Errorf("postgres: license membership: %w", err)
	}

	return row.Member, nil
}

// CountAdmins answers the account's admin count (the last-admin guard).
func (r UsersRepo) CountAdmins(ctx context.Context, accountID uuid.UUID) (int, error) {
	row, err := r.q.CountAdmins(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("postgres: count admins: %w", err)
	}

	return int(row.Total), nil
}

// UpdateUserPassword writes the new digest and clears the reset fields.
func (r UsersRepo) UpdateUserPassword(
	ctx context.Context, accountID, id uuid.UUID, digest string,
) error {
	err := r.q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		AccountID: accountID, ID: id, PasswordDigest: &digest,
	})
	if err != nil {
		return fmt.Errorf("postgres: update password %s: %w", id, err)
	}

	return nil
}

// SetUserPasswordReset stores the reset token digest and timestamp.
func (r UsersRepo) SetUserPasswordReset(
	ctx context.Context, accountID, id uuid.UUID, token string, sentAt time.Time,
) error {
	err := r.q.SetUserPasswordReset(ctx, db.SetUserPasswordResetParams{
		AccountID:           accountID,
		ID:                  id,
		PasswordResetToken:  &token,
		PasswordResetSentAt: &sentAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: set password reset %s: %w", id, err)
	}

	return nil
}

// SetUserBannedAt writes (or clears) the ban timestamp.
func (r UsersRepo) SetUserBannedAt(
	ctx context.Context, accountID, id uuid.UUID, bannedAt *time.Time,
) error {
	err := r.q.SetUserBannedAt(ctx, db.SetUserBannedAtParams{
		AccountID: accountID, ID: id, BannedAt: bannedAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: set banned_at %s: %w", id, err)
	}

	return nil
}

// DeleteUserTokens revokes every token of the user.
func (r UsersRepo) DeleteUserTokens(ctx context.Context, accountID, userID uuid.UUID) error {
	err := r.q.DeleteUserTokens(ctx, db.DeleteUserTokensParams{AccountID: accountID, UserID: &userID})
	if err != nil {
		return fmt.Errorf("postgres: revoke tokens of %s: %w", userID, err)
	}

	return nil
}

// DeleteUserTokensExcept revokes every token of the user but the acting
// one.
func (r UsersRepo) DeleteUserTokensExcept(
	ctx context.Context, accountID, userID, exceptID uuid.UUID,
) error {
	err := r.q.DeleteUserTokensExcept(ctx, db.DeleteUserTokensExceptParams{
		AccountID: accountID, UserID: &userID, ExceptID: exceptID,
	})
	if err != nil {
		return fmt.Errorf("postgres: revoke tokens of %s: %w", userID, err)
	}

	return nil
}

// CountGroupUsers answers the group's member count (limit checks).
func (r UsersRepo) CountGroupUsers(ctx context.Context, accountID, groupID uuid.UUID) (int, error) {
	row, err := r.q.CountGroupUsers(ctx, db.CountGroupUsersParams{
		AccountID: accountID, GroupID: &groupID,
	})
	if err != nil {
		return 0, fmt.Errorf("postgres: count group users: %w", err)
	}

	return int(row.Total), nil
}

// UserLicenseIDs answers the ids of the user's licenses (owned or
// attached).
func (r UsersRepo) UserLicenseIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.UserLicenseIds(ctx, db.UserLicenseIdsParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: user licenses %s: %w", userID, err)
	}

	out := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, row.ID)
	}

	return out, nil
}

// UserMachineIDs answers the ids of the user's machines.
func (r UsersRepo) UserMachineIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.UserMachineIds(ctx, db.UserMachineIdsParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: user machines %s: %w", userID, err)
	}

	out := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, row.ID)
	}

	return out, nil
}

// UserProductIDs answers the ids of the products behind the user's
// licenses.
func (r UsersRepo) UserProductIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.UserProductIds(ctx, db.UserProductIdsParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: user products %s: %w", userID, err)
	}

	out := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, row.ID)
	}

	return out, nil
}

// GroupIDsOfUser answers the ids of the groups the user belongs to
// (membership via group_id plus owned groups).
func (r UsersRepo) GroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.GroupIdsOfUser(ctx, db.GroupIdsOfUserParams{AccountID: accountID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("postgres: groups of user %s: %w", userID, err)
	}

	out := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, row.ID)
	}

	return out, nil
}

func listUsersParams(
	accountID uuid.UUID, f users.Filter, scopeKind string, scopeID uuid.UUID, p paging.Params,
) db.ListUsersParams {
	cutoff := time.Now().AddDate(0, 0, -90)

	size := p.SizeOrDefault()

	offset := 0
	if p.Mode == paging.ModeOffset {
		offset = (p.Number - 1) * size
	}

	scope := (*string)(nil)
	if scopeKind != "" {
		scope = &scopeKind
	}

	return db.ListUsersParams{
		Cutoff:    cutoff,
		AccountID: accountID,
		Roles:     f.Roles,
		GroupID:   nullUUIDPtr(f.GroupID),
		ProductID: nullUUIDPtr(f.ProductID),
		LicenseID: nullUUIDPtr(f.LicenseID),
		Status:    ref(f.Status),
		Assigned:  f.Assigned,
		Metadata:  jsonMap(f.Metadata),
		ScopeKind: scope,
		ScopeID:   scopeID,
		Cursor:    cursorPtr(p.Cursor),
		Asc:       p.Order == paging.OrderAsc,
		Limit:     int64(size),
		Offset:    int64(offset),
	}
}

func nullUUIDPtr(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}

	return &id.UUID
}

func cursorPtr(c uuid.NullUUID) *uuid.UUID {
	if !c.Valid {
		return nil
	}

	return &c.UUID
}

func userDomain(
	id, accountID uuid.UUID,
	firstName, lastName, email, passwordDigest, role *string,
	permissions json.RawMessage,
	groupID *uuid.UUID,
	metadata json.RawMessage,
	passwordResetToken *string,
	passwordResetSentAt *time.Time,
	bannedAt *time.Time,
	createdAt, updatedAt time.Time,
) domain.User {
	domainRole, _ := domain.ParseRole(deref(role))

	group := uuid.NullUUID{}
	if groupID != nil {
		group = uuid.NullUUID{UUID: *groupID, Valid: true}
	}

	var perms []string

	_ = json.Unmarshal(permissions, &perms)

	meta := map[string]any{}
	_ = json.Unmarshal(metadata, &meta)

	return domain.User{
		ID:                  id,
		AccountID:           accountID,
		FirstName:           deref(firstName),
		LastName:            deref(lastName),
		Email:               deref(email),
		PasswordDigest:      deref(passwordDigest),
		Role:                domainRole,
		Permissions:         toPermissionSet(perms),
		GroupID:             group,
		Metadata:            meta,
		PasswordResetToken:  deref(passwordResetToken),
		PasswordResetSentAt: passwordResetSentAt,
		BannedAt:            bannedAt,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}
}

func mapUserWrite(id uuid.UUID, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: write user %s: %w", id, users.ErrEmailTaken)
	}

	return fmt.Errorf("postgres: write user %s: %w", id, err)
}

func toPermissionSet(list []string) map[string]struct{} {
	if len(list) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(list))
	for _, p := range list {
		set[p] = struct{}{}
	}

	return set
}

// boolRef makes the explicitly supplied CASE guard non-null.
func boolRef(value bool) *bool { return &value }
