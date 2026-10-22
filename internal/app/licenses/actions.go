// License actions: revoke, renew, suspend, reinstate, check-in and the
// usage counters.
package licenses

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Revoke fires license.revoked and destroys the license.
func (s Scenario) Revoke(ctx context.Context, account domain.Account, idOrKey string) error {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "license.revoke"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseDestroy(access.PrincipalOf(ctx), license); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return s.write(ctx, func(ctx context.Context) error {
		s.publish(ctx, "license.revoked", license)

		if err := s.store.DeleteLicense(ctx, account.ID, license.ID); err != nil {
			return fmt.Errorf("licenses: revoke: %w", err)
		}

		return nil
	})
}

// Renew renews the expiry per the policy's renewal basis.
func (s Scenario) Renew(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.action(ctx, account, license, "renew"); err != nil {
		return domain.License{}, err
	}

	if license.Policy.Duration == nil {
		return domain.License{}, apperr.UnprocessableNoCode(
			"cannot be renewed because the policy does not have a duration", "")
	}

	duration := time.Duration(*license.Policy.Duration) * time.Second

	now := s.now()

	var expiry time.Time

	switch license.Policy.RenewalBasis {
	case "FROM_NOW":
		expiry = now.Add(duration)
	case "FROM_NOW_IF_EXPIRED":
		base := now
		if !license.Expired(now) && license.Expiry != nil {
			base = *license.Expiry
		}

		expiry = base.Add(duration)
	default: // FROM_EXPIRY (the default).
		if license.Expiry == nil {
			return domain.License{}, apperr.UnprocessableNoCode("The request could not be completed", "")
		}

		expiry = license.Expiry.Add(duration)
	}

	if err := s.store.SetLicenseExpiry(ctx, account.ID, license.ID, &expiry, now); err != nil {
		return domain.License{}, fmt.Errorf("licenses: renew: %w", err)
	}

	license.Expiry = &expiry
	license.UpdatedAt = now

	s.publish(ctx, "license.renewed", license)

	return license, nil
}

// Suspend sets the suspended flag.
func (s Scenario) Suspend(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.action(ctx, account, license, "suspend"); err != nil {
		return domain.License{}, err
	}

	if license.Suspended {
		return domain.License{}, apperr.UnprocessableNoCode("is already suspended", "/data/attributes/suspended")
	}

	now := s.now()

	if err := s.store.SetLicenseSuspended(ctx, account.ID, license.ID, true, now); err != nil {
		return domain.License{}, fmt.Errorf("licenses: suspend: %w", err)
	}

	license.Suspended = true
	license.UpdatedAt = now

	s.publish(ctx, "license.suspended", license)

	return license, nil
}

// Reinstate clears the suspended flag.
func (s Scenario) Reinstate(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.action(ctx, account, license, "reinstate"); err != nil {
		return domain.License{}, err
	}

	if !license.Suspended {
		return domain.License{}, apperr.UnprocessableNoCode("is not suspended", "/data/attributes/suspended")
	}

	now := s.now()

	if err := s.store.SetLicenseSuspended(ctx, account.ID, license.ID, false, now); err != nil {
		return domain.License{}, fmt.Errorf("licenses: reinstate: %w", err)
	}

	license.Suspended = false
	license.UpdatedAt = now

	s.publish(ctx, "license.reinstated", license)

	return license, nil
}

// CheckIn stamps the check-in timestamp.
func (s Scenario) CheckIn(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.action(ctx, account, license, "check-in"); err != nil {
		return domain.License{}, err
	}

	if !license.Policy.RequireCheckIn {
		return domain.License{}, apperr.UnprocessableNoCode(
			"cannot be checked in because the policy does not require it", "")
	}

	now := s.now()

	if err := s.store.SetLicenseLastCheckIn(ctx, account.ID, license.ID, now, now); err != nil {
		return domain.License{}, fmt.Errorf("licenses: check-in: %w", err)
	}

	license.LastCheckInAt = &now
	license.UpdatedAt = now

	s.publish(ctx, "license.checked-in", license)

	return license, nil
}

