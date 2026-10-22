// License CRUD: index, show, create (key generation included), update
// and destroy.
package licenses

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// uuidRe mirrors the reference's UUID shape test.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Input is the decoded create/update body.
type Input struct {
	Name *string
	Key  *string
	// ID carries the privileged data.id of create.
	ID uuid.NullUUID

	Expiry       *time.Time
	ExpirySet    bool
	Protected    *bool
	ProtectedSet bool
	Suspended    *bool
	SuspendedSet bool

	MaxMachines     *int32
	MaxMachinesSet  bool
	MaxCores        *int32
	MaxCoresSet     bool
	MaxMemory       *int64
	MaxMemorySet    bool
	MaxDisk         *int64
	MaxDiskSet      bool
	MaxUses         *int32
	MaxUsesSet      bool
	MaxProcesses    *int32
	MaxProcessesSet bool
	MaxUsers        *int32
	MaxUsersSet     bool

	Metadata    map[string]any
	MetadataSet bool

	// PolicyID is the required policy relationship (create).
	PolicyID uuid.NullUUID
	// Owner/OwnerSet carry the owner (user) relationship.
	Owner    uuid.NullUUID
	OwnerSet bool
	// Group/GroupSet carry the group relationship.
	Group    uuid.NullUUID
	GroupSet bool
}

// Index answers the filtered page within the principal's scope.
func (s Scenario) Index(
	ctx context.Context, account domain.Account, f Filter, params paging.Params,
) (List, error) {
	if err := access.Require(ctx, "license.read"); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	scopeKind, scopeID := licenseScope(principal)

	window := params.Window()
	now := s.now()

	items, err := s.store.ListLicenses(ctx, account.ID, f, scopeKind, scopeID, window, now)
	if err != nil {
		return List{}, fmt.Errorf("licenses: index: %w", err)
	}

	// The user post-check needs each page item's attachment state.
	attached := map[uuid.UUID]bool{}

	if principal.Role == domain.RoleUser {
		for i := range items {
			var attachErr error

			attached[items[i].ID], attachErr = s.userAttached(ctx, account.ID, principal.BearerID, items[i])
			if attachErr != nil {
				return List{}, attachErr
			}
		}
	}

	if err := access.LicenseIndexPostCheck(principal, items, attached); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	out := List{Items: items, Count: len(items)}

	if params.Mode == paging.ModeOffset {
		count, err := s.store.CountLicenses(ctx, account.ID, f, scopeKind, scopeID, now)
		if err != nil {
			return List{}, fmt.Errorf("licenses: index count: %w", err)
		}

		out.Count = count
	}

	if params.Mode == paging.ModeCursor && len(items) > int(window.Limit) {
		out.Items = items[:window.Limit]
		next := out.Items[len(out.Items)-1].ID
		out.Next = uuid.NullUUID{UUID: next, Valid: true}
	}

	return out, nil
}

// Show answers one license by id or raw key.
func (s Scenario) Show(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	if err := access.Require(ctx, "license.read"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return domain.License{}, err
	}

	if err := access.LicenseShow(access.PrincipalOf(ctx), license, attached); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	return license, nil
}

