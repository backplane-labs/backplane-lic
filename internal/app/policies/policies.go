// Package policies is the policy scenario: CRUD over the full
// attribute surface, the product/license relationship routes, the key
// pool and entitlement attach/detach.
package policies

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// ErrEntitlementTaken: the attach hit an existing join row.
var ErrEntitlementTaken = errors.New("policies: entitlement taken")

// Store is the scenario's consumer-side storage port.
type Store interface {
	// PolicyByID loads a policy; ErrNotFound when none.
	PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error)
	// PolicyByLicense loads the license bearer's own policy.
	PolicyByLicense(ctx context.Context, accountID, licenseID uuid.UUID) (domain.Policy, error)
	// InsertPolicy writes a new row.
	InsertPolicy(ctx context.Context, policy domain.Policy) error
	// UpdatePolicy overwrites the mutable columns.
	UpdatePolicy(ctx context.Context, policy domain.Policy) error
	// DeletePolicy destroys the row; false when gone.
	DeletePolicy(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	// ListPolicies pages the admin-side scope (with the product filter).
	ListPolicies(ctx context.Context, accountID uuid.UUID, filterProductID *uuid.UUID, page paging.Page) ([]domain.Policy, error) //nolint:lll // the wire name is long
	// CountPolicies counts the same scope.
	CountPolicies(ctx context.Context, accountID uuid.UUID, filterProductID *uuid.UUID) (int, error)
	// ListProductPolicies pages the product-bearer scope.
	ListProductPolicies(ctx context.Context, accountID, productID uuid.UUID, page paging.Page) ([]domain.Policy, error)
	// CountProductPolicies counts the product-bearer scope.
	CountProductPolicies(ctx context.Context, accountID, productID uuid.UUID) (int, error)
	// UserPolicyIDs answers the policies of the user's licenses.
	UserPolicyIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
}

// Input is the request body of create and update (decoded and
// shape-validated by the handler). The Set flags distinguish an absent
// attribute from an explicit null (the nullable numerics); the
// pointers distinguish false from unset for the booleans.
type Input struct {
	Name *string

	Duration    *int32
	DurationSet bool
	Strict      *bool
	Floating    *bool
	UsePool     *bool

	MaxMachines     *int32
	MaxMachinesSet  bool
	MaxProcesses    *int32
	MaxProcessesSet bool
	MaxUsers        *int32
	MaxUsersSet     bool
	MaxCores        *int32
	MaxCoresSet     bool
	MaxMemory       *int64
	MaxMemorySet    bool
	MaxDisk         *int64
	MaxDiskSet      bool
	MaxUses         *int32
	MaxUsesSet      bool

	MachineUniquenessStrategy   *string
	MachineMatchingStrategy     *string
	ComponentUniquenessStrategy *string
	ComponentMatchingStrategy   *string
	ExpirationStrategy          *string
	ExpirationBasis             *string
	RenewalBasis                *string
	TransferStrategy            *string
	AuthenticationStrategy      *string
	MachineLeasingStrategy      *string
	ProcessLeasingStrategy      *string
	OverageStrategy             *string
	Scheme                      *string
	Encrypted                   *bool
	Protected                   *bool
	ProtectedSet                bool

	RequireProductScope           *bool
	RequirePolicyScope            *bool
	RequireMachineScope           *bool
	RequireFingerprintScope       *bool
	RequireComponentsScope        *bool
	RequireUserScope              *bool
	RequireChecksumScope          *bool
	RequireVersionScope           *bool
	RequireCheckIn                *bool
	CheckInInterval               *string
	CheckInIntervalCount          *int32
	CheckInIntervalCountSet       bool
	HeartbeatDuration             *int32
	HeartbeatDurationSet          bool
	HeartbeatCullStrategy         *string
	HeartbeatResurrectionStrategy *string
	HeartbeatBasis                *string
	RequireHeartbeat              *bool

	Metadata    map[string]any
	MetadataSet bool

	// ProductID carries the create-only product relationship.
	ProductID uuid.UUID
	ProductOn bool
}

// ProductStore is the product seam (the products repository).
type ProductStore interface {
	// ProductByID loads a product (the create target and the product
	// relationship); products.ErrNotFound.
	ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error)
}

