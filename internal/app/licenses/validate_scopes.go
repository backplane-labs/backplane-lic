// The scoped validation checks: product, policy, user, entitlements,
// machine, fingerprints, components, checksum and version, then the
// resource and process limits (the tail of LicenseValidationService).
package licenses

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

const (
	leasePerUser = "PER_USER"
	matchTwo     = 2
)

// validateScopes runs the scope block; done answers whether the
// verdict is final.
func (s Scenario) validateScopes( //nolint:gocognit,gocyclo,cyclop,funlen,maintidx // the algorithm, verbatim
	ctx context.Context, account domain.Account, license domain.License,
	policy domain.Policy, scope Scope, _ *uuid.UUID, now time.Time,
) (Verdict, bool) {
	fail := func(detail, code string) (Verdict, bool) {
		return Verdict{false, detail, code, nil, nil}, true
	}

	// Product scope.
	if scope.Product != nil {
		if *scope.Product != license.ProductID.String() {
			return fail("product scope does not match", "PRODUCT_SCOPE_MISMATCH")
		}
	} else if policy.RequireProductScope {
		return fail("product scope is required", "PRODUCT_SCOPE_REQUIRED")
	}

	// Policy scope.
	if scope.Policy != nil {
		if *scope.Policy != license.PolicyID.String() {
			return fail("policy scope does not match", "POLICY_SCOPE_MISMATCH")
		}
	} else if policy.RequirePolicyScope {
		return fail("policy scope is required", "POLICY_SCOPE_REQUIRED")
	}

	// User scope.
	if scope.User != nil && *scope.User != "" { //nolint:nestif // the user-scope match
		ident := *scope.User

		matches := license.OwnerID.Valid && license.OwnerID.UUID.String() == ident

		if !matches {
			var (
				id    = uuid.Nil
				email *string
			)

			if parsed, err := uuid.Parse(ident); err == nil {
				id = parsed
			} else {
				email = &ident
			}

			matches, _ = s.store.LicenseUserMatches(ctx, account.ID, license.ID, id, email)
		}

		if !matches {
			return fail("user scope does not match", "USER_SCOPE_MISMATCH")
		}
	} else if policy.RequireUserScope {
		return fail("user scope is required", "USER_SCOPE_REQUIRED")
	}

	// Entitlements scope.
	if scope.Entitlements != nil {
		codes := uniq(scope.Entitlements)
		if len(codes) == 0 {
			return fail("entitlements scope is empty", "ENTITLEMENTS_SCOPE_EMPTY")
		}

		count, _ := s.store.CountLicenseEntitlementCodes(ctx, account.ID, license.PolicyID, license.ID, codes)
		if count != len(codes) {
			return fail("is missing one or more required entitlements", "ENTITLEMENTS_MISSING")
		}
	}

	// Machine scope.
	if scope.Machine != nil && *scope.Machine != "" {
		switch {
		case !policy.Floating && license.MachinesCount == 0:
			return fail("machine is not activated (has no associated machine)", "NO_MACHINE")
		case policy.Floating && license.MachinesCount == 0:
			return fail("machine is not activated (has no associated machines)", "NO_MACHINES")
		default:
			machine, err := s.machineOfScope(ctx, account, license, *scope.Machine, now)
			if err != nil || machine.ID == uuid.Nil {
				return fail("machine is not activated (does not match any associated machines)", "MACHINE_SCOPE_MISMATCH")
			}

			if v, done := s.machineHeartbeatChecks(ctx, policy, machine, scope.User, fail); done {
				return v, true
			}
		}
	} else if policy.RequireMachineScope {
		return fail("machine scope is required", "MACHINE_SCOPE_REQUIRED")
	}

	// Fingerprint scope(s).
	if len(scope.Fingerprints) > 0 { //nolint:nestif // the fingerprint scope, verbatim
		fingerprints := uniq(scope.Fingerprints)
		if len(fingerprints) == 0 {
			return fail("fingerprint scope is empty", "FINGERPRINT_SCOPE_EMPTY")
		}

		switch {
		case !policy.Floating && license.MachinesCount == 0:
			return fail("fingerprint is not activated (has no associated machine)", "NO_MACHINE")
		case policy.Floating && license.MachinesCount == 0:
			return fail("fingerprint is not activated (has no associated machines)", "NO_MACHINES")
		default:
			machines := s.machinesOfFingerprints(ctx, account, license, fingerprints, now)

			dead, alive := 0, 0

			for i := range machines {
				switch machines[i].HeartbeatStatus {
				case domain.MachineDead:
					dead++
				case domain.MachineAlive:
					alive++
				}
			}

			if dead == len(fingerprints) {
				return fail("machine heartbeat is dead", "HEARTBEAT_DEAD")
			}

			if len(fingerprints) > 1 {
				switch policy.MachineMatchingStrategy {
				case "MATCH_MOST":
					if alive < ceilHalf(len(fingerprints)) {
						return fail(
							"one or more fingerprint is not activated (does not match enough associated machines)",
							"FINGERPRINT_SCOPE_MISMATCH")
					}
				case "MATCH_TWO":
					if alive < matchTwo {
						return fail(
							"one or more fingerprint is not activated (does not match at least 2 associated machines)",
							"FINGERPRINT_SCOPE_MISMATCH")
					}
				case "MATCH_ALL":
					if alive < len(fingerprints) {
						return fail(
							"one or more fingerprint is not activated (does not match all associated machines)",
							"FINGERPRINT_SCOPE_MISMATCH")
					}
				default: // MATCH_ANY (the default).
					if alive == 0 {
						return fail(
							"one or more fingerprint is not activated (does not match any associated machines)",
							"FINGERPRINT_SCOPE_MISMATCH")
					}
				}
			} else if alive == 0 {
				return fail("fingerprint is not activated (does not match any associated machines)", "FINGERPRINT_SCOPE_MISMATCH")
			}

			// The user scope must match the alive machines' owners.
			if scope.User != nil && *scope.User != "" && !s.aliveMachinesForUser(machines, *scope.User) {
				return fail("user scope does not match (does not match associated machine owners)", "USER_SCOPE_MISMATCH")
			}

			if policy.RequireHeartbeat {
				for i := range machines {
					if machines[i].Alive() && machines[i].NotStarted() {
						return fail("machine heartbeat is required", "HEARTBEAT_NOT_STARTED")
					}
				}
			}
		}
	} else if policy.RequireFingerprintScope {
		return fail("fingerprint scope is required", "FINGERPRINT_SCOPE_REQUIRED")
	}

	// Components scope.
	if scope.Components != nil { //nolint:nestif // the components ladder, verbatim
		if len(scope.Fingerprints) == 0 {
			return fail("fingerprint scope is required when using the components scope", "FINGERPRINT_SCOPE_REQUIRED")
		}

		components := uniq(scope.Components)
		if len(components) == 0 {
			return fail("components scope is empty", "COMPONENTS_SCOPE_EMPTY")
		}

		machine, err := s.machineOfFingerprint(ctx, account, license, scope.Fingerprints[0], now)
		if err == nil && machine.ID != uuid.Nil {
			count, _ := s.store.CountMachineComponentsOf(ctx, machine.ID, components)

			mismatch := func(detail string) (Verdict, bool) {
				return Verdict{false, detail, "COMPONENTS_SCOPE_MISMATCH", nil, nil}, true
			}

			switch policy.ComponentMatchingStrategy {
			case "MATCH_MOST":
				if count < ceilHalf(len(components)) {
					return mismatch("one or more component is not activated (does not match enough associated components)")
				}
			case "MATCH_TWO":
				if count < matchTwo {
					return mismatch(
						"one or more component is not activated (does not match at least 2 associated components)")
				}
			case "MATCH_ALL":
				if count < len(components) {
					return mismatch("one or more component is not activated (does not match all associated components)")
				}
			default:
				if count == 0 {
					return mismatch("one or more component is not activated (does not match any associated components)")
				}
			}
		}
	} else if policy.RequireComponentsScope {
		return fail("components scope is required", "COMPONENTS_SCOPE_REQUIRED")
	}

	// Checksum scope: releases are out of scope for the clone, so a
	// checksum never matches an accessible artifact.
	if scope.Checksum != nil {
		return fail("checksum scope is not valid (does not match any accessible artifacts)", "CHECKSUM_SCOPE_MISMATCH")
	} else if policy.RequireChecksumScope {
		return fail("checksum scope is required", "CHECKSUM_SCOPE_REQUIRED")
	}

	// Version scope: likewise.
	if scope.Version != nil {
		return fail("version scope is not valid (does not match any accessible releases)", "VERSION_SCOPE_MISMATCH")
	} else if policy.RequireVersionScope {
		return fail("version scope is required", "VERSION_SCOPE_REQUIRED")
	}

	return Verdict{}, false
}

