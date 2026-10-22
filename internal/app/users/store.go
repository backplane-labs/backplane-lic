package users

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// ErrEmailTaken is the unique-index violation of (account, email).
var ErrEmailTaken = errors.New("users: email taken")

// Filter is the users index filter set (every field optional).
type Filter struct {
	// Roles defaults to ["user"] on the wire (the handler applies the
	// default before calling).
	Roles []string
	// GroupID/ProductID/LicenseID carry the FK filters.
	GroupID   uuid.NullUUID
	ProductID uuid.NullUUID
	LicenseID uuid.NullUUID
	// Status filters by the computed status (ACTIVE/INACTIVE/BANNED).
	Status string
	// Assigned filters on owning at least one license.
	Assigned *bool
	// Metadata is a jsonb containment filter (keys underscored).
	Metadata map[string]any
}

// Listed is a page row: the user plus her computed status.
type Listed struct {
	User   domain.User
	Status string
}

// Store is the scenario's consumer-side storage port.
//
//nolint:interfacebloat // one port per scenario is the architecture's shape
type Store interface {
	// Do runs fn in one transaction; the writes and the webhook events
	// of one action commit together.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
	// UserByID loads a user; ErrNotFound when none.
	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	// UserByEmail loads a user by downcased email; ErrNotFound.
	UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error)
	// InsertUser writes a new user row.
	InsertUser(ctx context.Context, user domain.User) error
	// UpdateUser writes every mutable column of the row.
	UpdateUser(ctx context.Context, user domain.User) error
	// DeleteUser removes the row.
	DeleteUser(ctx context.Context, accountID, id uuid.UUID) error
	// UserHasActiveLicense answers the user's status input.
	UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	// ListUsers answers the filtered page (with computed statuses).
	ListUsers(ctx context.Context, accountID uuid.UUID, f Filter, scopeKind string,
		scopeID uuid.UUID, p paging.Params,
	) ([]Listed, error)
	// CountUsers answers the filter's total.
	CountUsers(ctx context.Context, accountID uuid.UUID, f Filter, scopeKind string,
		scopeID uuid.UUID,
	) (int, error)
	// UserIsTeammate answers whether other shares a license with user.
	UserIsTeammate(ctx context.Context, accountID, userID, otherID uuid.UUID) (bool, error)
	// UserOfLicense answers whether the user owns or is attached to the
	// license.
	UserOfLicense(ctx context.Context, accountID, licenseID, userID uuid.UUID) (bool, error)
	// CountAdmins answers the account's admin count.
	CountAdmins(ctx context.Context, accountID uuid.UUID) (int, error)
	// UpdateUserPassword writes a new password digest and clears the
	// reset fields.
	UpdateUserPassword(ctx context.Context, accountID, id uuid.UUID, digest string) error
	// SetUserPasswordReset stores the reset token digest and timestamp.
	SetUserPasswordReset(ctx context.Context, accountID, id uuid.UUID, token string, sentAt time.Time) error
	// SetUserBannedAt writes (or clears) the ban timestamp.
	SetUserBannedAt(ctx context.Context, accountID, id uuid.UUID, bannedAt *time.Time) error
	// DeleteUserTokens revokes every token of the user.
	DeleteUserTokens(ctx context.Context, accountID, userID uuid.UUID) error
	// DeleteUserTokensExcept revokes every token of the user but the
	// acting one.
	DeleteUserTokensExcept(ctx context.Context, accountID, userID, exceptID uuid.UUID) error
	// CountGroupUsers answers the group's member count.
	CountGroupUsers(ctx context.Context, accountID, groupID uuid.UUID) (int, error)
	// GroupByID loads a group; ErrNotFound.
	GroupByID(ctx context.Context, accountID, id uuid.UUID) (domain.Group, error)
	// UserLicenseIDs answers the user's license ids (owned or attached).
	UserLicenseIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// UserMachineIDs answers the user's machine ids.
	UserMachineIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// UserProductIDs answers the product ids behind the user's licenses.
	UserProductIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// GroupIDsOfUser answers the group ids the user belongs to or owns.
	GroupIDsOfUser(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
}

// Page is one result page with its total (offset pagination meta).
type Page struct {
	Rows  []Listed
	Total int
}