// EntitlementStore is the entitlement seam (the entitlements
// repository).
type EntitlementStore interface {
	// EntitlementByID loads an entitlement; entitlements.ErrNotFound.
	EntitlementByID(ctx context.Context, accountID, id uuid.UUID) (domain.Entitlement, error)
	// EntitlementByCode loads an entitlement by code.
	EntitlementByCode(ctx context.Context, accountID uuid.UUID, code string) (domain.Entitlement, error)
	// ListPolicyEntitlements answers the policy's join rows.
	ListPolicyEntitlements(ctx context.Context, accountID, policyID uuid.UUID) ([]domain.PolicyEntitlement, error)
	// PolicyEntitlement answers one join row; entitlements.ErrNotFound.
	PolicyEntitlement(ctx context.Context, accountID, policyID, entitlementID uuid.UUID) (domain.PolicyEntitlement, error)
	// InsertPolicyEntitlements writes the join rows of one attach;
	// ErrEntitlementTaken on a collision.
	InsertPolicyEntitlements(
		ctx context.Context, accountID, policyID, joinID uuid.UUID, entitlementIDs []uuid.UUID,
	) error
	// DeletePolicyEntitlements removes the join rows; the answer lists
	// the ids that were actually attached.
	DeletePolicyEntitlements(
		ctx context.Context, accountID, policyID uuid.UUID, entitlementIDs []uuid.UUID,
	) ([]uuid.UUID, error)
}

// KeyStore is the key-pool seam (the keys repository).
type KeyStore interface {
	// ListPolicyPool pages the policy's key pool.
	ListPolicyPool(ctx context.Context, accountID, policyID uuid.UUID, page paging.Page) ([]domain.Key, error)
	// CountPolicyPool counts the pool.
	CountPolicyPool(ctx context.Context, accountID, policyID uuid.UUID) (int, error)
	// PopPolicyKey destroys the newest pooled key and answers it;
	// keys.ErrPoolEmpty when nothing is left.
	PopPolicyKey(ctx context.Context, accountID, policyID uuid.UUID) (domain.Key, error)
}

// Scenario runs the policy reads and writes.
type Scenario struct {
	store        Store
	products     ProductStore
	entitlements EntitlementStore
	pool         KeyStore
	events       events.Publisher
	clock        func() time.Time
}

// New builds the scenario over the policies, products, entitlements
// and keys repositories.
func New(
	store Store, productStore ProductStore, entitlementStore EntitlementStore, keyStore KeyStore,
	publisher events.Publisher, clock func() time.Time,
) Scenario {
	return Scenario{
		store: store, products: productStore, entitlements: entitlementStore, pool: keyStore,
		events: publisher, clock: clock,
	}
}

// List is one index page.
type List struct {
	Items []domain.Policy
	Count int
	Next  uuid.NullUUID
}

// Index answers the policies page (user and license bearers load the
// same page the admin does and fail the post-check unless everything
// on it is theirs).
func (s Scenario) Index( //nolint:cyclop // one branch per rule
	ctx context.Context, account domain.Account, filterProductID *uuid.UUID, params paging.Params,
) (List, error) {
	if err := access.Require(ctx, "policy.read"); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	var userPolicies map[uuid.UUID]struct{}

	licensePolicyID := uuid.Nil

	switch principal.Role { //nolint:exhaustive // the default branch covers the rest
	case domain.RoleUser:
		ids, err := s.store.UserPolicyIDs(ctx, account.ID, principal.BearerID)
		if err != nil {
			return List{}, fmt.Errorf("policies: index: %w", err)
		}

		userPolicies = idSet(ids)
	case domain.RoleLicense:
		if principal.License != nil {
			policy, err := s.store.PolicyByLicense(ctx, account.ID, principal.License.ID)
			if errors.Is(err, ErrNotFound) { //nolint:gocritic // mirrors the reference's shape
				licensePolicyID = uuid.Nil
			} else if err != nil {
				return List{}, fmt.Errorf("policies: index: %w", err)
			} else {
				licensePolicyID = policy.ID
			}
		}
	}

	window := params.Window()

	var (
		items []domain.Policy
		count int
		err   error
	)

	if principal.Role == domain.RoleProduct {
		items, err = s.store.ListProductPolicies(ctx, account.ID, principal.BearerID, window)
	} else {
		items, err = s.store.ListPolicies(ctx, account.ID, filterProductID, window)
	}

	if err != nil {
		return List{}, fmt.Errorf("policies: index: %w", err)
	}

	if err := access.PolicyIndexPostCheck(principal, items, userPolicies, licensePolicyID); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	out := List{Items: items, Count: len(items)}

	if params.Mode == paging.ModeOffset {
		if principal.Role == domain.RoleProduct {
			count, err = s.store.CountProductPolicies(ctx, account.ID, principal.BearerID)
		} else {
			count, err = s.store.CountPolicies(ctx, account.ID, filterProductID)
		}

		if err != nil {
			return List{}, fmt.Errorf("policies: index count: %w", err)
		}

		out.Count = count
	}

	if params.Mode == paging.ModeCursor && len(items) > int(window.Limit) {
		out.Items = items[:window.Limit]
		out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
	}

	return out, nil
}

