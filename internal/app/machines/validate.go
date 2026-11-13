package machines

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// violations collects a record's validation errors the way the
// reference serializes them: grouped by attribute in the order each
// attribute first failed, duplicates (same code and source) dropped.
type violations struct {
	// topic is the docs topic of the record ("machines" when empty).
	topic string
	// codePrefix names a nested record's errors (COMPONENTS_…).
	codePrefix string
	errs       []*apperr.T
}

func (v *violations) add(code, detail, pointer string) {
	code = v.codePrefix + code

	for _, e := range v.errs {
		if e.Code == code && e.Source.Pointer == pointer {
			return
		}
	}

	topic := v.topic
	if topic == "" {
		topic = "machines"
	}

	v.errs = append(v.errs, apperr.Validation(code, detail, pointer, aboutOf(topic, pointer)))
}

func (v *violations) err() error {
	if len(v.errs) == 0 {
		return nil
	}

	order := map[string]int{}
	for i, e := range v.errs {
		if _, seen := order[e.Source.Pointer]; !seen {
			order[e.Source.Pointer] = i
		}
	}

	slices.SortStableFunc(v.errs, func(a, b *apperr.T) int {
		return order[a.Source.Pointer] - order[b.Source.Pointer]
	})

	return apperr.MultiOf(v.errs...)
}

const (
	pointerBase        = "/data"
	pointerFingerprint = "/data/attributes/fingerprint"
	pointerOwner       = "/data/relationships/owner"
	pointerGroup       = "/data/relationships/group"
)

// validateCreate runs the model's create validations in its order.
func (s Scenario) validateCreate(
	ctx context.Context, account domain.Account, license domain.License, licensed bool, machine domain.Machine, in Input,
) error {
	var v violations

	if !licensed {
		v.add("LICENSE_NOT_FOUND", "must exist", "/data/relationships/license")
		v.add("POLICY_NOT_FOUND", "must exist", "/data/relationships/policy")
	}

	owner, group, err := s.relations(ctx, account, machine, &v)
	if err != nil {
		return err
	}

	if err := s.validateNested(ctx, account, license, licensed, machine, in.Components, &v); err != nil {
		return err
	}

	if err := s.validateFingerprint(ctx, account, license, licensed, machine, &v); err != nil {
		return err
	}

	validateAttributes(machine, &v)

	if in.ID != "" {
		if err := s.validateID(ctx, account, in.ID, &v); err != nil {
			return err
		}
	}

	if uuidRe.MatchString(machine.Fingerprint) {
		exists, err := s.store.MachineExists(ctx, account.ID, uuid.MustParse(machine.Fingerprint))
		if err != nil {
			return fmt.Errorf("machines: fingerprint conflict: %w", err)
		}

		if exists {
			v.add("FINGERPRINT_CONFLICT", "must not conflict with another machine's identifier (UUID)", pointerFingerprint)
		}
	}

	if licensed {
		if err := s.validateLimits(ctx, license, machine, ownerOf(owner), true, &v); err != nil {
			return err
		}

		if err := s.validateUniqueness(ctx, account, license, machine, &v); err != nil {
			return err
		}
	}

	if err := s.validateGroupLimit(ctx, group, &v); err != nil {
		return err
	}

	if err := s.validateOwner(ctx, license, licensed, owner, &v); err != nil {
		return err
	}

	return v.err()
}

// validateUpdate runs the model's update validations.
func (s Scenario) validateUpdate(
	ctx context.Context, account domain.Account, license domain.License, before, machine domain.Machine,
) error {
	var v violations

	owner, group, err := s.relations(ctx, account, machine, &v)
	if err != nil {
		return err
	}

	validateAttributes(machine, &v)

	if err := s.validateLimits(ctx, license, machine, ownerOf(owner), false, &v); err != nil {
		return err
	}

	if machine.GroupID != before.GroupID {
		if err := s.validateGroupLimit(ctx, group, &v); err != nil {
			return err
		}
	}

	if machine.OwnerID != before.OwnerID {
		if err := s.validateOwner(ctx, license, true, owner, &v); err != nil {
			return err
		}
	}

	return v.err()
}

// validateNested runs the component validations over an activation's
// nested components: a fingerprint repeated among them "is duplicated".
func (s Scenario) validateNested(
	ctx context.Context, account domain.Account, license domain.License, licensed bool, machine domain.Machine,
	components []ComponentInput, v *violations,
) error {
	seen := map[string]int{}
	for _, c := range components {
		seen[c.Fingerprint]++
	}

	for i, c := range components {
		nested := violations{topic: v.topic, codePrefix: "COMPONENTS_"}
		prefix := "/data/relationships/components/data/" + strconv.Itoa(i) + "/attributes/"
		component := domain.MachineComponent{Name: c.Name, Fingerprint: c.Fingerprint, Metadata: c.Metadata}

		validateComponentAttributes(component, prefix, &nested)

		if seen[c.Fingerprint] > 1 {
			nested.add("FINGERPRINT_CONFLICT", "is duplicated", prefix+"fingerprint")
		}

		if licensed {
			err := s.validateComponentUniqueness(ctx, account, license, machine.ID, c.Fingerprint, prefix, &nested)
			if err != nil {
				return err
			}
		}

		v.errs = append(v.errs, nested.errs...)
	}

	return nil
}