// machineHeartbeatChecks runs the machine-scope heartbeat and
// user-owner checks.
func (s Scenario) machineHeartbeatChecks(
	ctx context.Context, policy domain.Policy, machine domain.Machine, scopeUser *string,
	fail func(detail, code string) (Verdict, bool),
) (Verdict, bool) {
	if scopeUser != nil && *scopeUser != "" && machine.OwnerID.Valid { //nolint:nestif // the owner match
		ident := *scopeUser
		if machine.OwnerID.UUID.String() != ident {
			match := false
			if owner, err := s.users.UserByID(ctx, machine.AccountID, machine.OwnerID.UUID); err == nil && owner.Email != "" {
				match = owner.Email == ident
			}

			if !match {
				return fail("user scope does not match (does not match associated machine owner)", "USER_SCOPE_MISMATCH")
			}
		}
	}

	if policy.RequireHeartbeat && machine.NotStarted() {
		return fail("machine heartbeat is required", "HEARTBEAT_NOT_STARTED")
	}

	if machine.HeartbeatStatus == domain.MachineDead {
		return fail("machine heartbeat is dead", "HEARTBEAT_DEAD")
	}

	return Verdict{}, false
}

// validateResources runs the cores/memory/disk limit checks.
func (s Scenario) validateResources(
	ctx context.Context, account domain.Account, license domain.License,
	policy domain.Policy, scopeOwner *uuid.UUID,
) (Verdict, bool) {
	counts := MachineSums{
		Cores:  int(license.MachinesCoreCount),
		Memory: license.MachinesMemoryCount,
		Disk:   license.MachinesDiskCount,
	}

	if policy.MachineLeasingStrategy == leasePerUser {
		counts = s.ownerMachines(ctx, account, license, scopeOwner)
		_ = counts.Count
	}

	check := func(count, limit float64, detail, code string) (Verdict, bool) {
		if count <= limit {
			return Verdict{}, false
		}

		if overageCovers(policy, count, limit, !policy.Floating) {
			return Verdict{true, detail, code, nil, nil}, true
		}

		return Verdict{false, detail, code, nil, nil}, true
	}

	if limit := license.MaxCores(); limit != nil {
		if v, done := check(float64(counts.Cores), float64(*limit),
			"has too many associated machine cores", "TOO_MANY_CORES"); done {
			return v, true
		}
	}

	if limit := license.MaxMemory(); limit != nil {
		if v, done := check(float64(counts.Memory), float64(*limit),
			"has too much associated machine memory", "TOO_MUCH_MEMORY"); done {
			return v, true
		}
	}

	if limit := license.MaxDisk(); limit != nil {
		if v, done := check(float64(counts.Disk), float64(*limit),
			"has too much associated machine disk", "TOO_MUCH_DISK"); done {
			return v, true
		}
	}

	return Verdict{}, false
}