// Create writes a new license.
func (s Scenario) Create( //nolint:cyclop,funlen // the creation ladder, in the reference's order
	ctx context.Context, account domain.Account, in Input,
) (domain.License, error) {
	if err := access.Require(ctx, "license.create"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if !in.PolicyID.Valid {
		return domain.License{}, apperr.Validation("POLICY_NOT_FOUND", "must exist",
			"/data/relationships/policy", "licenses/#licenses-object-relationships-policy")
	}

	policy, err := s.policies.PolicyByID(ctx, account.ID, in.PolicyID.UUID)
	if err != nil {
		// The denormalized product association fails with it.
		return domain.License{}, apperr.MultiOf( //nolint:wrapcheck // the document itself
			apperr.Validation("POLICY_NOT_FOUND", "must exist",
				"/data/relationships/policy", "licenses/#licenses-object-relationships-policy"),
			apperr.Validation("PRODUCT_NOT_FOUND", "must exist",
				"/data/relationships/product", "licenses/#licenses-object-relationships-product"),
		)
	}

	var owner domain.User

	if in.Owner.Valid {
		owner, err = s.users.UserByID(ctx, account.ID, in.Owner.UUID)
		if err != nil {
			return domain.License{}, apperr.Validation("USER_NOT_FOUND", "must exist",
				"/data/relationships/owner", "licenses/#licenses-object-relationships-owner")
		}
	}

	if in.Group.Valid {
		if _, err = s.groups.GroupByID(ctx, account.ID, in.Group.UUID); err != nil {
			return domain.License{}, apperr.Validation("GROUP_NOT_FOUND", "must exist",
				"/data/relationships/group", "licenses/#licenses-object-relationships-group")
		}
	}

	now := s.now()

	license := domain.License{
		ID:        uuid.New(),
		AccountID: account.ID,
		ProductID: policy.ProductID,
		PolicyID:  policy.ID,
		Policy:    policy,
		Name:      in.Name,
		Expiry:    in.Expiry,
		Uses:      0,
		Suspended: firstBool(in.Suspended, false),
		Protected: in.Protected,
		Metadata:  in.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if in.ID.Valid {
		license.ID = in.ID.UUID
	}

	if license.Metadata == nil {
		license.Metadata = map[string]any{}
	}

	if in.Owner.Valid {
		license.OwnerID = in.Owner
		email := owner.Email
		license.OwnerEmail = &email

		// The license inherits the owner's group when none was given.
		if !in.Group.Valid && owner.GroupID.Valid {
			license.GroupID = owner.GroupID
		}
	}

	if in.Group.Valid {
		license.GroupID = in.Group
	}

	// protected inherits the policy's flag when unset.
	if license.Protected == nil {
		license.Protected = policy.Protected
	}

	// The expiry clock starts at creation for FROM_CREATION policies
	// with a duration (an explicit expiry wins).
	if license.Expiry == nil && policy.ExpirationBasis == "FROM_CREATION" && policy.Duration != nil {
		expiry := now.Add(time.Duration(*policy.Duration) * time.Second)
		license.Expiry = &expiry
	}

	// A check-in policy anchors the first window at creation.
	if policy.RequireCheckIn {
		license.LastCheckInAt = &now
	}

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.setKey(ctx, account, &license, in.Key); err != nil {
			return err
		}

		if err := s.validate(ctx, account, license, true); err != nil {
			return err
		}

		if err := access.LicenseCreate(access.PrincipalOf(ctx), license); err != nil {
			return err //nolint:wrapcheck // the authorization's own error
		}

		if err := s.store.InsertLicense(ctx, license); err != nil {
			return fmt.Errorf("licenses: create: %w", err)
		}

		return s.publish(ctx, "license.created", license)
	}); err != nil {
		return domain.License{}, err
	}

	return license, nil
}

// Update merges the input over the license and writes it.
func (s Scenario) Update( //nolint:cyclop,funlen // one branch per attribute
	ctx context.Context, account domain.Account, idOrKey string, in Input,
) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := access.Require(ctx, "license.update"); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseUpdate(access.PrincipalOf(ctx), license); err != nil {
		return domain.License{}, err //nolint:wrapcheck // the authorization's own error
	}

	if in.Name != nil {
		license.Name = in.Name
	}

	if in.ExpirySet {
		license.Expiry = in.Expiry
	}

	if in.ProtectedSet {
		license.Protected = in.Protected
	}

	if in.SuspendedSet {
		license.Suspended = firstBool(in.Suspended, false)
	}

	if in.MaxMachinesSet {
		license.MaxMachinesOverride = in.MaxMachines
	}

	if in.MaxCoresSet {
		license.MaxCoresOverride = in.MaxCores
	}

	if in.MaxMemorySet {
		license.MaxMemoryOverride = in.MaxMemory
	}

	if in.MaxDiskSet {
		license.MaxDiskOverride = in.MaxDisk
	}

	if in.MaxUsesSet {
		license.MaxUsesOverride = in.MaxUses
	}

	if in.MaxProcessesSet {
		license.MaxProcessesOverride = in.MaxProcesses
	}

	if in.MaxUsersSet {
		license.MaxUsersOverride = in.MaxUsers
	}

	if in.MetadataSet {
		license.Metadata = in.Metadata
		if license.Metadata == nil {
			license.Metadata = map[string]any{}
		}
	}

	license.UpdatedAt = s.now()

	if err := s.validate(ctx, account, license, false); err != nil {
		return domain.License{}, err
	}

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateLicense(ctx, license); err != nil {
			return fmt.Errorf("licenses: update: %w", err)
		}

		return s.publish(ctx, "license.updated", license)
	}); err != nil {
		return domain.License{}, err
	}

	return license, nil
}