// relations resolves the owner and group: a missing one "must exist".
func (s Scenario) relations(
	ctx context.Context, account domain.Account, machine domain.Machine, v *violations,
) (*domain.User, *domain.Group, error) {
	var (
		owner *domain.User
		group *domain.Group
	)

	if machine.OwnerID.Valid {
		user, err := s.directory.UserByID(ctx, account.ID, machine.OwnerID.UUID)

		switch {
		case err == nil:
			owner = &user
		case errors.Is(err, ErrNotFound):
			v.add("OWNER_NOT_FOUND", "must exist", pointerOwner)
		default:
			return nil, nil, fmt.Errorf("machines: owner: %w", err)
		}
	}

	if machine.GroupID.Valid {
		found, err := s.directory.GroupByID(ctx, account.ID, machine.GroupID.UUID)

		switch {
		case err == nil:
			group = &found
		case errors.Is(err, ErrNotFound):
			v.add("GROUP_NOT_FOUND", "must exist", pointerGroup)
		default:
			return nil, nil, fmt.Errorf("machines: group: %w", err)
		}
	}

	return owner, group, nil
}

// validateFingerprint is the fingerprint's uniqueness within the
// license, exclusion, length and presence.
func (s Scenario) validateFingerprint(
	ctx context.Context, account domain.Account, license domain.License, licensed bool, machine domain.Machine,
	v *violations,
) error {
	if licensed {
		taken, err := s.store.FingerprintTaken(ctx, account.ID,
			Uniqueness{Kind: kindLicense, ID: license.ID}, machine.Fingerprint)
		if err != nil {
			return fmt.Errorf("machines: fingerprint taken: %w", err)
		}

		if taken {
			v.add("FINGERPRINT_TAKEN", "has already been taken", pointerFingerprint)
		}
	}

	if isReservedWord(machine.Fingerprint) {
		v.add("FINGERPRINT_NOT_ALLOWED", "is reserved", pointerFingerprint)
	}

	if tooLong(&machine.Fingerprint, maxFingerprintLength) {
		v.add("FINGERPRINT_TOO_LONG", "is too long (maximum is 4096 characters)", pointerFingerprint)
	}

	if machine.Fingerprint == "" {
		v.add("FINGERPRINT_MISSING", "can't be blank", pointerFingerprint)
	}

	return nil
}

// validateAttributes checks metadata, the resources and the text
// lengths.
func validateAttributes(machine domain.Machine, v *violations) {
	if detail := domain.MetadataViolation(machine.Metadata); detail != "" {
		v.add("METADATA_TOO_LONG", detail, "/data/attributes/metadata")
	}

	if machine.Cores != nil && *machine.Cores < 1 {
		v.add("CORES_INVALID", "must be greater than or equal to 1", "/data/attributes/cores")
	}

	if machine.Memory != nil && *machine.Memory < 1 {
		v.add("MEMORY_INVALID", "must be greater than or equal to 1", "/data/attributes/memory")
	}

	if machine.Disk != nil && *machine.Disk < 1 {
		v.add("DISK_INVALID", "must be greater than or equal to 1", "/data/attributes/disk")
	}

	for _, text := range []struct {
		value *string
		code  string
		name  string
	}{
		{machine.IP, "IP_TOO_LONG", "ip"},
		{machine.Hostname, "HOSTNAME_TOO_LONG", "hostname"},
		{machine.Platform, "PLATFORM_TOO_LONG", "platform"},
		{machine.Name, "NAME_TOO_LONG", "name"},
	} {
		if tooLong(text.value, maxTextLength) {
			v.add(text.code, "is too long (maximum is 255 characters)", "/data/attributes/"+text.name)
		}
	}
}

func (s Scenario) validateID(ctx context.Context, account domain.Account, id string, v *violations) error {
	if !uuidRe.MatchString(id) {
		v.add("ID_INVALID", "must be a valid UUID", "/data/id")

		return nil
	}

	exists, err := s.store.MachineExists(ctx, account.ID, uuid.MustParse(id))
	if err != nil {
		return fmt.Errorf("machines: id conflict: %w", err)
	}

	if exists {
		v.add("ID_CONFLICT", "must not conflict with another machine", "/data/id")
	}

	return nil
}

// ownerOf is the owner the per-user limits count by: an owner that does
// not exist counts as none (record.owner is nil there).
func ownerOf(owner *domain.User) Owner {
	if owner == nil {
		return Owner{}
	}

	return Owner{UUID: owner.ID, Valid: true}
}

