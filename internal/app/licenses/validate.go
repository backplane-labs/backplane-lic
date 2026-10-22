// License validation: the exact algorithm of the reference's
// LicenseValidationService, the quick validate (scope skipped), the
// scoped POST validate and validate-key.
package licenses

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Verdict is a validation outcome: valid, the detail phrase and the
// code.
type Verdict struct {
	Valid  bool
	Detail string
	Code   string
	// Checksum/Version are the scoped validation touches.
	Checksum *string
	Version  *string
}

// Scope is the meta.scope of a validation request (nil values mean the
// key was absent).
type Scope struct {
	Present bool
	Product *string
	Policy  *string
	User    *string
	Machine *string
	// Fingerprint/Fingerprints/Components/Entitlements are the matched
	// lists (compacted and uniq'd).
	Fingerprints []string
	Components   []string
	Entitlements []string
	Checksum     *string
	Version      *string
}

// ValidateQuick runs the GET validate (scope checks skipped, no touch
// for the portal origin).
func (s Scenario) ValidateQuick(
	ctx context.Context, account domain.Account, idOrKey string, skipTouch bool,
) (domain.License, Verdict, error) {
	license, err := s.licenseForValidate(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, Verdict{}, err
	}

	now := s.now()

	verdict := s.runValidation(ctx, account, &license, Scope{Present: false}, now)

	if !skipTouch {
		license = s.touch(ctx, account, license, verdict, now)
	}

	return license, verdict, nil
}

// Validate runs the POST validate by id (with scope and nonce).
func (s Scenario) Validate(
	ctx context.Context, account domain.Account, idOrKey string, scope Scope,
) (domain.License, Verdict, error) {
	license, err := s.licenseForValidate(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, Verdict{}, err
	}

	now := s.now()

	verdict := s.runValidation(ctx, account, &license, scope, now)
	s.touch(ctx, account, license, verdict, now)
	_ = s.touchOnFirstValidation(ctx, account, license, now)

	s.publish(ctx, validationEvent(verdict), license)

	return license, verdict, nil
}