// Show answers one policy by id.
func (s Scenario) Show(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Policy, error) {
	if err := access.Require(ctx, "policy.read"); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Policy{}, err
	}

	if err := s.showCheck(ctx, account.ID, policy); err != nil {
		return domain.Policy{}, err
	}

	return policy, nil
}

// showCheck runs the show ownership rules for the loaded policy.
func (s Scenario) showCheck(ctx context.Context, accountID uuid.UUID, policy domain.Policy) error {
	principal := access.PrincipalOf(ctx)

	ownedByUser, ownedByLicense := false, false

	switch principal.Role { //nolint:exhaustive // the default branch covers the rest
	case domain.RoleUser:
		ids, err := s.store.UserPolicyIDs(ctx, accountID, principal.BearerID)
		if err != nil {
			return fmt.Errorf("policies: show: %w", err)
		}

		_, ownedByUser = idSet(ids)[policy.ID]
	case domain.RoleLicense:
		if principal.License != nil {
			own, err := s.store.PolicyByLicense(ctx, accountID, principal.License.ID)
			if err != nil {
				return fmt.Errorf("policies: show: %w", err)
			}

			ownedByLicense = own.ID == policy.ID
		}
	}

	if err := access.PolicyShow(principal, policy, ownedByUser, ownedByLicense); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	return nil
}

// Create writes a new policy.
func (s Scenario) Create(ctx context.Context, account domain.Account, in Input) (domain.Policy, error) {
	if err := access.Require(ctx, "policy.create"); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	// The product relationship is required; its existence is a model
	// validation (PRODUCT_NOT_FOUND).
	product, err := s.products.ProductByID(ctx, account.ID, in.ProductID)
	if errors.Is(err, products.ErrNotFound) {
		return domain.Policy{}, apperr.Validation("PRODUCT_NOT_FOUND", "must exist",
			"/data/relationships/product", "policies/#policies-object-relationships-product")
	}

	if err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the product lookup's own error
	}

	now := s.clock()

	policy := domain.PolicyCreateDefaults()
	policy.ID = uuid.New()
	policy.AccountID = account.ID
	policy.ProductID = product.ID
	policy.Metadata = map[string]any{}
	policy.CreatedAt = now
	policy.UpdatedAt = now
	policy.Protected = &account.Protected

	s.applyInput(&policy, in, true)

	// A node-locked policy with no explicit machine limit gets exactly
	// one machine (the reference's before_validation).
	if !policy.Floating && !in.MaxMachinesSet {
		one := int32(1)
		policy.MaxMachines = &one
	}

	// Requiring heartbeats anchors their deadline at creation when no
	// basis was given.
	if policy.RequireHeartbeat && in.HeartbeatBasis == nil {
		policy.HeartbeatBasis = "FROM_CREATION"
	}

	if err := access.PolicyWrite(access.PrincipalOf(ctx), policy, false); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.validate(policy); err != nil {
		return domain.Policy{}, err
	}

	if err := s.store.InsertPolicy(ctx, policy); err != nil {
		return domain.Policy{}, fmt.Errorf("policies: create: %w", err)
	}

	s.publish(ctx, "policy.created", policy)

	return policy, nil
}

// Update merges the input over the policy and writes it.
// Resolve loads the policy within the principal's scope (the
// before_action's scoped find; a miss is the 404 that precedes the
// typed-params decode and the authorization).
func (s Scenario) Resolve(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Policy, error) {
	return s.load(ctx, account.ID, id)
}