// validateLimits checks the machine count (create only) and the core,
// memory and disk totals against the license, per license or per owner,
// within the policy's overage allowance.
func (s Scenario) validateLimits(
	ctx context.Context, license domain.License, machine domain.Machine, owner Owner, create bool, v *violations,
) error {
	policy := license.Policy
	if policy.OverageStrategy == "ALWAYS_ALLOW_OVERAGE" {
		return nil
	}

	perUser := policy.MachineLeasePerUser()
	holder := kindLicense

	if perUser {
		holder = kindUser
	}

	totals, err := s.store.MachineTotals(ctx, license.ID, perUser, owner, machine.ID)
	if err != nil {
		return fmt.Errorf("machines: totals: %w", err)
	}

	if limit := license.MaxMachines(); create && limit != nil {
		prev := totals.Count
		if !perUser {
			prev = int64(license.MachinesCount)
		}

		if next := prev + 1; prev > 0 && exceeds(policy, next, int64(*limit)) {
			v.add("MACHINE_LIMIT_EXCEEDED", fmt.Sprintf("machine count has exceeded maximum allowed for %s (%d)",
				holder, *limit), pointerBase)
		}
	}

	resources := []struct {
		limit *int64
		next  int64
		code  string
		what  string
	}{
		{
			int64Of(license.MaxCores()), totals.Cores + int64(valueOr(machine.Cores)),
			"MACHINE_CORE_LIMIT_EXCEEDED", "machine core count",
		},
		{
			license.MaxMemory(), totals.Memory + valueOr(machine.Memory),
			"MACHINE_MEMORY_LIMIT_EXCEEDED", "machine memory",
		},
		{license.MaxDisk(), totals.Disk + valueOr(machine.Disk), "MACHINE_DISK_LIMIT_EXCEEDED", "machine disk"},
	}

	for _, r := range resources {
		if r.limit != nil && exceeds(policy, r.next, *r.limit) {
			v.add(r.code, fmt.Sprintf("%s has exceeded maximum allowed for %s (%d)", r.what, holder, *r.limit), pointerBase)
		}
	}

	return nil
}

// exceeds answers whether next is past limit and not covered by the
// overage allowance.
func exceeds(policy domain.Policy, next, limit int64) bool {
	return next > limit && !policy.WithinOverage(float64(next), float64(limit))
}

// validateUniqueness is the fingerprint against the policy's machine
// uniqueness strategy.
func (s Scenario) validateUniqueness(
	ctx context.Context, account domain.Account, license domain.License, machine domain.Machine, v *violations,
) error {
	scope, detail := Uniqueness{Kind: kindLicense, ID: license.ID}, "has already been taken"

	switch license.Policy.MachineUniqueness() {
	case "UNIQUE_PER_ACCOUNT":
		scope, detail = Uniqueness{Kind: "account", ID: account.ID}, "has already been taken for this account"
	case "UNIQUE_PER_PRODUCT":
		scope, detail = Uniqueness{Kind: kindProduct, ID: license.ProductID}, "has already been taken for this product"
	case "UNIQUE_PER_POLICY":
		scope, detail = Uniqueness{Kind: "policy", ID: license.PolicyID}, "has already been taken for this policy"
	}

	taken, err := s.store.FingerprintTaken(ctx, account.ID, scope, machine.Fingerprint)
	if err != nil {
		return fmt.Errorf("machines: fingerprint uniqueness: %w", err)
	}

	if taken {
		v.add("FINGERPRINT_TAKEN", detail, pointerFingerprint)
	}

	return nil
}

// validateGroupLimit checks the group's machine limit.
func (s Scenario) validateGroupLimit(ctx context.Context, group *domain.Group, v *violations) error {
	if group == nil || group.MaxMachines == nil {
		return nil
	}

	count, err := s.store.GroupMachineCount(ctx, group.ID)
	if err != nil {
		return fmt.Errorf("machines: group count: %w", err)
	}

	if count >= *group.MaxMachines {
		v.add("GROUP_MACHINE_LIMIT_EXCEEDED",
			"machine count has exceeded maximum allowed by current group ("+strconv.Itoa(*group.MaxMachines)+")",
			pointerGroup)
	}

	return nil
}

// validateOwner asserts the owner is a license user (its owner or a
// licensee).
func (s Scenario) validateOwner(
	ctx context.Context, license domain.License, licensed bool, owner *domain.User, v *violations,
) error {
	if !licensed || owner == nil {
		return nil
	}

	member, err := s.store.LicenseHasUser(ctx, license.ID, owner.ID)
	if err != nil {
		return fmt.Errorf("machines: license user: %w", err)
	}

	if !member {
		v.add("OWNER_INVALID", "must be a valid license user", pointerOwner)
	}

	return nil
}

func int64Of(v *int32) *int64 {
	if v == nil {
		return nil
	}

	out := int64(*v)

	return &out
}

func valueOr[T int32 | int64](v *T) T {
	if v == nil {
		return 0
	}

	return *v
}
