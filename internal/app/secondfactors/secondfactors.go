// Package secondfactors is the TOTP second-factor scenario: create
// (secret revealed), enable/disable (OTP-verified), delete, and the
// index/show reads. Exactly one factor exists per user.
package secondfactors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound

// ErrDuplicate is the one-factor-per-user conflict.
var ErrDuplicate = errors.New("secondfactors: factor exists")

// errEnabledDuplicate commits the consumed OTP before returning the expected conflict.
var errEnabledDuplicate = errors.New("secondfactors: enabled factor exists")

// Store is the scenario's consumer-side storage port.
type Store interface {
	Do(ctx context.Context, fn func(context.Context) error) error
	// UserByID loads the target user.
	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	// UserByEmail loads a user by downcased email.
	UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error)
	// SecondFactorByID loads the user's factor.
	SecondFactorByID(ctx context.Context, accountID, userID, id uuid.UUID) (domain.SecondFactor, error)
	// AnySecondFactor loads the user's (only) factor.
	AnySecondFactor(ctx context.Context, userID uuid.UUID) (domain.SecondFactor, error)
	// InsertSecondFactor writes the factor.
	InsertSecondFactor(ctx context.Context, factor domain.SecondFactor) error
	// UpdateSecondFactor writes enabled and the replay guard.
	UpdateSecondFactor(ctx context.Context, factor domain.SecondFactor) error
	// DeleteSecondFactor removes the factor.
	DeleteSecondFactor(ctx context.Context, accountID, userID, id uuid.UUID) error
	// ListSecondFactors answers the user's factors.
	ListSecondFactors(ctx context.Context, accountID, userID uuid.UUID, limit int) ([]domain.SecondFactor, error)
	// EnabledSecondFactorSecret loads the enabled factor's secret.
	EnabledSecondFactorSecret(ctx context.Context, userID uuid.UUID) (string, bool, error)
}

// Scenario runs the second-factor scenarios.
type Scenario struct {
	store  Store
	events events.Publisher
	rng    Reader
	clock  func() time.Time
}

// Reader is the entropy source (crypto.Reader in production).
type Reader interface {
	Read(p []byte) (int, error)
}

// New builds the scenario.
func New(store Store, publisher events.Publisher, rng Reader, clock func() time.Time) Scenario {
	return Scenario{store: store, events: publisher, rng: rng, clock: clock}
}