func (s Scenario) Update(ctx context.Context, account domain.Account, id uuid.UUID, in Input) (domain.Policy, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Policy{}, err
	}

	if err := access.Require(ctx, "policy.update"); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyWrite(access.PrincipalOf(ctx), policy, false); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	s.applyInput(&policy, in, false)
	policy.UpdatedAt = s.clock()

	if err := s.validate(policy); err != nil {
		return domain.Policy{}, err
	}

	if err := s.store.UpdatePolicy(ctx, policy); err != nil {
		return domain.Policy{}, fmt.Errorf("policies: update: %w", err)
	}

	s.publish(ctx, "policy.updated", policy)

	return policy, nil
}

// Destroy removes the policy.
func (s Scenario) Destroy(ctx context.Context, account domain.Account, id uuid.UUID) error {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "policy.delete"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyWrite(access.PrincipalOf(ctx), policy, true); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if _, err := s.store.DeletePolicy(ctx, account.ID, id); err != nil {
		return fmt.Errorf("policies: destroy: %w", err)
	}

	s.publish(ctx, "policy.deleted", policy)

	return nil
}

// applyInput overlays the request body over the policy (create: over
// the defaults; update: over the stored row).
func (s Scenario) applyInput( //nolint:cyclop,funlen // one branch per attribute
	policy *domain.Policy, in Input, create bool,
) {
	str := func(dst, src *string) {
		if src != nil {
			*dst = *src
		}
	}

	if in.Name != nil {
		policy.Name = *in.Name
	}

	if in.DurationSet {
		policy.Duration = in.Duration
	}

	boolPtr(&policy.Strict, in.Strict)
	boolPtr(&policy.Floating, in.Floating)

	if create {
		boolPtr(&policy.UsePool, in.UsePool)
		boolPtr(&policy.Encrypted, in.Encrypted)

		str(&policy.Scheme, in.Scheme)
	}

	if in.MaxMachinesSet {
		policy.MaxMachines = in.MaxMachines
	}

	if in.MaxProcessesSet {
		policy.MaxProcesses = in.MaxProcesses
	}

	if in.MaxUsersSet {
		policy.MaxUsers = in.MaxUsers
	}

	if in.MaxCoresSet {
		policy.MaxCores = in.MaxCores
	}

	if in.MaxMemorySet {
		policy.MaxMemory = in.MaxMemory
	}

	if in.MaxDiskSet {
		policy.MaxDisk = in.MaxDisk
	}

	if in.MaxUsesSet {
		policy.MaxUses = in.MaxUses
	}

	str(&policy.MachineUniquenessStrategy, in.MachineUniquenessStrategy)
	str(&policy.MachineMatchingStrategy, in.MachineMatchingStrategy)
	str(&policy.ComponentUniquenessStrategy, in.ComponentUniquenessStrategy)
	str(&policy.ComponentMatchingStrategy, in.ComponentMatchingStrategy)
	str(&policy.ExpirationStrategy, in.ExpirationStrategy)
	str(&policy.ExpirationBasis, in.ExpirationBasis)
	str(&policy.RenewalBasis, in.RenewalBasis)
	str(&policy.TransferStrategy, in.TransferStrategy)
	str(&policy.AuthenticationStrategy, in.AuthenticationStrategy)
	str(&policy.MachineLeasingStrategy, in.MachineLeasingStrategy)
	str(&policy.ProcessLeasingStrategy, in.ProcessLeasingStrategy)
	str(&policy.OverageStrategy, in.OverageStrategy)

	if in.ProtectedSet {
		policy.Protected = in.Protected
	}

	boolPtr(&policy.RequireProductScope, in.RequireProductScope)
	boolPtr(&policy.RequirePolicyScope, in.RequirePolicyScope)
	boolPtr(&policy.RequireMachineScope, in.RequireMachineScope)
	boolPtr(&policy.RequireFingerprintScope, in.RequireFingerprintScope)
	boolPtr(&policy.RequireComponentsScope, in.RequireComponentsScope)
	boolPtr(&policy.RequireUserScope, in.RequireUserScope)
	boolPtr(&policy.RequireChecksumScope, in.RequireChecksumScope)
	boolPtr(&policy.RequireVersionScope, in.RequireVersionScope)
	boolPtr(&policy.RequireCheckIn, in.RequireCheckIn)

	str(&policy.CheckInInterval, in.CheckInInterval)

	if in.CheckInIntervalCountSet {
		policy.CheckInIntervalCount = in.CheckInIntervalCount
	}

	if in.HeartbeatDurationSet {
		policy.HeartbeatDuration = in.HeartbeatDuration
	}

	str(&policy.HeartbeatCullStrategy, in.HeartbeatCullStrategy)
	str(&policy.HeartbeatResurrectionStrategy, in.HeartbeatResurrectionStrategy)
	str(&policy.HeartbeatBasis, in.HeartbeatBasis)
	boolPtr(&policy.RequireHeartbeat, in.RequireHeartbeat)

	if in.MetadataSet {
		policy.Metadata = in.Metadata
		if policy.Metadata == nil {
			policy.Metadata = map[string]any{}
		}
	}
}

