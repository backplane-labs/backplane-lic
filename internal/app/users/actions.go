package users

import (
	"context"
	"fmt"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

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

	if bannedAt != nil {
		user.BannedAt = bannedAt
	} else {
		user.BannedAt = nil
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.SetUserBannedAt(ctx, account.ID, user.ID, bannedAt); err != nil {
			return err //nolint:wrapcheck // the port's own error
		}

		return s.events.Publish(ctx, event, user)
	}); err != nil {
		return Listed{}, fmt.Errorf("users: ban: %w", err)
	}

	status, err := s.status(ctx, user)
	if err != nil {
		return Listed{}, err
	}

	return Listed{User: user, Status: status}, nil
}