// IncrementUsage adds to the usage counter (default 1).
func (s Scenario) IncrementUsage(
	ctx context.Context, account domain.Account, idOrKey string, increment int64,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.usageWrite(ctx, license, "increment"); err != nil {
		return domain.License{}, err
	}

	if increment > math.MaxInt32 {
		return domain.License{}, apperr.BadRequestSource("integer is too large", "/meta/increment")
	}

	uses := license.Uses + int32(increment) //nolint:gosec // bounded by the MaxInt32 check above

	if limit := license.MaxUses(); limit != nil && uses > *limit {
		return domain.License{}, apperr.Validation("USES_LIMIT_EXCEEDED",
			fmt.Sprintf("usage exceeds maximum allowed for license (%d)", *limit),
			"/data/attributes/uses", "licenses/#licenses-object-attrs-uses")
	}

	return s.writeUsage(ctx, account, license, uses, "license.usage.incremented")
}

// DecrementUsage subtracts from the usage counter (default 1).
func (s Scenario) DecrementUsage(
	ctx context.Context, account domain.Account, idOrKey string, decrement int64,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.usageWrite(ctx, license, "decrement"); err != nil {
		return domain.License{}, err
	}

	if decrement > math.MaxInt32 {
		return domain.License{}, apperr.BadRequestSource("integer is too large", "/meta/decrement")
	}

	uses := license.Uses - int32(decrement) //nolint:gosec // bounded by the MaxInt32 check above

	if uses < 0 {
		return domain.License{}, apperr.Validation("USES_INVALID",
			"must be greater than or equal to 0",
			"/data/attributes/uses", "licenses/#licenses-object-attrs-uses")
	}

	return s.writeUsage(ctx, account, license, uses, "license.usage.decremented")
}

// ResetUsage zeroes the usage counter.
func (s Scenario) ResetUsage(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := s.usageWrite(ctx, license, "reset"); err != nil {
		return domain.License{}, err
	}

	return s.writeUsage(ctx, account, license, 0, "license.usage.reset")
}

func (s Scenario) writeUsage(
	ctx context.Context, account domain.Account, license domain.License, uses int32, event string,
) (domain.License, error) {
	now := s.now()

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseUses(ctx, account.ID, license.ID, uses, now); err != nil {
			return fmt.Errorf("licenses: usage: %w", err)
		}

		license.Uses = uses
		license.UpdatedAt = now

		s.publish(ctx, event, license)

		// The first use starts the expiry clock of FROM_FIRST_USE
		// policies.
		if event == "license.usage.incremented" && license.Expiry == nil &&
			license.Policy.ExpirationBasis == "FROM_FIRST_USE" && license.Policy.Duration != nil {
			expiry := now.Add(time.Duration(*license.Policy.Duration) * time.Second)

			if err := s.store.SetLicenseExpiry(ctx, account.ID, license.ID, &expiry, now); err != nil {
				return fmt.Errorf("licenses: usage expiry: %w", err)
			}

			license.Expiry = &expiry
		}

		return nil
	}); err != nil {
		return domain.License{}, err
	}

	return license, nil
}

// action runs the acting routes' shared permission and record checks.
func (s Scenario) action(ctx context.Context, account domain.Account, license domain.License, clause string) error {
	permissions := map[string]string{
		"renew":     "license.renew",
		"suspend":   "license.suspend",
		"reinstate": "license.reinstate",
		"check-in":  "license.check-in",
	}

	if err := access.Require(ctx, permissions[clause]); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return err
	}

	if err := access.LicenseAction(access.PrincipalOf(ctx), license, clause, attached); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return nil
}

func (s Scenario) usageWrite(ctx context.Context, license domain.License, kind string) error {
	if err := access.Require(ctx, "license.usage."+kind); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseUsageWrite(access.PrincipalOf(ctx), license, kind); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return nil
}