// validate runs the model validations (Policy's rules, 1.5 of the
// behavior digest).
func (s Scenario) validate( //nolint:gocognit,gocyclo,cyclop,funlen // the table, verbatim
	policy domain.Policy,
) error {
	var errs []*apperr.T

	attr := func(code, detail, name string) {
		errs = append(errs, apperr.Validation(code, detail,
			"/data/attributes/"+name, "policies/#policies-object-attrs-"+name))
	}

	if strings.TrimSpace(policy.Name) == "" {
		attr("NAME_MISSING", "can't be blank", "name")
	} else if len(policy.Name) > 255 { //nolint:mnd // the reference's literal bound
		attr("NAME_TOO_LONG", "is too long (maximum is 255 characters)", "name")
	}

	if d := policy.Duration; d != nil && *d < 86400 {
		attr("DURATION_INVALID", "must be greater than or equal to 86400 (1 day)", "duration")
	}

	if m := policy.MaxMachines; m != nil {
		switch {
		case policy.Floating && *m < 1:
			attr("MAX_MACHINES_INVALID", "must be greater than or equal to 1 for floating policy", "maxMachines")
		case !policy.Floating && *m != 1:
			attr("MAX_MACHINES_INVALID", "must be equal to 1 for non-floating policy", "maxMachines")
		case *m < 0:
			attr("MAX_MACHINES_INVALID", "must be a number between 0 and 2147483647", "maxMachines")
		}
	}

	num := func(value *int32, minimum int32, code, name string) {
		if value == nil {
			return
		}

		if *value < minimum {
			attr(code, fmt.Sprintf("must be greater than or equal to %d", minimum), name)
		}
	}

	num(policy.MaxProcesses, 1, "MAX_PROCESSES_INVALID", "maxProcesses")
	num(policy.MaxUsers, 1, "MAX_USERS_INVALID", "maxUsers")
	num(policy.MaxCores, 1, "MAX_CORES_INVALID", "maxCores")
	num(policy.MaxUses, 0, "MAX_USES_INVALID", "maxUses")

	bignum := func(value *int64, minimum int64, code, name string) {
		if value == nil {
			return
		}

		if *value < minimum {
			attr(code, fmt.Sprintf("must be greater than or equal to %d", minimum), name)
		}
	}

	bignum(policy.MaxMemory, 1, "MAX_MEMORY_INVALID", "maxMemory")
	bignum(policy.MaxDisk, 1, "MAX_DISK_INVALID", "maxDisk")

	if h := policy.HeartbeatDuration; h != nil && *h < 60 {
		attr("HEARTBEAT_DURATION_INVALID", "must be greater than or equal to 60 (1 minute)", "heartbeatDuration")
	}

	if policy.RequireCheckIn {
		if policy.CheckInInterval != "" && !in(domain.CheckInIntervals(), policy.CheckInInterval) {
			attr("CHECK_IN_INTERVAL_NOT_ALLOWED", "must be one of: day, week, month, year", "checkInInterval")
		}

		if c := policy.CheckInIntervalCount; c != nil && (*c < 1 || *c > 365) {
			attr("CHECK_IN_INTERVAL_COUNT_NOT_ALLOWED", "must be a number between 1 and 365 inclusive", "checkInIntervalCount")
		}
	}

	enum := func(value, name string, allowed []string, code string) {
		if value != "" && !in(allowed, value) {
			attr(code, "unsupported "+strings.ToLower(strings.ReplaceAll(name, "Strategy", " strategy")), name)
		}
	}

	enum(policy.MachineUniquenessStrategy, "machineUniquenessStrategy",
		domain.MachineUniquenessStrategies(), "MACHINE_UNIQUENESS_STRATEGY_NOT_ALLOWED")
	enum(policy.MachineMatchingStrategy, "machineMatchingStrategy",
		domain.MatchingStrategies(), "MACHINE_MATCHING_STRATEGY_NOT_ALLOWED")
	enum(policy.ComponentUniquenessStrategy, "componentUniquenessStrategy",
		domain.ComponentUniquenessStrategies(), "COMPONENT_UNIQUENESS_STRATEGY_NOT_ALLOWED")
	enum(policy.ComponentMatchingStrategy, "componentMatchingStrategy", domain.MatchingStrategies(), "COMPONENT_MATCHING_STRATEGY_NOT_ALLOWED") //nolint:lll // the wire name is long
	enum(policy.ExpirationStrategy, "expirationStrategy", domain.ExpirationStrategies(), "EXPIRATION_STRATEGY_NOT_ALLOWED")
	enum(policy.ExpirationBasis, "expirationBasis", domain.ExpirationBases(), "EXPIRATION_BASIS_NOT_ALLOWED")
	enum(policy.RenewalBasis, "renewalBasis", domain.RenewalBases(), "RENEWAL_BASIS_NOT_ALLOWED")
	enum(policy.TransferStrategy, "transferStrategy", domain.TransferStrategies(), "TRANSFER_STRATEGY_NOT_ALLOWED")
	enum(policy.AuthenticationStrategy, "authenticationStrategy", domain.AuthenticationStrategies(), "AUTHENTICATION_STRATEGY_NOT_ALLOWED")  //nolint:lll // the wire name is long
	enum(policy.MachineLeasingStrategy, "machineLeasingStrategy", domain.MachineLeasingStrategies(), "MACHINE_LEASING_STRATEGY_NOT_ALLOWED") //nolint:lll // the wire name is long
	enum(policy.ProcessLeasingStrategy, "processLeasingStrategy", domain.ProcessLeasingStrategies(), "PROCESS_LEASING_STRATEGY_NOT_ALLOWED") //nolint:lll // the wire name is long
	enum(policy.HeartbeatBasis, "heartbeatBasis", domain.HeartbeatBases(), "HEARTBEAT_BASIS_NOT_ALLOWED")

	if policy.HeartbeatCullStrategy != "" && !in(domain.HeartbeatCullStrategies(), policy.HeartbeatCullStrategy) {
		attr("HEARTBEAT_CULL_STRATEGY_NOT_ALLOWED", "unsupported heartbeat cull strategy", "heartbeatCullStrategy")
	}

	if policy.HeartbeatResurrectionStrategy != "" &&
		!in(domain.HeartbeatResurrectionStrategies(), policy.HeartbeatResurrectionStrategy) {
		attr("HEARTBEAT_RESURRECTION_STRATEGY_NOT_ALLOWED", "unsupported heartbeat resurrection strategy", "heartbeatResurrectionStrategy") //nolint:lll // the wire name is long
	}

	if policy.HeartbeatResurrectionStrategy == "ALWAYS_REVIVE" && policy.HeartbeatCullStrategy != "KEEP_DEAD" {
		attr("HEARTBEAT_CULL_STRATEGY_NOT_ALLOWED",
			"incompatible heartbeat cull strategy (must be KEEP_DEAD when resurrection strategy is ALWAYS_REVIVE)",
			"heartbeatCullStrategy")
	}

	if policy.OverageStrategy != "" && !in(domain.OverageStrategies(), policy.OverageStrategy) {
		attr("OVERAGE_STRATEGY_NOT_ALLOWED", "unsupported overage strategy", "overageStrategy")
	} else if detail, bad := overageCompat(policy); bad {
		attr("OVERAGE_STRATEGY_NOT_ALLOWED", detail, "overageStrategy")
	}

	if policy.Scheme != "" && !in(domain.PolicyCryptoSchemes(), policy.Scheme) {
		attr("SCHEME_NOT_ALLOWED", "unsupported signing scheme", "scheme")
	}

	switch {
	case policy.Encrypted && policy.Scheme != "" && policy.Scheme != "LEGACY_ENCRYPT":
		attr("SCHEME_NOT_ALLOWED",
			"unsupported encryption scheme (scheme must be LEGACY_ENCRYPT for legacy encrypted policies)", "scheme")
	case !policy.Encrypted && policy.Scheme == "LEGACY_ENCRYPT":
		attr("SCHEME_INVALID", "must be encrypted when using LEGACY_ENCRYPT scheme", "scheme")
	case policy.UsePool && policy.Scheme != "":
		attr("SCHEME_NOT_SUPPORTED", "cannot use a scheme and use a pool", "scheme")
	}

	if policy.UsePool && policy.Encrypted {
		attr("ENCRYPTED_NOT_SUPPORTED", "cannot be encrypted and use a pool", "encrypted")
	}

	if err := apperr.MultiOf(errs...); err != nil {
		return err //nolint:wrapcheck // the document itself
	}

	return nil
}