// Destroy removes the license (event license.deleted).
func (s Scenario) Destroy(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return domain.License{}, err
	}

	if err := access.Require(ctx, "license.delete"); err != nil {
		return license, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.LicenseDestroy(access.PrincipalOf(ctx), license); err != nil {
		return license, err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.publish(ctx, "license.deleted", license); err != nil {
			return err
		}

		if err := s.store.DeleteLicense(ctx, account.ID, license.ID); err != nil {
			return fmt.Errorf("licenses: destroy: %w", err)
		}

		return nil
	}); err != nil {
		return license, err
	}

	return license, nil
}

// load resolves the license within the principal's scope (the scoped
// find of the reference's set_license: a miss is the record 404).
func (s Scenario) load(ctx context.Context, account domain.Account, idOrKey string) (domain.License, error) {
	principal := access.PrincipalOf(ctx)

	var (
		license domain.License
		err     error
	)

	if uuidRe.MatchString(idOrKey) {
		license, err = s.store.LicenseByID(ctx, account.ID, uuid.MustParse(idOrKey))
	} else {
		license, err = s.store.LicenseByKey(ctx, account.ID, idOrKey)
	}

	if errors.Is(err, ErrNotFound) {
		return domain.License{}, apperr.NotFound("license", idOrKey)
	}

	if err != nil {
		return domain.License{}, fmt.Errorf("licenses: load: %w", err)
	}

	switch licenseScopeOnly(principal) {
	case "product":
		if license.ProductID != principal.BearerID {
			return domain.License{}, apperr.NotFound("license", idOrKey)
		}
	case "user":
		attached, err := s.userAttached(ctx, account.ID, principal.BearerID, license)
		if err != nil {
			return domain.License{}, err
		}

		if !access.LicenseVisibleToUser(principal, license, attached) {
			return domain.License{}, apperr.NotFound("license", idOrKey)
		}
	case "license":
		if license.ID != principal.BearerID {
			return domain.License{}, apperr.NotFound("license", idOrKey)
		}
	}

	return license, nil
}

// userAttached answers whether the user is a licensee of the license.
func (s Scenario) userAttached(ctx context.Context, accountID, userID uuid.UUID, license domain.License) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}

	attached, err := s.store.LicenseeIsUser(ctx, accountID, license.ID, userID)
	if err != nil {
		return false, fmt.Errorf("licenses: user attached: %w", err)
	}

	return attached, nil
}