// validateProcesses runs the process limit checks per the leasing
// strategy.
func (s Scenario) validateProcesses( //nolint:cyclop // the leasing-strategy ladder
	ctx context.Context, account domain.Account, license domain.License,
	policy domain.Policy, scope Scope, scopeOwner *uuid.UUID,
) (Verdict, bool) {
	var count, limit int

	maxProcesses := license.MaxProcesses()

	switch {
	case policy.ProcessLeasingStrategy == "PER_MACHINE" && len(scope.Fingerprints) > 0:
		machines := s.machinesOfFingerprints(ctx, account, license, scope.Fingerprints, s.now())
		if machine := firstAlive(machines); machine != nil {
			count, _ = s.store.CountMachineProcesses(ctx, account.ID, machine.ID)
			if mp := machine.MaxProcesses(maxProcesses); mp != nil {
				limit = int(*mp)
			}
		}

	case policy.ProcessLeasingStrategy == "PER_MACHINE" && scope.Machine != nil:
		if machine, err := s.machineOfScope(ctx, account, license, *scope.Machine, s.now()); err == nil && machine.Alive() {
			count, _ = s.store.CountMachineProcesses(ctx, account.ID, machine.ID)
			if mp := machine.MaxProcesses(maxProcesses); mp != nil {
				limit = int(*mp)
			}
		}

	case policy.ProcessLeasingStrategy == "PER_LICENSE":
		count, _ = s.store.CountLicenseProcesses(ctx, account.ID, license.ID, nil)

		if maxProcesses != nil {
			limit = int(*maxProcesses)
		}

	case policy.ProcessLeasingStrategy == leasePerUser:
		count, _ = s.store.CountLicenseProcesses(ctx, account.ID, license.ID, scopeOwner)

		if maxProcesses != nil {
			limit = int(*maxProcesses)
		}
	default:
		if maxProcesses == nil {
			return Verdict{}, false
		}

		count, _ = s.store.CountLicenseProcesses(ctx, account.ID, license.ID, nil)
		limit = int(*maxProcesses)

		if count <= limit {
			return Verdict{}, false
		}
	}

	if count > limit {
		if overageCovers(policy, float64(count), float64(limit), !policy.Floating) {
			return Verdict{true, "has too many associated processes", "TOO_MANY_PROCESSES", nil, nil}, true
		}

		return Verdict{false, "has too many associated processes", "TOO_MANY_PROCESSES", nil, nil}, true
	}

	return Verdict{}, false
}