// overageCompat answers the incompatible-overage detail (nil when the
// combination holds).
func overageCompat(policy domain.Policy) (string, bool) {
	switch policy.OverageStrategy {
	case "ALLOW_1_25X_OVERAGE":
		if !policy.Floating {
			return "incompatible overage strategy (cannot use ALLOW_1_25X_OVERAGE for node-locked policy)", true
		}

		return divisible(policy, 4, "ALLOW_1_25X_OVERAGE", "4") //nolint:mnd // the reference's literal bound
	case "ALLOW_1_5X_OVERAGE":
		if !policy.Floating {
			return "incompatible overage strategy (cannot use ALLOW_1_5X_OVERAGE for node-locked policy)", true
		}

		return divisible(policy, 2, "ALLOW_1_5X_OVERAGE", "2") //nolint:mnd // the reference's literal bound
	default:
		return "", false
	}
}

func divisible(policy domain.Policy, by int32, strategy, factor string) (string, bool) {
	bad := func(name string, value int32) (string, bool) {
		if value%by != 0 {
			return fmt.Sprintf(
				"incompatible overage strategy (cannot use %s with a max %s value not divisible by %s)",
				strategy, name, factor,
			), true
		}

		return "", false
	}

	for _, pair := range []struct {
		name  string
		value *int32
	}{
		{"machines", policy.MaxMachines},
		{"processes", policy.MaxProcesses},
		{"users", policy.MaxUsers},
		{"cores", policy.MaxCores},
	} {
		if pair.value == nil {
			continue
		}

		if detail, badPair := bad(pair.name, *pair.value); badPair {
			return detail, true
		}
	}

	return "", false
}

