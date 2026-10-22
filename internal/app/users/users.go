// Package users is the user scenario: the bootstrap admin seed, the
// status computation and the me/profile reads. The full CRUD arrives
// with the users routes.
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// ErrEmailTaken is the unique-index violation of (account, email).
var ErrEmailTaken = errors.New("users: email taken")

// Store is the scenario's consumer-side storage port.
type Store interface {
	// UserByID loads a user; ErrNotFound when none.
	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	// UserByEmail loads a user by downcased email; ErrNotFound.
	UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error)
	// InsertUser writes a new user row.
	InsertUser(ctx context.Context, user domain.User) error
	// UserHasActiveLicense answers the user's status input: a license
	// created/validated/checked-in/out within the last 90 days.
	UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error)
}

// Scenario reads and seeds users.
type Scenario struct {
	store Store
	clock func() time.Time
}

// New builds the scenario.
func New(store Store, clock func() time.Time) Scenario {
	return Scenario{store: store, clock: clock}
}

// SeedAdmin creates the configured admin user when her email is not
// taken (bcrypt, like the reference's has_secure_password).
func (s Scenario) SeedAdmin(ctx context.Context, accountID uuid.UUID, email, password string) error {
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("users: seed admin: %w", err)
	}

	now := s.clock()

	user := domain.User{
		ID:             uuid.New(),
		AccountID:      accountID,
		Email:          email,
		PasswordDigest: string(digest),
		Role:           domain.RoleAdmin,
		Metadata:       map[string]any{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	err = s.store.InsertUser(ctx, user)
	if errors.Is(err, ErrEmailTaken) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("users: seed admin: %w", err)
	}

	return nil
}

// Me loads the bearer's user with her computed status.
func (s Scenario) Me(ctx context.Context, accountID, id uuid.UUID) (domain.User, string, error) {
	user, err := s.store.UserByID(ctx, accountID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.User{}, "", err //nolint:wrapcheck // the sentinel speaks
	}

	if err != nil {
		return domain.User{}, "", fmt.Errorf("users: me: %w", err)
	}

	active, err := s.store.UserHasActiveLicense(ctx, accountID, id)
	if err != nil {
		return domain.User{}, "", fmt.Errorf("users: me: active license: %w", err)
	}

	return user, user.Status(active, s.clock()), nil
}