// List answers the user's factors.
func (s Scenario) List(
	ctx context.Context, account domain.Account, principal access.Principal, userID uuid.UUID, limit int,
) ([]domain.SecondFactor, error) {
	if err := access.Require(ctx, "user.second-factors.read"); err != nil {
		return nil, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.target(ctx, account, principal, userID)
	if err != nil {
		return nil, err
	}

	if err := access.SecondFactorShow(principal, user.ID); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	return s.store.ListSecondFactors(ctx, account.ID, user.ID, limit) //nolint:wrapcheck // the port's own error
}

// Show loads one factor.
func (s Scenario) Show(
	ctx context.Context, account domain.Account, principal access.Principal, userID, factorID uuid.UUID,
) (domain.SecondFactor, error) {
	if err := access.Require(ctx, "user.second-factors.read"); err != nil {
		return domain.SecondFactor{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.target(ctx, account, principal, userID)
	if err != nil {
		return domain.SecondFactor{}, err
	}

	if err := access.SecondFactorShow(principal, user.ID); err != nil {
		return domain.SecondFactor{}, err //nolint:wrapcheck // the authorization's own error
	}

	factor, err := s.store.SecondFactorByID(ctx, account.ID, user.ID, factorID)
	if errors.Is(err, ErrNotFound) {
		return domain.SecondFactor{}, apperr.NotFound("second factor", factorID.String())
	}

	if err != nil {
		return domain.SecondFactor{}, fmt.Errorf("second factors: show: %w", err)
	}

	return factor, nil
}

// Create issues a new factor: the user's password when she has no
// enabled factor yet, a valid OTP of the enabled one otherwise (that
// path then conflicts: one factor per user). The response reveals the
// secret and provisioning uri.
func (s Scenario) create(
	ctx context.Context, account domain.Account, principal access.Principal, userID uuid.UUID,
	password, otp string,
) (domain.SecondFactor, error) {
	if err := access.Require(ctx, "user.second-factors.create"); err != nil {
		return domain.SecondFactor{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.target(ctx, account, principal, userID)
	if err != nil {
		return domain.SecondFactor{}, err
	}

	if err := access.SecondFactorWrite(principal, user.ID); err != nil {
		return domain.SecondFactor{}, err //nolint:wrapcheck // the authorization's own error
	}

	enabled, err := s.enabledFactor(ctx, user.ID)
	if err != nil {
		return domain.SecondFactor{}, err
	}

	if enabled != nil {
		verified, ok, err := s.verify(*enabled, otp)
		if err != nil {
			return domain.SecondFactor{}, err
		}

		if !ok {
			return domain.SecondFactor{}, otpInvalid()
		}

		enabled.LastVerifiedAt = &verified
		if err := s.store.UpdateSecondFactor(ctx, *enabled); err != nil {
			return domain.SecondFactor{}, fmt.Errorf("second factors: consume OTP: %w", err)
		}

		return domain.SecondFactor{}, errEnabledDuplicate
	}

	if !user.HasPassword() || !user.Authenticates(password) {
		return domain.SecondFactor{}, passwordInvalid()
	}

	secret, err := randomBase32(s.rng)
	if err != nil {
		return domain.SecondFactor{}, err
	}

	now := s.clock()

	factor := domain.SecondFactor{
		ID:        uuid.New(),
		AccountID: account.ID,
		UserID:    user.ID,
		Secret:    secret,
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.InsertSecondFactor(ctx, factor); err != nil {
		if errors.Is(err, ErrDuplicate) {
			return domain.SecondFactor{}, conflict()
		}

		return domain.SecondFactor{}, fmt.Errorf("second factors: create: %w", err)
	}

	if err := s.events.Publish(ctx, "second-factor.created", factor); err != nil {
		return domain.SecondFactor{}, fmt.Errorf("second factors: create: %w", err)
	}

	return factor, nil
}

// Update toggles the factor (a valid OTP of THIS factor required).
func (s Scenario) update(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	userID, factorID uuid.UUID,
	enabled bool,
	otp string,
) (domain.SecondFactor, error) {
	if err := access.Require(ctx, "user.second-factors.update"); err != nil {
		return domain.SecondFactor{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.target(ctx, account, principal, userID)
	if err != nil {
		return domain.SecondFactor{}, err
	}

	if err := access.SecondFactorWrite(principal, user.ID); err != nil {
		return domain.SecondFactor{}, err //nolint:wrapcheck // the authorization's own error
	}

	factor, err := s.store.SecondFactorByID(ctx, account.ID, user.ID, factorID)
	if errors.Is(err, ErrNotFound) {
		return domain.SecondFactor{}, apperr.NotFound("second factor", factorID.String())
	}

	if err != nil {
		return domain.SecondFactor{}, fmt.Errorf("second factors: update: %w", err)
	}

	verified, ok, err := s.verify(factor, otp)
	if err != nil {
		return domain.SecondFactor{}, err
	}

	if !ok {
		return domain.SecondFactor{}, otpInvalid()
	}

	factor.Enabled = enabled
	factor.LastVerifiedAt = &verified

	if err := s.store.UpdateSecondFactor(ctx, factor); err != nil {
		return domain.SecondFactor{}, fmt.Errorf("second factors: update: %w", err)
	}

	event := "second-factor.disabled"
	if enabled {
		event = "second-factor.enabled"
	}

	if err := s.events.Publish(ctx, event, factor); err != nil {
		return domain.SecondFactor{}, fmt.Errorf("second factors: update: %w", err)
	}

	return factor, nil
}

// Delete removes the factor; an enabled one demands its OTP.
func (s Scenario) delete(
	ctx context.Context, account domain.Account, principal access.Principal, userID, factorID uuid.UUID,
	otp string,
) error {
	if err := access.Require(ctx, "user.second-factors.delete"); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	user, err := s.target(ctx, account, principal, userID)
	if err != nil {
		return err
	}

	if err := access.SecondFactorWrite(principal, user.ID); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	factor, err := s.store.SecondFactorByID(ctx, account.ID, user.ID, factorID)
	if errors.Is(err, ErrNotFound) {
		return apperr.NotFound("second factor", factorID.String())
	}

	if err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	enabled, err := s.enabledFactor(ctx, user.ID)
	if err != nil {
		return err
	}

	if enabled != nil {
		if _, ok, err := s.verify(*enabled, otp); err != nil {
			return err
		} else if !ok {
			return otpInvalid()
		}
	}

	if err := s.store.DeleteSecondFactor(ctx, account.ID, user.ID, factorID); err != nil {
		return fmt.Errorf("second factors: delete: %w", err)
	}

	return s.events.Publish(ctx, "second-factor.deleted", factor) //nolint:wrapcheck // the port's own error
}

// target resolves the user (scoped for the sub-route) or 404s.
func (s Scenario) target(
	ctx context.Context, account domain.Account, _ access.Principal, userID uuid.UUID,
) (domain.User, error) {
	user, err := s.store.UserByID(ctx, account.ID, userID)
	if errors.Is(err, ErrNotFound) {
		return domain.User{}, apperr.NotFound("user", userID.String())
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("second factors: user: %w", err)
	}

	return user, nil
}

// enabledFactor answers the user's enabled factor (nil when none).
func (s Scenario) enabledFactor(ctx context.Context, userID uuid.UUID) (*domain.SecondFactor, error) {
	secret, ok, err := s.store.EnabledSecondFactorSecret(ctx, userID)
	if err != nil || !ok {
		return nil, err //nolint:wrapcheck // the store's own error
	}

	factor, err := s.store.AnySecondFactor(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("second factors: enabled: %w", err)
	}

	if factor.Secret != secret {
		return nil, nil //nolint:nilnil // a mismatched row is not an error
	}

	return &factor, nil
}

// verify checks the OTP against the factor with replay protection,
// persisting the matched timestamp.
func (s Scenario) verify(factor domain.SecondFactor, otp string) (time.Time, bool, error) {
	ts, ok, err := crypto.VerifyTOTPAfter(factor.Secret, otp, s.clock(), factor.LastVerifiedAt)
	if err != nil || !ok {
		return time.Time{}, ok, err //nolint:wrapcheck // the crypto's own error
	}

	factor.LastVerifiedAt = &ts

	return ts, true, nil
}

// secretBytes is the TOTP secret's entropy (160 bits, ROTP's default).
const secretBytes = 20

// randomBase32 generates a 160-bit base32 secret (ROTP::Base32.random).
func randomBase32(rng Reader) (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := io.ReadFull(rng, buf); err != nil {
		return "", fmt.Errorf("second factors: secret: %w", err)
	}

	return base32Encode(buf), nil
}

const (
	base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	bitsPerByte    = 8
	bitsPerSymbol  = 5
	base32Mask     = 31
)

func base32Encode(buf []byte) string {
	var out []byte

	bits := 0
	value := 0

	for _, b := range buf {
		value = value<<bitsPerByte | int(b)
		bits += bitsPerByte

		for bits >= bitsPerSymbol {
			out = append(out, base32Alphabet[(value>>(bits-bitsPerSymbol))&base32Mask])
			bits -= bitsPerSymbol
		}
	}

	if bits > 0 {
		out = append(out, base32Alphabet[(value<<(bitsPerSymbol-bits))&base32Mask])
	}

	return string(out)
}

func otpInvalid() *apperr.T {
	return apperr.UnauthorizedSource("OTP_INVALID", "second factor must be valid", "", "/meta/otp")
}

func passwordInvalid() *apperr.T {
	return apperr.UnauthorizedSource("PASSWORD_INVALID", "password must be valid", "", "/meta/password")
}

func conflict() *apperr.T {
	return apperr.Conflict("The request could not be completed because of a conflict")
}

// Create records the factor and its event in one transaction.
func (s Scenario) Create(
	ctx context.Context, account domain.Account, principal access.Principal,
	userID uuid.UUID, password, otp string,
) (domain.SecondFactor, error) {
	var factor domain.SecondFactor

	duplicate := false

	err := s.store.Do(ctx, func(ctx context.Context) error {
		var err error

		factor, err = s.create(ctx, account, principal, userID, password, otp)
		if errors.Is(err, errEnabledDuplicate) {
			duplicate = true
			return nil
		}

		return err
	})

	if err == nil && duplicate {
		return domain.SecondFactor{}, conflict()
	}

	return factor, err //nolint:wrapcheck // scenario error
}

// Update serializes OTP verification and the state change with its event.
func (s Scenario) Update(
	ctx context.Context, account domain.Account, principal access.Principal,
	userID, factorID uuid.UUID, enabled bool, otp string,
) (domain.SecondFactor, error) {
	var factor domain.SecondFactor

	err := s.store.Do(ctx, func(ctx context.Context) error {
		var err error

		factor, err = s.update(ctx, account, principal, userID, factorID, enabled, otp)

		return err
	})

	return factor, err //nolint:wrapcheck // scenario error
}

// Delete removes the factor and records its event atomically.
func (s Scenario) Delete(
	ctx context.Context, account domain.Account, principal access.Principal,
	userID, factorID uuid.UUID, otp string,
) error {
	if err := s.store.Do(ctx, func(ctx context.Context) error {
		return s.delete(ctx, account, principal, userID, factorID, otp)
	}); err != nil {
		return fmt.Errorf("second factors: delete: %w", err)
	}

	return nil
}