// load fetches the policy within the principal's scope (a miss is the
// reference's scoped-find 404).
func (s Scenario) load(ctx context.Context, accountID uuid.UUID, id uuid.UUID) (domain.Policy, error) { //nolint:gocritic,lll // mirrors the reference's shape
	policy, err := s.store.PolicyByID(ctx, accountID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.Policy{}, apperr.NotFound("policy", id.String())
	}

	if err != nil {
		return domain.Policy{}, fmt.Errorf("policies: load: %w", err)
	}

	principal := access.PrincipalOf(ctx)

	switch principal.Role { //nolint:exhaustive // the default branch covers the rest
	case domain.RoleProduct:
		if policy.ProductID != principal.BearerID {
			return domain.Policy{}, apperr.NotFound("policy", id.String())
		}
	case domain.RoleUser:
		ids, err := s.store.UserPolicyIDs(ctx, accountID, principal.BearerID)
		if err != nil {
			return domain.Policy{}, fmt.Errorf("policies: scope: %w", err)
		}

		if _, ok := idSet(ids)[policy.ID]; !ok {
			return domain.Policy{}, apperr.NotFound("policy", id.String())
		}
	case domain.RoleLicense:
		if principal.License != nil {
			own, err := s.store.PolicyByLicense(ctx, accountID, principal.License.ID)
			if err != nil {
				return domain.Policy{}, apperr.NotFound("policy", id.String())
			}

			if own.ID != policy.ID {
				return domain.Policy{}, apperr.NotFound("policy", id.String())
			}
		}
	}

	return policy, nil
}

func (s Scenario) publish(ctx context.Context, event string, policy domain.Policy) {
	_ = s.events.Publish(ctx, event, policy)
}

func boolPtr(dst, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func idSet(ids []uuid.UUID) map[uuid.UUID]struct{} {
	set := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}

	return set
}

func in(list []string, value string) bool {
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}

	return false
}