// ValidateKey runs the POST validate-key (anonymous allowed); the
// answer's license is nil when the key matches none.
func (s Scenario) ValidateKey(
	ctx context.Context, account domain.Account, key string, legacyEncrypted bool, scope Scope,
) (*domain.License, Verdict, error) {
	license := s.lookupKey(ctx, account, key, legacyEncrypted)

	// Anonymous bearers and unknown keys pass straight through.
	principal := access.PrincipalOf(ctx)
	if principal.BearerID == uuid.Nil || license == nil {
		return license, s.runValidation(ctx, account, license, scope, s.now()), nil
	}

	if err := access.Require(ctx, "license.validate"); err != nil {
		return nil, Verdict{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, principal.BearerID, *license)
	if err != nil {
		return nil, Verdict{}, err
	}

	if err := access.LicenseAction(principal, *license, "validate", attached); err != nil {
		return nil, Verdict{}, err //nolint:wrapcheck // the authorization's own error
	}

	now := s.now()

	verdict := s.runValidation(ctx, account, license, scope, now)

	*license = s.touch(ctx, account, *license, verdict, now)
	_ = s.touchOnFirstValidation(ctx, account, *license, now)
	s.publish(ctx, validationEvent(verdict), *license)

	return license, verdict, nil
}

// lookupKey resolves the license key (the legacy encrypted form
// compares the bcrypt digest).
func (s Scenario) lookupKey(
	ctx context.Context, account domain.Account, key string, legacyEncrypted bool,
) *domain.License {
	if legacyEncrypted {
		licenseID, _, ok := splitLegacyKey(key)
		if !ok {
			return nil
		}

		license, err := s.store.LicenseByID(ctx, account.ID, licenseID)
		if err != nil || !domain.CompareLegacyKey(license.Key, key) {
			return nil
		}

		return &license
	}

	license, err := s.store.LicenseByKey(ctx, account.ID, key)
	if err != nil {
		return nil
	}

	return &license
}

// splitLegacyKey splits "<32-hex id>-<bits>" (the encrypted key form:
// the id without dashes).
func splitLegacyKey(key string) (uuid.UUID, string, bool) {
	if len(key) < 34 || key[32] != '-' {
		return uuid.Nil, "", false
	}

	id, err := uuid.Parse(key[:8] + "-" + key[8:12] + "-" + key[12:16] + "-" + key[16:20] + "-" + key[20:32])
	if err != nil {
		return uuid.Nil, "", false
	}

	return id, key[32:], true
}

// licenseForValidate loads the license and runs the validate
// authorization.
func (s Scenario) licenseForValidate(
	ctx context.Context, account domain.Account, idOrKey string,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := access.Require(ctx, "license.validate"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.License{}, err
	}

	if err := access.LicenseAction(access.PrincipalOf(ctx), license, "validate", attached); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	return license, nil
}

// validate is the algorithm, verbatim in order.
func (s Scenario) runValidation( //nolint:gocognit,gocyclo,cyclop,funlen,maintidx // the algorithm, verbatim
	ctx context.Context, account domain.Account, licensePtr *domain.License, scope Scope, now time.Time,
) Verdict {
	// nil license → NOT_FOUND (validate-key only; the :id routes 404).
	if licensePtr == nil {
		return Verdict{false, "does not exist", "NOT_FOUND", nil, nil}
	}

	license := *licensePtr
	policy := license.Policy

	// The user-scope owner resolution (per-user lease counts).
	scopeOwner := (*uuid.UUID)(nil)

	if scope.User != nil && *scope.User != "" {
		if id, err := uuid.Parse(*scope.User); err == nil {
			scopeOwner = &id
		} else if user, err := s.users.UserByEmail(ctx, account.ID, *scope.User); err == nil {
			scopeOwner = &user.ID
		}
	}

	if license.Banned() {
		return Verdict{false, "is banned", "BANNED", nil, nil}
	}

	if license.Suspended {
		return Verdict{false, "is suspended", "SUSPENDED", nil, nil}
	}

	if policy.ExpirationStrategy == "RESTRICT_ACCESS" && license.Expired(now) {
		return Verdict{false, "is expired", "EXPIRED", nil, nil}
	}

	if license.CheckInOverdue(now) {
		return Verdict{false, "is overdue for check in", "OVERDUE", nil, nil}
	}

	if scope.Present { //nolint:nestif // the algorithm, verbatim
		verdict, done := s.validateScopes(ctx, account, license, policy, scope, scopeOwner, now)
		if done {
			return verdict
		}
	} else {
		if policy.RequireProductScope {
			return Verdict{false, "product scope is required", "PRODUCT_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequirePolicyScope {
			return Verdict{false, "policy scope is required", "POLICY_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequireUserScope {
			return Verdict{false, "user scope is required", "USER_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequireMachineScope {
			return Verdict{false, "machine scope is required", "MACHINE_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequireFingerprintScope {
			return Verdict{false, "fingerprint scope is required", "FINGERPRINT_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequireComponentsScope {
			return Verdict{false, "components scope is required", "COMPONENTS_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequireChecksumScope {
			return Verdict{false, "checksum scope is required", "CHECKSUM_SCOPE_REQUIRED", nil, nil}
		}

		if policy.RequireVersionScope {
			return Verdict{false, "version scope is required", "VERSION_SCOPE_REQUIRED", nil, nil}
		}
	}

	// The user limit.
	if limit := license.MaxUsers(); limit != nil && license.UsersCount() > *limit {
		if !overageCovers(policy, float64(license.UsersCount()), float64(*limit), !policy.Floating) {
			return Verdict{false, "has too many associated users", "TOO_MANY_USERS", nil, nil}
		}

		return Verdict{true, "has too many associated users", "TOO_MANY_USERS", nil, nil}
	}

	// Non-strict policies exit early.
	if !policy.Strict {
		if license.Expired(now) {
			allow := policy.ExpirationStrategy == "ALLOW_ACCESS" ||
				policy.ExpirationStrategy == "MAINTAIN_ACCESS"

			return Verdict{allow, "is expired", "EXPIRED", nil, nil}
		}

		return Verdict{true, "is valid", "VALID", nil, nil}
	}

	// Node-locked: exactly one machine.
	if !policy.Floating && license.MachinesCount == 0 {
		return Verdict{false, "must have exactly 1 associated machine", "NO_MACHINE", nil, nil}
	}

	if !policy.Floating && license.MachinesCount > 1 {
		count := license.MachinesCount
		if policy.MachineLeasingStrategy == "PER_USER" {
			count = int32(s.ownerMachines(ctx, account, license, scopeOwner).Count) //nolint:gosec // a machine total
		}

		limit := license.MaxMachines()
		bound := int32(1)

		if limit != nil {
			bound = *limit
		}

		if count > bound {
			allow := policy.OverageStrategy == "ALWAYS_ALLOW_OVERAGE" ||
				(policy.OverageStrategy == "ALLOW_2X_OVERAGE" && float64(count) <= float64(bound)*2)

			return Verdict{allow, "has too many associated machines", "TOO_MANY_MACHINES", nil, nil}
		}
	}

	// Floating: at least one machine.
	if policy.Floating && license.MachinesCount == 0 {
		return Verdict{false, "must have at least 1 associated machine", "NO_MACHINES", nil, nil}
	}

	// Floating: the machine limit.
	if limit := license.MaxMachines(); policy.Floating && limit != nil && license.MachinesCount > 1 &&
		license.MachinesCount > *limit {
		count := license.MachinesCount
		if policy.MachineLeasingStrategy == "PER_USER" {
			count = int32(s.ownerMachines(ctx, account, license, scopeOwner).Count) //nolint:gosec // a machine total
		}

		if !overageCovers(policy, float64(count), float64(*limit), false) {
			return Verdict{false, "has too many associated machines", "TOO_MANY_MACHINES", nil, nil}
		}

		return Verdict{true, "has too many associated machines", "TOO_MANY_MACHINES", nil, nil}
	}

	if v, done := s.validateResources(ctx, account, license, policy, scopeOwner); done {
		return v
	}

	if v, done := s.validateProcesses(ctx, account, license, policy, scope, scopeOwner); done {
		return v
	}

	if license.Expired(now) {
		allow := policy.ExpirationStrategy == "ALLOW_ACCESS" ||
			policy.ExpirationStrategy == "MAINTAIN_ACCESS"

		return Verdict{allow, "is expired", "EXPIRED", nil, nil}
	}

	return Verdict{true, "is valid", "VALID", nil, nil}
}

func validationEvent(verdict Verdict) string {
	if verdict.Valid {
		return "license.validation.succeeded"
	}

	return "license.validation.failed"
}
