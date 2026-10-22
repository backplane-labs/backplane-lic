// Package passwords is the password-reset request scenario (POST
// /passwords): always a 204, indistinguishable for unknown emails.
package passwords

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound

// Store is the scenario's consumer-side storage port.
type Store interface {
	// UserByEmail loads a user by downcased email; ErrNotFound.
	UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error)
	// SetUserPasswordReset stores the reset token digest and timestamp.
	SetUserPasswordReset(ctx context.Context, accountID, id uuid.UUID, token string, sentAt time.Time) error
}

// ResetMeta carries the webhook payload's extra facts (the plaintext
// reset token rides the event, never an API response).
type ResetMeta struct {
	PasswordResetToken string
}

// Scenario runs the password scenarios.
type Scenario struct {
	store  Store
	events events.Publisher
	rng    io.Reader
	clock  func() time.Time
}

// New builds the scenario.
func New(store Store, publisher events.Publisher, rng io.Reader, clock func() time.Time) Scenario {
	return Scenario{store: store, events: publisher, rng: rng, clock: clock}
}

// Request generates and stores the reset token for the email, if the
// user exists. Unknown emails are a silent no-op (anti-enumeration).
func (s Scenario) Request(ctx context.Context, account domain.Account, email string, deliver *bool) error {
	user, err := s.store.UserByEmail(ctx, account.ID, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("passwords: request: %w", err)
	}

	raw, err := crypto.GeneratePasswordResetToken(s.rng)
	if err != nil {
		return fmt.Errorf("passwords: request: %w", err)
	}

	now := s.clock()
	digest := crypto.DigestV3(account.Keys.SecretKey, raw)

	if err := s.store.SetUserPasswordReset(ctx, account.ID, user.ID, digest, now); err != nil {
		return fmt.Errorf("passwords: request: %w", err)
	}

	if err := s.events.Publish(ctx, "user.password-reset", user); err != nil {
		return fmt.Errorf("passwords: request: %w", err)
	}

	// The email itself is out of scope (no mailer in the clone);
	// deliver only gates it.
	_ = deliver

	return nil
}
