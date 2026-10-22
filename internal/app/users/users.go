// Package users is the user scenario: the bootstrap admin seed, the
// status computation, CRUD, the password/ban actions and the
// relationship reads (me/profile included).
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Scenario runs the user scenarios.
type Scenario struct {
	store  Store
	events events.Publisher
	clock  func() time.Time
}

// New builds the scenario (events publishes the user webhook events).
func New(store Store, publisher events.Publisher, clock func() time.Time) Scenario {
	return Scenario{store: store, events: publisher, clock: clock}
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

	status, err := s.status(ctx, user)
	if err != nil {
		return domain.User{}, "", err
	}

	return user, status, nil
}

// status computes the user's wire status (the active-license input is
// loaded per user).
func (s Scenario) status(ctx context.Context, user domain.User) (string, error) {
	if user.Banned() {
		return "BANNED", nil
	}

	active, err := s.store.UserHasActiveLicense(ctx, user.AccountID, user.ID)
	if err != nil {
		return "", fmt.Errorf("users: status: %w", err)
	}

	return user.Status(active, s.clock()), nil
}