// setKey resolves the license's key: the custom key (crypting scheme
// policies' templates) or a generated one (pool, seed or random).
func (s Scenario) setKey(ctx context.Context, account domain.Account, license *domain.License, custom *string) error {
	policy := license.Policy

	if custom != nil && *custom != "" {
		license.Key = *custom

		if policy.Encrypted && policy.Scheme == "LEGACY_ENCRYPT" {
			return apperr.Validation("KEY_NOT_SUPPORTED",
				"cannot be specified for a legacy encrypted license",
				"/data/attributes/key", "licenses/#licenses-object-attrs-key")
		}

		if policy.Scheme != "" {
			in := s.keyInput(account, *license)
			seed := crypto.SeedKey(in, *custom)
			key, err := crypto.SchemeKey(in, seed)

			switch {
			case err == nil:
				license.Key = key
			case errors.Is(err, crypto.ErrKeyTooLarge):
				return apperr.Validation("KEY_BYTE_SIZE_EXCEEDED",
					"key exceeds maximum byte length (max size of 245 bytes)",
					"/data/attributes/key", "licenses/#licenses-object-attrs-key")
			case errors.Is(err, crypto.ErrJWTClaimsInvalid):
				return apperr.Validation("KEY_JWT_CLAIMS_INVALID",
					"key is not a valid JWT claims payload (must be a valid JSON encoded string)",
					"/data/attributes/key", "licenses/#licenses-object-attrs-key")
			default:
				return fmt.Errorf("licenses: crypt key: %w", err)
			}
		}

		return nil
	}

	switch {
	case policy.UsePool:
		key, err := s.pool.PopPolicyKey(ctx, account.ID, policy.ID)
		if err != nil {
			return apperr.Validation("POLICY_POOL_EMPTY", "pool is empty",
				"/data/relationships/policy", "licenses/#licenses-object-relationships-policy")
		}

		license.Key = key.Key

		return nil
	default:
		in := s.keyInput(account, *license)

		key, raw, err := crypto.GenerateKey(in)
		if err != nil {
			return fmt.Errorf("licenses: generate key: %w", err)
		}

		license.Key = key
		license.RawKey = raw

		return nil
	}
}

func (s Scenario) keyInput(account domain.Account, license domain.License) crypto.KeyInput {
	return crypto.KeyInput{
		Keys:      account.Keys,
		AccountID: account.ID,
		ProductID: license.ProductID,
		Policy: crypto.PolicyKeyFacts{
			ID:        license.PolicyID,
			Duration:  license.Policy.Duration,
			Scheme:    license.Policy.Scheme,
			Encrypted: license.Policy.Encrypted,
		},
		LicenseID:  license.ID,
		OwnerID:    license.OwnerID,
		OwnerEmail: license.OwnerEmail,
		CreatedAt:  license.CreatedAt,
		Expiry:     license.Expiry,
		RNG:        s.rng,
	}
}

