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

	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// UsersRepo adapts the generated user queries to the users and authn
// scenarios' storage ports.
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
		row.BannedAt, row.CreatedAt, row.UpdatedAt), nil
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
		row.BannedAt, row.CreatedAt, row.UpdatedAt), nil
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
		Metadata:       jsonMap(user.Metadata),
	})
	if err != nil {
		return mapUserWrite(user.ID, err)
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

func userDomain(
	id, accountID uuid.UUID,
	firstName, lastName, email, passwordDigest, role *string,
	permissions json.RawMessage,
	groupID *uuid.UUID,
	metadata json.RawMessage,
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
		ID:             id,
		AccountID:      accountID,
		FirstName:      deref(firstName),
		LastName:       deref(lastName),
		Email:          deref(email),
		PasswordDigest: deref(passwordDigest),
		Role:           domainRole,
		Permissions:    toPermissionSet(perms),
		GroupID:        group,
		Metadata:       meta,
		BannedAt:       bannedAt,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

func mapUserWrite(id uuid.UUID, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("postgres: insert user %s: %w", id, users.ErrEmailTaken)
	}

	return fmt.Errorf("postgres: insert user %s: %w", id, err)
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
