package users

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// The password-reset token's validity window.
const resetTokenTTL = 24 * time.Hour

// UpdatePassword changes the bearer's own password (oldPassword
// verified), revoking every other token of the user.
func (s Scenario) UpdatePassword(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias, oldPassword, newPassword string,
) (Listed, error) {
	if err := access.Require(ctx, "user.password.update"); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return Listed{}, err
	}

	if err := access.UserPasswordUpdate(principal, user); err != nil {
		return Listed{}, err //nolint:wrapcheck // the authorization's own error
	}

	valid := user.HasPassword() &&
		bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(oldPassword)) == nil
	if !valid {
		return Listed{}, apperr.UnauthorizedSource("", "is not valid", "", "/meta/oldPassword")
	}

	if err := s.setPassword(ctx, account.ID, user.ID, newPassword); err != nil {
		return Listed{}, err
	}

	if err := s.store.DeleteUserTokensExcept(ctx, account.ID, user.ID, principal.TokenID); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	return s.show(ctx, account.ID, user.ID)
}

// ResetPassword resets the password with the emailed reset token
// (anti-enumeration: a token mismatch is the user 404), revoking every
// token of the user.
func (s Scenario) ResetPassword(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias, resetToken, newPassword string,
) (Listed, error) {
	// The reset policy skips authentication entirely (anonymous resets
	// are the point); a passwordless user of a protected account cannot
	// reset (nothing to prove the claim with).
	if principal.BearerType != "" {
		if err := access.Require(ctx, "user.password.reset"); err != nil {
			return Listed{}, err //nolint:wrapcheck // the port's own error
		}
	}

	// Unscoped lookup: the reset token itself proves the claim.
	user, err := s.byAlias(ctx, account.ID, alias)
	if errors.Is(err, ErrNotFound) {
		return Listed{}, apperr.NotFound("user", alias)
	}

	if err != nil {
		return Listed{}, err
	}

	if user.Role == domain.RoleUser && account.Protected && !user.HasPassword() {
		return Listed{}, apperr.ForbiddenReason(access.ReasonAllResources)
	}

	if principal.BearerType != "" && user.ID != principal.BearerID {
		return Listed{}, apperr.ForbiddenReason(access.ReasonAllResources)
	}

	if err := s.verifyResetToken(account, user, alias, resetToken); err != nil {
		return Listed{}, err
	}

	if err := s.setPassword(ctx, account.ID, user.ID, newPassword); err != nil {
		return Listed{}, err
	}

	if err := s.store.DeleteUserTokens(ctx, account.ID, user.ID); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	return s.show(ctx, account.ID, user.ID)
}

// Ban sets bannedAt (only plain-user targets).
func (s Scenario) Ban(
	ctx context.Context, account domain.Account, principal access.Principal, alias string,
) (Listed, error) {
	return s.setBanned(ctx, account, principal, alias, true)
}

// Unban clears bannedAt.
func (s Scenario) Unban(
	ctx context.Context, account domain.Account, principal access.Principal, alias string,
) (Listed, error) {
	return s.setBanned(ctx, account, principal, alias, false)
}

func (s Scenario) setBanned(
	ctx context.Context, account domain.Account, principal access.Principal, alias string, banned bool,
) (Listed, error) {
	permission := "user.ban"
	event := "user.banned"

	if !banned {
		permission = "user.unban"
		event = "user.unbanned"
	}

	if err := access.Require(ctx, permission); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return Listed{}, err
	}

	if err := access.UserBan(principal, user); err != nil {
		return Listed{}, err //nolint:wrapcheck // the authorization's own error
	}

	var bannedAt *time.Time

	if banned {
		t := s.clock()
		bannedAt = &t
	}

	if err := s.store.SetUserBannedAt(ctx, account.ID, user.ID, bannedAt); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	if bannedAt != nil {
		user.BannedAt = bannedAt
	} else {
		user.BannedAt = nil
	}

	if err := s.events.Publish(ctx, event, user); err != nil {
		return Listed{}, fmt.Errorf("users: ban: %w", err)
	}

	status, err := s.status(ctx, user)
	if err != nil {
		return Listed{}, err
	}

	return Listed{User: user, Status: status}, nil
}

// verifyResetToken checks the reset claim (anti-enumeration 404) and
// its 24h window.
func (s Scenario) verifyResetToken(
	account domain.Account, user domain.User, alias, resetToken string,
) error {
	stored := []byte(user.PasswordResetToken)
	given := []byte(crypto.DigestV3(account.Keys.SecretKey, resetToken))

	if user.PasswordResetToken == "" ||
		subtle.ConstantTimeCompare(stored, given) != 1 {
		return apperr.NotFound("user", alias)
	}

	if user.PasswordResetSentAt != nil &&
		s.clock().Sub(*user.PasswordResetSentAt) > resetTokenTTL {
		return apperr.UnauthorizedSource("", "is expired", "", "/meta/passwordResetToken")
	}

	return nil
}

// setPassword validates and writes the new password digest, clearing
// the reset fields.
func (s Scenario) setPassword(ctx context.Context, accountID, id uuid.UUID, password string) error {
	if n := utf8.RuneCountInString(password); n < minPasswordLen || n > maxPasswordLen {
		return unprocessable(
			"PASSWORD_TOO_SHORT",
			"is too short (minimum is 6 characters)",
			"attributes/password",
		)
	}

	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("users: password: %w", err)
	}

	return s.store.UpdateUserPassword(ctx, accountID, id, string(digest)) //nolint:wrapcheck // the store's own error
}

// show loads the user fresh (post-write) with her status.
func (s Scenario) show(ctx context.Context, accountID, id uuid.UUID) (Listed, error) {
	user, err := s.store.UserByID(ctx, accountID, id)
	if err != nil {
		return Listed{}, err //nolint:wrapcheck // the caller maps the sentinel
	}

	status, err := s.status(ctx, user)
	if err != nil {
		return Listed{}, err
	}

	return Listed{User: user, Status: status}, nil
}