// validate runs the model validations in the reference's order.
func (s Scenario) validate( //nolint:gocognit,gocyclo,cyclop,funlen // the table, verbatim
	ctx context.Context, account domain.Account, license domain.License, create bool,
) error {
	var errs []*apperr.T

	attr := func(code, detail, name string) {
		errs = append(errs, apperr.Validation(code, detail,
			"/data/attributes/"+name, "licenses/#licenses-object-attrs-"+name))
	}

	rel := func(code, detail, name string) {
		errs = append(errs, apperr.Validation(code, detail,
			"/data/relationships/"+name, "licenses/#licenses-object-relationships-"+name))
	}

	// data.id conflicts (create only).
	if create && license.ID != uuid.Nil {
		exists, err := s.store.LicenseIDExists(ctx, account.ID, license.ID)
		if err != nil {
			return fmt.Errorf("licenses: id exists: %w", err)
		}

		if exists {
			attr("ID_CONFLICT", "must not conflict with another license", "id")
		}
	}

	// The key validations (create only; the key is immutable).
	if create { //nolint:nestif // the key validations, verbatim
		if uuidRe.MatchString(license.Key) {
			exists, err := s.store.LicenseIDExists(ctx, account.ID, uuid.MustParse(license.Key))
			if err != nil {
				return fmt.Errorf("licenses: key conflict: %w", err)
			}

			if exists {
				attr("KEY_CONFLICT", "must not conflict with another license's identifier (UUID)", "key")
			}
		}

		if license.Key == "actions" || license.Key == "action" {
			attr("KEY_NOT_ALLOWED", "is reserved", "key")
		}

		taken, err := s.store.LicenseKeyTaken(ctx, account.ID, license.Key)
		if err != nil {
			return fmt.Errorf("licenses: key taken: %w", err)
		}

		if taken {
			attr("KEY_TAKEN", "has already been taken", "key")
		}

		if license.Policy.Scheme == "" && len(license.Key) < 6 {
			switch {
			case len(license.Key) < 1:
				attr("KEY_TOO_SHORT", "is too short (minimum is 1 character)", "key")
			default:
				attr("KEY_TOO_SHORT", "is too short (minimum is 6 characters)", "key")
			}
		}

		if len(license.Key) > 16*1024 {
			attr("KEY_TOO_LONG", "is too long (maximum is 16384 characters)", "key")
		}
	}

	// Group limits.
	if license.GroupID.Valid { //nolint:nestif // the group-limit validation
		group, err := s.groups.GroupByID(ctx, account.ID, license.GroupID.UUID)
		if err == nil && group.MaxLicenses != nil {
			count, err := s.store.CountLicensesOfGroup(ctx, account.ID, group.ID)
			if err != nil {
				return fmt.Errorf("licenses: group count: %w", err)
			}

			if count >= *group.MaxLicenses {
				rel("GROUP_LICENSE_LIMIT_EXCEEDED",
					fmt.Sprintf("license count has exceeded maximum allowed by current group (%d)", *group.MaxLicenses),
					"group")
			}
		}
	}

	// The owner may not also be a licensee.
	if license.OwnerID.Valid {
		licensee, err := s.store.LicenseeIsUser(ctx, account.ID, license.ID, license.OwnerID.UUID)
		if err != nil {
			return fmt.Errorf("licenses: owner licensee: %w", err)
		}

		if licensee {
			rel("OWNER_INVALID", "already exists (user is attached through users)", "owner")
		}
	}

	if license.Name != nil && len(*license.Name) > 1024 {
		attr("NAME_TOO_LONG", "is too long (maximum is 1024 characters)", "name")
	}

	if detail := domain.MetadataViolation(license.Metadata); detail != "" {
		attr("METADATA_TOO_LONG", detail, "metadata")
	}

	num := func(value *int32, minimum int32, code, name string) {
		if value == nil {
			return
		}

		if *value < minimum {
			attr(code, fmt.Sprintf("must be greater than or equal to %d", minimum), name)
		}
	}

	if m := license.MaxMachinesOverride; m != nil {
		switch {
		case *m < 0:
			attr("MAX_MACHINES_INVALID", "must be greater than or equal to 0", "maxMachines")
		case license.Policy.Floating && *m < 1:
			attr("MAX_MACHINES_INVALID", "must be greater than or equal to 1 for floating policy", "maxMachines")
		case !license.Policy.Floating && *m != 1:
			attr("MAX_MACHINES_INVALID", "must be equal to 1 for non-floating policy", "maxMachines")
		}
	}

	num(license.MaxCoresOverride, 1, "MAX_CORES_INVALID", "maxCores")
	num(license.MaxUsesOverride, 0, "MAX_USES_INVALID", "maxUses")
	num(license.MaxProcessesOverride, 0, "MAX_PROCESSES_INVALID", "maxProcesses")
	num(license.MaxUsersOverride, 0, "MAX_USERS_INVALID", "maxUsers")

	bignum := func(value *int64, minimum int64, code, name string) {
		if value == nil {
			return
		}

		if *value < minimum {
			attr(code, fmt.Sprintf("must be greater than or equal to %d", minimum), name)
		}
	}

	bignum(license.MaxMemoryOverride, 1, "MAX_MEMORY_INVALID", "maxMemory")
	bignum(license.MaxDiskOverride, 1, "MAX_DISK_INVALID", "maxDisk")

	if err := apperr.MultiOf(errs...); err != nil {
		return err //nolint:wrapcheck // the document itself
	}

	return nil
}

// licenseScope answers the index scope of the principal.
func licenseScope(principal access.Principal) (string, uuid.UUID) {
	switch principal.Role {
	case domain.RoleProduct:
		return "product", principal.BearerID
	case domain.RoleUser:
		return "user", principal.BearerID
	case domain.RoleLicense:
		return "license", principal.BearerID
	default:
		return "", uuid.Nil
	}
}

// licenseScopeOnly answers the scope kind alone.
func licenseScopeOnly(principal access.Principal) string {
	kind, _ := licenseScope(principal)

	return kind
}

func firstBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}

	return *p
}