// firstAlive answers the first alive machine of the list.
func firstAlive(machines []domain.Machine) *domain.Machine {
	for index := range machines {
		if machines[index].Alive() {
			return &machines[index]
		}
	}

	return nil
}

// ownerMachines answers the owner-scoped machine sums.
func (s Scenario) ownerMachines(
	ctx context.Context, account domain.Account, license domain.License, owner *uuid.UUID,
) MachineSums {
	sums, err := s.store.SumLicenseMachinesForOwner(ctx, account.ID, license.ID, owner)
	if err != nil {
		return MachineSums{}
	}

	return sums
}

// machineOfScope loads the machine the machine-scope names (by id).
func (s Scenario) machineOfScope(
	ctx context.Context, account domain.Account, license domain.License, idOrFingerprint string, now time.Time,
) (domain.Machine, error) {
	if id, err := uuid.Parse(idOrFingerprint); err == nil {
		return s.store.LicenseMachineByID(ctx, account.ID, license.ID, id, now) //nolint:wrapcheck // the store's own error
	}

	return s.store.LicenseMachineByFingerprint( //nolint:wrapcheck // the store's own error
		ctx, account.ID, license.ID, idOrFingerprint, now,
	)
}

// machineOfFingerprint loads the license's machine by one fingerprint.
func (s Scenario) machineOfFingerprint(
	ctx context.Context, account domain.Account, license domain.License, fingerprint string, now time.Time,
) (domain.Machine, error) {
	return s.store.LicenseMachineByFingerprint( //nolint:wrapcheck // the store's own error
		ctx, account.ID, license.ID, fingerprint, now,
	)
}

// machinesOfFingerprints answers the license's machines matching the
// fingerprints.
func (s Scenario) machinesOfFingerprints(
	ctx context.Context, account domain.Account, license domain.License, fingerprints []string, now time.Time,
) []domain.Machine {
	machines, err := s.store.LicenseMachines(ctx, account.ID, license.ID, MachineFilter{}, now)
	if err != nil {
		return nil
	}

	wanted := map[string]bool{}
	for _, fingerprint := range fingerprints {
		wanted[fingerprint] = true
	}

	out := make([]domain.Machine, 0, len(machines))
	for i := range machines {
		if wanted[machines[i].Fingerprint] {
			out = append(out, machines[i])
		}
	}

	return out
}

// aliveMachinesForUser answers whether every alive machine is the
// user's (or unowned).
func (s Scenario) aliveMachinesForUser(machines []domain.Machine, ident string) bool {
	for i := range machines {
		if !machines[i].Alive() || !machines[i].OwnerID.Valid {
			continue
		}

		if machines[i].OwnerID.UUID.String() != ident {
			return false
		}
	}

	return true
}

// touch persists the validation touches and answers the license with
// them applied (the response renders the touched values).
func (s Scenario) touch(
	ctx context.Context, account domain.Account, license domain.License, verdict Verdict, now time.Time,
) domain.License {
	_ = s.store.TouchLicenseValidated(ctx, account.ID, license.ID, now, verdict.Checksum, verdict.Version)

	license.LastValidatedAt = &now
	license.LastValidatedChecksum = verdict.Checksum
	license.LastValidatedVersion = verdict.Version

	return license
}

// touchOnFirstValidation starts the FROM_FIRST_VALIDATION expiry
// clock.
func (s Scenario) touchOnFirstValidation(
	ctx context.Context, account domain.Account, license domain.License, now time.Time,
) error {
	if license.Expiry != nil || license.Policy.ExpirationBasis != "FROM_FIRST_VALIDATION" ||
		license.Policy.Duration == nil {
		return nil
	}

	expiry := now.Add(time.Duration(*license.Policy.Duration) * time.Second)

	if err := s.store.SetLicenseExpiry(ctx, account.ID, license.ID, &expiry, now); err != nil {
		return fmt.Errorf("licenses: first validation expiry: %w", err)
	}

	license.Expiry = &expiry

	return nil
}

func uniq(list []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list))

	for _, item := range list {
		if item == "" || seen[item] {
			continue
		}

		seen[item] = true
		out = append(out, item)
	}

	return out
}

// ceilHalf answers ceil(n/2).
func ceilHalf(n int) int {
	return (n + 1) / 2 //nolint:mnd // the reference's ceil(n/2.0)
}
