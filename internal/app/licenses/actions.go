// License actions: revoke, renew, suspend, reinstate, check-in and the
// usage counters.
package licenses

import (
	"context"
	"errors"
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
		if err := s.publish(ctx, "license.revoked", license); err != nil {
			return err
		}

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

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseExpiry(ctx, account.ID, license.ID, &expiry, now); err != nil {
			return fmt.Errorf("licenses: renew: %w", err)
		}

		license.Expiry = &expiry
		license.UpdatedAt = now

		return s.publish(ctx, "license.renewed", license)
	}); err != nil {
		return domain.License{}, err
	}

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

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseSuspended(ctx, account.ID, license.ID, true, now); err != nil {
			return fmt.Errorf("licenses: suspend: %w", err)
		}

		license.Suspended = true
		license.UpdatedAt = now

		return s.publish(ctx, "license.suspended", license)
	}); err != nil {
		return domain.License{}, err
	}

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

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseSuspended(ctx, account.ID, license.ID, false, now); err != nil {
			return fmt.Errorf("licenses: reinstate: %w", err)
		}

		license.Suspended = false
		license.UpdatedAt = now

		return s.publish(ctx, "license.reinstated", license)
	}); err != nil {
		return domain.License{}, err
	}

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

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseLastCheckIn(ctx, account.ID, license.ID, now, now); err != nil {
			return fmt.Errorf("licenses: check-in: %w", err)
		}

		license.LastCheckInAt = &now
		license.UpdatedAt = now

		return s.publish(ctx, "license.checked-in", license)
	}); err != nil {
		return domain.License{}, err
	}

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

	return s.writeUsage(ctx, account, license, increment, "license.usage.incremented")
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

	return s.writeUsage(ctx, account, license, decrement, "license.usage.decremented")
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
	ctx context.Context, account domain.Account, license domain.License, amount int64, event string,
) (domain.License, error) {
	now := s.now()

	if err := s.write(ctx, func(ctx context.Context) error {
		uses := int32(0)

		if event != "license.usage.reset" {
			current, err := s.store.LicenseUsesForUpdate(ctx, account.ID, license.ID)
			if errors.Is(err, ErrUsageConflict) {
				conflict := apperr.Conflict("failed to increment due to another conflicting update")
				conflict.Source.Pointer = "/data/attributes/uses"

				return conflict
			}

			if err != nil {
				return fmt.Errorf("licenses: usage lock: %w", err)
			}

			uses, err = usageValue(current, amount, event == "license.usage.incremented", license.MaxUses())
			if err != nil {
				return err
			}
		}

		if err := s.store.SetLicenseUses(ctx, account.ID, license.ID, uses, now); err != nil {
			return fmt.Errorf("licenses: usage: %w", err)
		}

		license.Uses = uses
		license.UpdatedAt = now

		if err := s.publish(ctx, event, license); err != nil {
			return err
		}

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

// usageValue checks model validations before the database's integer range.
// Comparisons precede arithmetic, so even MinInt64/MaxInt64 inputs cannot wrap.
func usageValue( //nolint:cyclop // ordered model/range bounds without overflowing arithmetic
	current int32, amount int64, increment bool, limit *int32,
) (int32, error) {
	base := int64(current)

	negative := increment && amount < -base || !increment && amount > base
	if negative {
		return 0, apperr.Validation("USES_INVALID", "must be greater than or equal to 0",
			"/data/attributes/uses", "licenses/#licenses-object-attrs-uses")
	}

	if limit != nil && (increment && amount > int64(*limit)-base || !increment && amount < base-int64(*limit)) {
		return 0, apperr.Validation("USES_LIMIT_EXCEEDED",
			fmt.Sprintf("usage exceeds maximum allowed for license (%d)", *limit),
			"/data/attributes/uses", "licenses/#licenses-object-attrs-uses")
	}

	parameter := "decrement"
	if increment {
		parameter = "increment"
	}

	if increment && amount > math.MaxInt32-base || !increment && amount < base-math.MaxInt32 {
		return 0, apperr.BadRequestSource("integer is too large", "/meta/"+parameter)
	}

	if increment {
		return int32(base + amount), nil //nolint:gosec // range checked above
	}

	return int32(base - amount), nil //nolint:gosec // range checked above
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
